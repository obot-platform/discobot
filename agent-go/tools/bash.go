package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/thread"
)

type bashInput struct {
	Command         string `json:"command"`
	Description     string `json:"description"`
	Timeout         int    `json:"timeout"` // milliseconds; 0 = default 120s
	RunInBackground bool   `json:"run_in_background"`
}

func (e *Executor) executeBash(ctx context.Context, toolCtx *thread.ToolContext, call message.ToolCallPart) (thread.ToolExecuteResult, error) {
	var input bashInput
	if err := unmarshalInput(call, &input); err != nil {
		return errResult(call, err.Error()), nil
	}
	if input.Command == "" {
		return errResult(call, "command is required"), nil
	}

	timeout := 120 * time.Second
	if input.Timeout > 0 {
		ms := input.Timeout
		if ms > 600_000 {
			ms = 600_000
		}
		timeout = time.Duration(ms) * time.Millisecond
	}

	if input.RunInBackground {
		return e.startBashBackground(toolCtx, call, input.Command)
	}
	return e.runBashSync(ctx, toolCtx, call, input.Command, timeout)
}

// bashLogPath returns the path for the log file for a bash call.
// All bash output (foreground and background) is persisted here so the LLM
// can reference or tail the file later.
//
// Path: {dataDir}/bash/{threadID}/{toolCallID}.log
func (e *Executor) bashLogPath(toolCtx *thread.ToolContext, toolCallID string) string {
	return filepath.Join(e.dataDir, "bash", contextThreadID(toolCtx, e.defaultThreadID), toolCallID+".log")
}

// runBashSync runs a bash command synchronously, returns the combined output,
// and saves it to a log file in {dataDir}/bash/{threadID}/.
func (e *Executor) runBashSync(ctx context.Context, toolCtx *thread.ToolContext, call message.ToolCallPart, command string, timeout time.Duration) (thread.ToolExecuteResult, error) {
	cwd := e.getCwd()
	logPath := e.bashLogPath(toolCtx, call.ToolCallID)
	bashPath, err := resolveBashCommand()
	if err != nil {
		return errResult(call, err.Error()), nil
	}

	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return errResult(call, fmt.Sprintf("failed to create log directory: %v", err)), nil
	}

	logFile, err := os.Create(logPath)
	if err != nil {
		return errResult(call, fmt.Sprintf("failed to create log file: %v", err)), nil
	}
	defer logFile.Close()

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Wrap command to capture the new working directory after execution.
	const sentinel = "__DISCOBOT_PWD_SENTINEL__"
	wrapped := fmt.Sprintf("%s\n__exit=$?\nprintf '%%s\\n' '%s'\npwd\nexit $__exit", command, sentinel)

	cmd := exec.CommandContext(cmdCtx, bashPath, "-c", wrapped)
	cmd.Dir = cwd
	cmd.Env = e.bashEnv()
	// Put bash in its own process group so that killing it also kills any
	// child processes it spawned (e.g. sleep, subshells).
	setSysProcAttr(cmd)
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Kill the entire process group (negative PID = pgid).
		return killProcessGroup(cmd.Process.Pid, syscall.SIGKILL)
	}

	// Capture stdout and stderr separately so that the sentinel (written to
	// stdout) is never mixed with stderr content. Both streams are tee'd to
	// the shared log file so the on-disk record stays interleaved.
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = io.MultiWriter(&stdoutBuf, logFile)
	cmd.Stderr = io.MultiWriter(&stderrBuf, logFile)

	_ = cmd.Run()

	// Parse the sentinel and cwd from stdout only, then append stderr.
	stdoutUser, newCwd := extractCwdFromOutput(stdoutBuf.String(), sentinel)
	output := stdoutUser + stderrBuf.String()

	if newCwd != "" && newCwd != cwd {
		e.setCwd(newCwd)
	}

	if cmdCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
		output = strings.TrimRight(output, "\n") + fmt.Sprintf("\n[Command timed out after %s and was killed]", timeout)
		fmt.Fprintf(logFile, "[Command timed out after %s and was killed]\n", timeout)
	}

	return textResult(call, addLineNumbers(output, 1)), nil
}

// startBashBackground launches a bash command in the background. It returns
// immediately with the process PID and log path so the LLM can tail or read
// the output at any time. Output is streamed directly to the log file.
func (e *Executor) startBashBackground(toolCtx *thread.ToolContext, call message.ToolCallPart, command string) (thread.ToolExecuteResult, error) {
	cwd := e.getCwd()
	logPath := e.bashLogPath(toolCtx, call.ToolCallID)
	bashPath, err := resolveBashCommand()
	if err != nil {
		return errResult(call, err.Error()), nil
	}

	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return errResult(call, fmt.Sprintf("failed to create log directory: %v", err)), nil
	}

	logFile, err := os.Create(logPath)
	if err != nil {
		return errResult(call, fmt.Sprintf("failed to create log file: %v", err)), nil
	}

	cmd := exec.Command(bashPath, "-c", command) //nolint:gosec
	cmd.Dir = cwd
	cmd.Env = e.bashEnv()
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return errResult(call, fmt.Sprintf("failed to start background command: %v", err)), nil
	}

	pid := cmd.Process.Pid

	// Let the process run independently; close the log file when it exits.
	go func() {
		_ = cmd.Wait()
		logFile.Close()
	}()

	return textResult(call, fmt.Sprintf(
		"Background process started.\nPID: %d\nOutput: %s\n\nUse `tail -f %s` to follow the output, or `kill %d` to stop it.",
		pid, logPath, logPath, pid,
	)), nil
}

func resolveBashCommand() (string, error) {
	return resolveBashCommandForOS(runtime.GOOS, filepath.SplitList(os.Getenv("PATH")), os.Getenv("PATHEXT"))
}

func resolveBashCommandForOS(goos string, pathDirs []string, pathExt string) (string, error) {
	if goos != "windows" {
		return "bash", nil
	}

	for _, dir := range pathDirs {
		dir = strings.TrimSpace(strings.Trim(dir, `"`))
		if dir == "" {
			continue
		}
		for _, ext := range windowsBashExtensions(pathExt) {
			candidate := filepath.Join(dir, "bash"+ext)
			info, err := os.Stat(candidate)
			if err != nil || info.IsDir() {
				continue
			}
			return candidate, nil
		}
	}

	return "", fmt.Errorf("bash is not available: no real Bash executable was found in PATH. Install a Bash executable such as bash.exe from Git Bash or another compatibility layer; bash.cmd and bash.bat are not supported")
}

func windowsBashExtensions(pathExt string) []string {
	exts := []string{".exe"}
	seen := map[string]struct{}{
		".exe": {},
	}

	for _, raw := range strings.Split(pathExt, ";") {
		ext := strings.TrimSpace(strings.ToLower(raw))
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		if ext == ".cmd" || ext == ".bat" {
			continue
		}
		if _, ok := seen[ext]; ok {
			continue
		}
		seen[ext] = struct{}{}
		exts = append(exts, ext)
	}

	return exts
}

// extractCwdFromOutput splits the sentinel + pwd from the end of the output.
// Returns (userOutput, newCwd).
func extractCwdFromOutput(raw, sentinel string) (string, string) {
	idx := strings.LastIndex(raw, sentinel)
	if idx < 0 {
		return raw, ""
	}
	userOutput := raw[:idx]
	after := strings.TrimPrefix(raw[idx:], sentinel)
	after = strings.TrimPrefix(after, "\n")
	lines := strings.SplitN(after, "\n", 2)
	newCwd := strings.TrimSpace(lines[0])
	return userOutput, newCwd
}

// getCwd returns the current persisted working directory.
func (e *Executor) getCwd() string {
	e.cwdMu.Lock()
	defer e.cwdMu.Unlock()
	return e.currentCwd
}

// setCwd updates the persisted working directory.
func (e *Executor) setCwd(cwd string) {
	e.cwdMu.Lock()
	defer e.cwdMu.Unlock()
	e.currentCwd = cwd
}
