package hooks

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestFormatHookFailureMessage_UsesMarkdownWithInlineOutput(t *testing.T) {
	message := formatHookFailureMessage(buildHookFailureMessageMetadata(HookResult{
		Hook: Hook{
			Name:    "Go Check",
			Pattern: "*.go",
		},
		ExitCode: 2,
		Output:   "lint failed\nsecond line",
	}, []string{"main.go", "internal/app.go"}, "/tmp/go-check.log", ""))

	for _, want := range []string{
		"### Hook failed: Go Check",
		"- Exit code: `2`",
		"- Pattern: `*.go`",
		"- Files: main.go, internal/app.go",
		"#### Output",
		"```text",
		"lint failed\nsecond line",
		"Please fix the issues above and ensure the hook passes.",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("expected markdown hook failure message to contain %q, got:\n%s", want, message)
		}
	}
}

func TestFormatHookFailureMessage_UsesOutputPathWhenInlineOutputTooLarge(t *testing.T) {
	var outputLines []string
	for i := 1; i <= 20; i++ {
		outputLines = append(outputLines, strings.Repeat("x", 300)+strings.Join([]string{" line ", strconv.Itoa(i)}, ""))
	}

	message := formatHookFailureMessage(buildHookFailureMessageMetadata(HookResult{
		Hook: Hook{
			Name: "Go Check",
		},
		ExitCode: 1,
		Output:   strings.Join(outputLines, "\n"),
	}, nil, "/tmp/go-check.log", ""))

	if !strings.Contains(message, "Full output was written to `/tmp/go-check.log`.") {
		t.Fatalf("expected message to reference full output path, got:\n%s", message)
	}
	if !strings.Contains(message, "Last 15 lines:") {
		t.Fatalf("expected message to include the truncated output tail, got:\n%s", message)
	}
	if !strings.Contains(message, outputLines[5]) {
		t.Fatalf("expected message to include the first tailed line %q, got:\n%s", outputLines[5], message)
	}
	if !strings.Contains(message, outputLines[len(outputLines)-1]) {
		t.Fatalf("expected message to include the last tailed line %q, got:\n%s", outputLines[len(outputLines)-1], message)
	}
	if strings.Contains(message, outputLines[4]) {
		t.Fatalf("did not expect message to include lines before the tail, got:\n%s", message)
	}
}

func TestBuildHookFailureMessageMetadata_UsesTailForTruncatedOutput(t *testing.T) {
	var outputLines []string
	for i := 1; i <= 20; i++ {
		outputLines = append(outputLines, strings.Repeat("y", 300)+" line "+strconv.Itoa(i))
	}

	meta := buildHookFailureMessageMetadata(HookResult{
		Hook: Hook{
			Name: "Go Check",
		},
		ExitCode: 1,
		Output:   strings.Join(outputLines, "\n"),
	}, nil, "/tmp/go-check.log", "")

	if !meta.OutputTruncated {
		t.Fatal("expected output to be marked truncated")
	}
	if meta.OutputPath != "/tmp/go-check.log" {
		t.Fatalf("output path = %q, want %q", meta.OutputPath, "/tmp/go-check.log")
	}
	if meta.OutputTail != strings.Join(outputLines[5:], "\n") {
		t.Fatalf("output tail = %q, want %q", meta.OutputTail, strings.Join(outputLines[5:], "\n"))
	}
	if meta.Output != "" {
		t.Fatalf("expected inline output to be empty for truncated output, got %q", meta.Output)
	}
}

func TestBuildHookFailureMessageMetadata_IncludesHookPath(t *testing.T) {
	meta := buildHookFailureMessageMetadata(HookResult{
		Hook: Hook{
			Name:    "Go Check",
			Path:    ".claude/hooks/go-check.sh",
			Pattern: "*.go",
		},
		ExitCode: 1,
	}, []string{"main.go"}, "/tmp/go-check.log", "")

	if meta.HookPath != ".claude/hooks/go-check.sh" {
		t.Fatalf("hook path = %q, want %q", meta.HookPath, ".claude/hooks/go-check.sh")
	}
}

func TestBuildHookFailureMessageMetadata_RelativizesAbsoluteHookPath(t *testing.T) {
	// Use t.TempDir() so workspaceRoot is a real absolute path on all platforms
	// (e.g. C:\... on Windows, /tmp/... on Unix). A hardcoded Unix path like
	// "/workspace" is not considered absolute on Windows (no drive letter), which
	// causes filepath.IsAbs to return false and the relativization to be skipped.
	workspaceRoot := t.TempDir()
	meta := buildHookFailureMessageMetadata(HookResult{
		Hook: Hook{
			Name:    "Go Check",
			Path:    filepath.Join(workspaceRoot, ".discobot", "hooks", "go-check.sh"),
			Pattern: "*.go",
		},
		ExitCode: 1,
	}, []string{"main.go"}, "/tmp/go-check.log", workspaceRoot)

	if meta.HookPath != ".discobot/hooks/go-check.sh" {
		t.Fatalf("hook path = %q, want %q", meta.HookPath, ".discobot/hooks/go-check.sh")
	}
}

func TestRerunHook_FailureWithNotifyLLMReturnsReprompt(t *testing.T) {
	testHomeDir := t.TempDir()
	t.Setenv("HOME", testHomeDir)        // Unix
	t.Setenv("USERPROFILE", testHomeDir) // Windows
	workspaceRoot := t.TempDir()
	hooksDir := filepath.Join(workspaceRoot, HooksDir)
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}

	hookPath := filepath.Join(hooksDir, "go-check.sh")
	hookSource := `#!/bin/bash
#---
# name: Go Check
# type: file
# pattern: "*.go"
#---
echo "lint failed"
exit 1
`
	if err := os.WriteFile(hookPath, []byte(hookSource), 0o755); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	mgr := NewManager(workspaceRoot, "session-123")
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}

	runResult, err := mgr.RerunHook("go-check")
	if err != nil {
		t.Fatalf("RerunHook() failed: %v", err)
	}
	if runResult == nil {
		t.Fatal("expected rerun result")
	}
	if runResult.Result.Success {
		t.Fatal("expected rerun to fail")
	}
	if !runResult.Eval.ShouldReprompt {
		t.Fatal("expected rerun failure to request reprompt")
	}
	if runResult.Eval.HookFailure == nil {
		t.Fatal("expected rerun failure metadata")
	}
	if runResult.Eval.HookFailure.HookName != "Go Check" {
		t.Fatalf("hook name = %q, want %q", runResult.Eval.HookFailure.HookName, "Go Check")
	}
	if !strings.Contains(runResult.Eval.LLMMessage, "### Hook failed: Go Check") {
		t.Fatalf("expected rerun failure message, got: %s", runResult.Eval.LLMMessage)
	}
}
