package hooks

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// DefaultTimeout is the default hook execution timeout (15 minutes).
const DefaultTimeout = 15 * time.Minute

// HookResult is the result of executing a hook.
type HookResult struct {
	Success    bool
	ExitCode   int
	Output     string
	Hook       Hook
	DurationMs int64
}

// ExecuteOptions configures how a hook is executed.
type ExecuteOptions struct {
	Cwd          string
	Env          map[string]string
	Timeout      time.Duration
	ChangedFiles []string
	SessionID    string
	OutputPath   string
}

// ExecuteHook runs a hook script and returns the result.
func ExecuteHook(hook Hook, opts ExecuteOptions) HookResult {
	start := time.Now()

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	command, args := buildHookCommand(hook.Path)
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = opts.Cwd

	// Build environment
	env := os.Environ()
	env = append(env, "DISCOBOT_HOOK_TYPE="+string(hook.Type))
	if opts.SessionID != "" {
		env = append(env, "DISCOBOT_SESSION_ID="+opts.SessionID)
	}
	if opts.Cwd != "" {
		env = append(env, "DISCOBOT_WORKSPACE="+opts.Cwd)
	}
	if len(opts.ChangedFiles) > 0 {
		env = append(env, "DISCOBOT_CHANGED_FILES="+strings.Join(opts.ChangedFiles, " "))
	}
	for k, v := range opts.Env {
		env = append(env, k+"="+v)
	}
	cmd.Env = env

	// Set process group so we can kill the entire group on timeout
	setSysProcAttr(cmd)

	// Capture stdout and stderr
	var outputBuf bytes.Buffer
	cmd.Stdout = &outputBuf
	cmd.Stderr = &outputBuf

	err := cmd.Start()
	if err != nil {
		result := HookResult{
			Success:    false,
			ExitCode:   127,
			Output:     err.Error(),
			Hook:       hook,
			DurationMs: time.Since(start).Milliseconds(),
		}
		writeOutputFile(opts.OutputPath, result.Output)
		return result
	}

	err = cmd.Wait()
	durationMs := time.Since(start).Milliseconds()
	output := outputBuf.String()

	exitCode := 0
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			// Kill process group on timeout
			if cmd.Process != nil {
				_ = killProcessGroup(cmd.Process.Pid, syscall.SIGKILL)
			}
			exitCode = 124
			output += fmt.Sprintf("\n[Hook timed out after %ds and was killed]\n", int(timeout.Seconds()))
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 126
		}
	}

	result := HookResult{
		Success:    exitCode == 0,
		ExitCode:   exitCode,
		Output:     output,
		Hook:       hook,
		DurationMs: durationMs,
	}

	writeOutputFile(opts.OutputPath, output)

	return result
}

// GetHookOutputPath returns the path to a hook's output log file.
func GetHookOutputPath(hooksDataDir, hookID string) string {
	return filepath.Join(hooksDataDir, "output", hookID+".log")
}

// writeOutputFile writes output to a file, creating parent directories.
func writeOutputFile(path, output string) {
	if path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(output), 0o644)
}

func buildHookCommand(path string) (string, []string) {
	if runtime.GOOS != "windows" {
		return path, nil
	}

	interpreter, args := parseScriptShebang(path)
	switch interpreter {
	case "bash", "sh", "zsh":
		return "bash", append(args, path)
	case "":
		return path, nil
	default:
		return interpreter, append(args, path)
	}
}

func parseScriptShebang(path string) (string, []string) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", nil
	}

	line, _, _ := strings.Cut(string(content), "\n")
	if !strings.HasPrefix(line, "#!") {
		return "", nil
	}

	fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "#!")))
	if len(fields) == 0 {
		return "", nil
	}

	interpreter := filepath.Base(fields[0])
	args := fields[1:]
	if interpreter == "env" {
		for len(args) > 0 && args[0] == "-S" {
			args = args[1:]
		}
		if len(args) == 0 {
			return "", nil
		}
		interpreter = filepath.Base(args[0])
		args = args[1:]
	}

	return strings.ToLower(interpreter), args
}
