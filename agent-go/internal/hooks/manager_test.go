package hooks

import (
	"os"
	"path/filepath"
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
	}, []string{"main.go", "internal/app.go"}, "/tmp/go-check.log"))

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
	message := formatHookFailureMessage(buildHookFailureMessageMetadata(HookResult{
		Hook: Hook{
			Name: "Go Check",
		},
		ExitCode: 1,
		Output:   strings.Repeat("x", InlineOutputMaxBytes+1),
	}, nil, "/tmp/go-check.log"))

	if !strings.Contains(message, "Full output was written to `/tmp/go-check.log`.") {
		t.Fatalf("expected message to reference full output path, got:\n%s", message)
	}
	if strings.Contains(message, "```text") {
		t.Fatalf("did not expect inline code fence when output is too large, got:\n%s", message)
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
	}, []string{"main.go"}, "/tmp/go-check.log")

	if meta.HookPath != ".claude/hooks/go-check.sh" {
		t.Fatalf("hook path = %q, want %q", meta.HookPath, ".claude/hooks/go-check.sh")
	}
}

func TestRerunHook_FailureWithNotifyLLMReturnsReprompt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
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
