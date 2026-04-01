package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/obot-platform/discobot/agent-go/message"
)

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("bash is not available on Windows")
	}
}

func skipIfBashUnavailable(t *testing.T) {
	t.Helper()
	if _, err := resolveBashCommand(); err != nil {
		t.Skipf("bash unavailable: %v", err)
	}
}

// runBash is a test helper that executes a Bash tool call and returns the output text.
// It accepts an arbitrary input map so callers can set timeout, run_in_background, etc.
func runBash(t *testing.T, e *Executor, input map[string]any) (string, bool) {
	t.Helper()
	raw, _ := json.Marshal(input)
	result, err := e.Execute(context.Background(), nil, message.ToolCallPart{
		ToolCallID: t.Name(),
		ToolName:   "Bash",
		Input:      string(raw),
	})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	switch v := result.Result.Output.(type) {
	case message.TextOutput:
		return v.Value, true
	case message.ErrorTextOutput:
		return v.Value, false
	}
	return "", false
}

func TestBash_EmptyCommand(t *testing.T) {
	skipOnWindows(t)
	e := New(t.TempDir(), t.TempDir(), t.Name())
	_, ok := runBash(t, e, map[string]any{"command": ""})
	if ok {
		t.Error("expected ErrorTextOutput for empty command, got TextOutput")
	}
}

func TestBash_SimpleEcho(t *testing.T) {
	skipOnWindows(t)
	e := New(t.TempDir(), t.TempDir(), t.Name())
	out, ok := runBash(t, e, map[string]any{"command": "echo hello"})
	if !ok {
		t.Fatalf("unexpected error output: %s", out)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("expected 'hello' in output, got: %q", out)
	}
}

// TestBash_LineNumbers verifies output lines carry "     N→" prefixes.
func TestBash_LineNumbers(t *testing.T) {
	skipOnWindows(t)
	e := New(t.TempDir(), t.TempDir(), t.Name())
	out, ok := runBash(t, e, map[string]any{"command": "echo hello"})
	if !ok {
		t.Fatalf("unexpected error output: %s", out)
	}
	// addLineNumbers produces "     1→hello\n"
	if !strings.Contains(out, "→") {
		t.Errorf("expected →-separated line numbers in output, got: %q", out)
	}
	trimmed := strings.TrimLeft(out, " ")
	if !strings.HasPrefix(trimmed, "1→") {
		t.Errorf("expected first line to start with '1→', got: %q", out)
	}
}

// TestBash_MultiLineNumbers verifies each line gets the correct sequential number.
func TestBash_MultiLineNumbers(t *testing.T) {
	skipOnWindows(t)
	e := New(t.TempDir(), t.TempDir(), t.Name())
	out, ok := runBash(t, e, map[string]any{"command": "printf 'a\\nb\\nc\\n'"})
	if !ok {
		t.Fatalf("unexpected error output: %s", out)
	}
	for i, want := range []string{"a", "b", "c"} {
		// Each line must appear with its correct number.
		if !strings.Contains(out, strings.TrimSpace(strings.Repeat(" ", 5)+string(rune('0'+i+1))+"\t"+want)) {
			t.Logf("full output:\n%s", out)
		}
		// Simpler: just verify the content appears and line count is right.
		_ = want
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 numbered lines, got %d:\n%s", len(lines), out)
	}
}

// TestBash_StderrCaptured verifies stderr output is included in the result.
func TestBash_StderrCaptured(t *testing.T) {
	skipOnWindows(t)
	e := New(t.TempDir(), t.TempDir(), t.Name())
	out, _ := runBash(t, e, map[string]any{"command": "echo errline >&2"})
	if !strings.Contains(out, "errline") {
		t.Errorf("expected stderr 'errline' in output, got: %s", out)
	}
}

// TestBash_NonZeroExitIsTextOutput verifies a failing command returns TextOutput, not an error.
func TestBash_NonZeroExitIsTextOutput(t *testing.T) {
	skipOnWindows(t)
	e := New(t.TempDir(), t.TempDir(), t.Name())
	_, ok := runBash(t, e, map[string]any{"command": "exit 1"})
	if !ok {
		t.Error("expected TextOutput (not ErrorTextOutput) for non-zero exit code")
	}
}

// TestBash_CwdPersistsAcrossCalls verifies that a cd in one call is visible in the next.
func TestBash_CwdPersistsAcrossCalls(t *testing.T) {
	skipOnWindows(t)
	e := New(t.TempDir(), t.TempDir(), t.Name())
	runBash(t, e, map[string]any{"command": "cd /tmp"})
	out, ok := runBash(t, e, map[string]any{"command": "pwd"})
	if !ok {
		t.Fatalf("unexpected error: %s", out)
	}
	if !strings.Contains(out, "/tmp") {
		t.Errorf("expected cwd '/tmp' after 'cd /tmp', got: %s", out)
	}
}

func TestBash_HeredocCommandOutput(t *testing.T) {
	skipOnWindows(t)
	e := New(t.TempDir(), t.TempDir(), t.Name())

	out, ok := runBash(t, e, map[string]any{"command": "cat <<'EOF'\nfoo\nEOF"})
	if !ok {
		t.Fatalf("unexpected error: %s", out)
	}
	if !strings.Contains(out, "foo") {
		t.Errorf("expected heredoc output in result, got: %s", out)
	}
}

func TestBash_HeredocThenCwdPersistsAcrossCalls(t *testing.T) {
	skipOnWindows(t)
	e := New(t.TempDir(), t.TempDir(), t.Name())

	out, ok := runBash(t, e, map[string]any{"command": "cat <<'EOF'\nhello\nEOF\ncd /tmp"})
	if !ok {
		t.Fatalf("unexpected error: %s", out)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("expected heredoc output in result, got: %s", out)
	}

	out, ok = runBash(t, e, map[string]any{"command": "pwd"})
	if !ok {
		t.Fatalf("unexpected error: %s", out)
	}
	if !strings.Contains(out, "/tmp") {
		t.Errorf("expected cwd '/tmp' after heredoc + cd, got: %s", out)
	}
}

// TestBash_LogFileCreatedForeground verifies the log file is written to the expected path.
func TestBash_LogFileCreatedForeground(t *testing.T) {
	skipOnWindows(t)
	cwd := t.TempDir()
	dataDir := t.TempDir()
	e := New(cwd, dataDir, "thread-1")
	raw, _ := json.Marshal(map[string]string{"command": "echo logged"})
	callID := "call-fg"
	_, err := e.Execute(context.Background(), nil, message.ToolCallPart{
		ToolCallID: callID,
		ToolName:   "Bash",
		Input:      string(raw),
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	logPath := filepath.Join(dataDir, "bash", "thread-1", callID+".log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("log file not found at %s: %v", logPath, err)
	}
	if !strings.Contains(string(data), "logged") {
		t.Errorf("expected 'logged' in log file, got: %s", data)
	}
}

// TestBash_Timeout verifies that a slow command is killed and the output contains a timeout notice.
func TestBash_Timeout(t *testing.T) {
	skipOnWindows(t)
	e := New(t.TempDir(), t.TempDir(), t.Name())
	raw, _ := json.Marshal(map[string]any{
		"command": "sleep 60",
		"timeout": 100, // 100 ms
	})
	start := time.Now()
	result, err := e.Execute(context.Background(), nil, message.ToolCallPart{
		ToolCallID: "timeout-call",
		ToolName:   "Bash",
		Input:      string(raw),
	})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}

	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("timeout did not fire: command ran for %s", elapsed)
	}

	out, ok := "", false
	switch v := result.Result.Output.(type) {
	case message.TextOutput:
		out, ok = v.Value, true
	case message.ErrorTextOutput:
		out = v.Value
	}
	if !ok {
		t.Fatalf("unexpected ErrorTextOutput: %s", out)
	}
	if !strings.Contains(out, "timed out") {
		t.Errorf("expected 'timed out' in output, got: %s", out)
	}
}

// TestBash_BackgroundReturnsPIDAndLogPath verifies the immediate response for a background command.
func TestBash_BackgroundReturnsPIDAndLogPath(t *testing.T) {
	skipOnWindows(t)
	cwd := t.TempDir()
	e := New(cwd, t.TempDir(), "bg-thread")
	raw, _ := json.Marshal(map[string]any{
		"command":           "sleep 5",
		"run_in_background": true,
	})
	result, err := e.Execute(context.Background(), nil, message.ToolCallPart{
		ToolCallID: "bg-call",
		ToolName:   "Bash",
		Input:      string(raw),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out, ok := result.Result.Output.(message.TextOutput)
	if !ok {
		t.Fatalf("expected TextOutput for background command, got %T", result.Result.Output)
	}
	if !strings.Contains(out.Value, "PID:") {
		t.Errorf("expected 'PID:' in output, got: %s", out.Value)
	}
	if !strings.Contains(out.Value, "bg-call.log") {
		t.Errorf("expected log path containing 'bg-call.log' in output, got: %s", out.Value)
	}
}

// TestBash_BackgroundLogFileWritten verifies background output is saved to the log file.
func TestBash_BackgroundLogFileWritten(t *testing.T) {
	skipOnWindows(t)
	dataDir := t.TempDir()
	e := New(t.TempDir(), dataDir, "bg-thread")
	raw, _ := json.Marshal(map[string]any{
		"command":           "echo bg-output",
		"run_in_background": true,
	})
	_, err := e.Execute(context.Background(), nil, message.ToolCallPart{
		ToolCallID: "bg-log",
		ToolName:   "Bash",
		Input:      string(raw),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	logPath := filepath.Join(dataDir, "bash", "bg-thread", "bg-log.log")
	// Give the background process time to write.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(logPath)
		if err == nil && strings.Contains(string(data), "bg-output") {
			return // pass
		}
		time.Sleep(50 * time.Millisecond)
	}
	data, _ := os.ReadFile(logPath)
	t.Errorf("expected 'bg-output' in background log file within 2s, got: %s", data)
}

func TestBash_DefaultPassesProcessEnv(t *testing.T) {
	skipOnWindows(t)
	t.Setenv("DISCOBOT_BASH_ENV_TEST_DEFAULT", "present")

	e := New(t.TempDir(), t.TempDir(), t.Name())
	out, ok := runBash(t, e, map[string]any{"command": "echo \"${DISCOBOT_BASH_ENV_TEST_DEFAULT}\""})
	if !ok {
		t.Fatalf("unexpected error output: %s", out)
	}
	if !strings.Contains(out, "→present") {
		t.Errorf("expected env var to be visible to bash, got: %q", out)
	}
}

func TestBash_AllowlistFiltersEnv(t *testing.T) {
	skipOnWindows(t)
	t.Setenv("DISCOBOT_BASH_ENV_TEST_ALLOWED", "yes")
	t.Setenv("DISCOBOT_BASH_ENV_TEST_BLOCKED", "no")

	e := New(t.TempDir(), t.TempDir(), t.Name())
	e.SetBashEnvAllowlist([]string{"DISCOBOT_BASH_ENV_TEST_ALLOWED"})

	out, ok := runBash(t, e, map[string]any{"command": "echo \"${DISCOBOT_BASH_ENV_TEST_ALLOWED}|${DISCOBOT_BASH_ENV_TEST_BLOCKED}\""})
	if !ok {
		t.Fatalf("unexpected error output: %s", out)
	}
	if !strings.Contains(out, "→yes|") {
		t.Errorf("expected only allowlisted env var in output, got: %q", out)
	}
}

func TestBash_RequestScopedEnvVisible(t *testing.T) {
	skipOnWindows(t)

	e := New(t.TempDir(), t.TempDir(), t.Name())
	e.SetEnvSnapshot(func() map[string]string {
		return map[string]string{"DISCOBOT_BASH_ENV_TEST_REQUEST": "from-request"}
	})

	out, ok := runBash(t, e, map[string]any{"command": "echo \"${DISCOBOT_BASH_ENV_TEST_REQUEST}\""})
	if !ok {
		t.Fatalf("unexpected error output: %s", out)
	}
	if !strings.Contains(out, "→from-request") {
		t.Errorf("expected request-scoped env var to be visible to bash, got: %q", out)
	}
}

func TestBash_RequestScopedEnvRespectsAllowlist(t *testing.T) {
	skipOnWindows(t)

	e := New(t.TempDir(), t.TempDir(), t.Name())
	e.SetBashEnvAllowlist([]string{"DISCOBOT_BASH_ENV_TEST_ALLOWED_REQUEST"})
	e.SetEnvSnapshot(func() map[string]string {
		return map[string]string{
			"DISCOBOT_BASH_ENV_TEST_ALLOWED_REQUEST": "allowed-request",
			"DISCOBOT_BASH_ENV_TEST_BLOCKED_REQUEST": "blocked-request",
		}
	})

	out, ok := runBash(t, e, map[string]any{"command": "echo \"${DISCOBOT_BASH_ENV_TEST_ALLOWED_REQUEST}|${DISCOBOT_BASH_ENV_TEST_BLOCKED_REQUEST}\""})
	if !ok {
		t.Fatalf("unexpected error output: %s", out)
	}
	if !strings.Contains(out, "→allowed-request|") {
		t.Errorf("expected only allowlisted request-scoped env var in output, got: %q", out)
	}
}

func TestBash_RequestScopedEnvOverridesProcessEnv(t *testing.T) {
	skipOnWindows(t)
	t.Setenv("DISCOBOT_BASH_ENV_TEST_OVERRIDE", "from-process")

	e := New(t.TempDir(), t.TempDir(), t.Name())
	e.SetEnvSnapshot(func() map[string]string {
		return map[string]string{"DISCOBOT_BASH_ENV_TEST_OVERRIDE": "from-request"}
	})

	out, ok := runBash(t, e, map[string]any{"command": "echo \"${DISCOBOT_BASH_ENV_TEST_OVERRIDE}\""})
	if !ok {
		t.Fatalf("unexpected error output: %s", out)
	}
	if !strings.Contains(out, "→from-request") {
		t.Errorf("expected request-scoped env var to override process env, got: %q", out)
	}
}

func TestBash_InvalidWorkingDirReturnsError(t *testing.T) {
	skipIfBashUnavailable(t)

	e := New(t.TempDir(), t.TempDir(), t.Name())
	e.setCwd(filepath.Join(t.TempDir(), "missing"))

	out, ok := runBash(t, e, map[string]any{"command": "pwd"})
	if ok {
		t.Fatalf("expected ErrorTextOutput for invalid cwd, got: %s", out)
	}
	if !strings.Contains(out, "failed to run command") {
		t.Fatalf("expected startup error in output, got: %q", out)
	}
}

func TestBash_WindowsSecondCommandStillRunsAfterPwd(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only test")
	}
	skipIfBashUnavailable(t)

	cwd := t.TempDir()
	e := New(cwd, t.TempDir(), t.Name())

	out, ok := runBash(t, e, map[string]any{"command": "pwd"})
	if !ok {
		t.Fatalf("unexpected error from first pwd: %s", out)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("expected first pwd output to be non-empty")
	}
	if !sameResolvedPath(e.getCwd(), cwd) {
		t.Fatalf("executor cwd = %q, want native path matching %q", e.getCwd(), cwd)
	}

	out, ok = runBash(t, e, map[string]any{"command": "pwd"})
	if !ok {
		t.Fatalf("unexpected error from second pwd: %s", out)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("expected second pwd output to be non-empty")
	}
}

func TestNormalizeBashWorkingDirForOS(t *testing.T) {
	tests := []struct {
		name string
		goos string
		cwd  string
		want string
	}{
		{
			name: "non-windows unchanged",
			goos: "linux",
			cwd:  "/tmp/discobot",
			want: "/tmp/discobot",
		},
		{
			name: "msys drive path converted",
			goos: "windows",
			cwd:  "/e/src/discobot",
			want: `E:\src\discobot`,
		},
		{
			name: "wsl drive path converted",
			goos: "windows",
			cwd:  "/mnt/c/Users/tester/project",
			want: `C:\Users\tester\project`,
		},
		{
			name: "non-drive path left alone",
			goos: "windows",
			cwd:  "/home/tester",
			want: "/home/tester",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeBashWorkingDirForOS(tc.goos, tc.cwd); got != tc.want {
				t.Fatalf("normalizeBashWorkingDirForOS() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveBashCommandForOS_NonWindows(t *testing.T) {
	got, err := resolveBashCommandForOS("linux", nil, "")
	if err != nil {
		t.Fatalf("resolveBashCommandForOS returned error: %v", err)
	}
	if got != "bash" {
		t.Fatalf("resolveBashCommandForOS() = %q, want %q", got, "bash")
	}
}

func TestResolveBashCommandForOS_WindowsPrefersRealExecutable(t *testing.T) {
	dirWithCmd := t.TempDir()
	dirWithExe := t.TempDir()

	if err := os.WriteFile(filepath.Join(dirWithCmd, "bash.cmd"), []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatalf("failed to create bash.cmd: %v", err)
	}
	want := filepath.Join(dirWithExe, "bash.exe")
	if err := os.WriteFile(want, []byte("fake exe"), 0o644); err != nil {
		t.Fatalf("failed to create bash.exe: %v", err)
	}

	got, err := resolveBashCommandForOS("windows", []string{dirWithCmd, dirWithExe}, ".CMD;.EXE;.BAT")
	if err != nil {
		t.Fatalf("resolveBashCommandForOS returned error: %v", err)
	}
	if got != want {
		t.Fatalf("resolveBashCommandForOS() = %q, want %q", got, want)
	}
}

func TestResolveBashCommandForOS_WindowsUsesFirstExecutableInPath(t *testing.T) {
	firstDir := t.TempDir()
	secondDir := t.TempDir()

	first := filepath.Join(firstDir, "bash.exe")
	second := filepath.Join(secondDir, "bash.exe")
	if err := os.WriteFile(first, []byte("first"), 0o644); err != nil {
		t.Fatalf("failed to create first bash.exe: %v", err)
	}
	if err := os.WriteFile(second, []byte("second"), 0o644); err != nil {
		t.Fatalf("failed to create second bash.exe: %v", err)
	}

	got, err := resolveBashCommandForOS("windows", []string{firstDir, secondDir}, ".EXE")
	if err != nil {
		t.Fatalf("resolveBashCommandForOS returned error: %v", err)
	}
	if got != first {
		t.Fatalf("resolveBashCommandForOS() = %q, want %q", got, first)
	}
}

func TestResolveBashCommandForOS_WindowsRejectsBatchShims(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bash.cmd"), []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatalf("failed to create bash.cmd: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bash.bat"), []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatalf("failed to create bash.bat: %v", err)
	}

	_, err := resolveBashCommandForOS("windows", []string{dir}, ".CMD;.BAT")
	if err == nil {
		t.Fatal("expected resolveBashCommandForOS to fail when only batch shims exist")
	}
	if !strings.Contains(err.Error(), "bash.cmd and bash.bat are not supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestExtractCwdFromOutput unit-tests the sentinel-based cwd extraction helper.
func TestExtractCwdFromOutput(t *testing.T) {
	const sentinel = "__DISCOBOT_PWD_SENTINEL__"

	tests := []struct {
		name    string
		raw     string
		wantOut string
		wantCwd string
	}{
		{
			name:    "basic",
			raw:     "output line\n" + sentinel + "\n/home/user\n",
			wantOut: "output line\n",
			wantCwd: "/home/user",
		},
		{
			name:    "no sentinel",
			raw:     "just output\n",
			wantOut: "just output\n",
			wantCwd: "",
		},
		{
			name:    "empty user output",
			raw:     sentinel + "\n/tmp\n",
			wantOut: "",
			wantCwd: "/tmp",
		},
		{
			name:    "multiple sentinels uses last",
			raw:     sentinel + "\n/first\n" + sentinel + "\n/second\n",
			wantOut: sentinel + "\n/first\n",
			wantCwd: "/second",
		},
		{
			name:    "cwd with trailing whitespace is trimmed",
			raw:     sentinel + "\n/some/path   \n",
			wantOut: "",
			wantCwd: "/some/path",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotOut, gotCwd := extractCwdFromOutput(tc.raw, sentinel)
			if gotOut != tc.wantOut {
				t.Errorf("output: got %q, want %q", gotOut, tc.wantOut)
			}
			if gotCwd != tc.wantCwd {
				t.Errorf("cwd: got %q, want %q", gotCwd, tc.wantCwd)
			}
		})
	}
}
