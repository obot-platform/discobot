package hooks

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/obot-platform/discobot/agent-go/message"
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
	if !strings.Contains(runResult.Result.Output, "lint failed") {
		t.Fatalf("expected hook output to contain %q, got %q", "lint failed", runResult.Result.Output)
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

func TestEmitCurrentStatusChunk_EmitsHooksStatusDataChunk(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	mgr := NewManager(t.TempDir(), "session-123")

	status := StatusFile{
		Hooks: map[string]HookRunStatus{
			"go-check": {
				HookID:       "go-check",
				HookName:     "Go Check",
				Type:         string(HookTypeFile),
				LastRunAt:    "2026-03-31T00:00:00Z",
				LastResult:   "pending",
				LastExitCode: 0,
				OutputPath:   "/tmp/go-check.log",
			},
		},
		PendingHooks:    []string{"go-check"},
		LastEvaluatedAt: "2026-03-31T00:00:00Z",
	}
	if err := SaveStatus(mgr.hooksDataDir, status); err != nil {
		t.Fatalf("SaveStatus() failed: %v", err)
	}

	var gotChunk message.MessageChunk
	mgr.SetChunkEmitter(func(chunk message.MessageChunk) {
		gotChunk = chunk
	})
	if gotChunk != nil {
		t.Fatalf("expected no initial chunk replay, got %#v", gotChunk)
	}

	mgr.emitCurrentStatusChunk()

	dataChunk, ok := gotChunk.(message.DataChunk)
	if !ok {
		t.Fatalf("expected DataChunk, got %T", gotChunk)
	}
	if dataChunk.DataType != "hooks-status" {
		t.Fatalf("data type = %q, want hooks-status", dataChunk.DataType)
	}

	var gotStatus StatusFile
	if err := json.Unmarshal(dataChunk.Data, &gotStatus); err != nil {
		t.Fatalf("json.Unmarshal() failed: %v", err)
	}
	if gotStatus.Hooks["go-check"].LastResult != "pending" {
		t.Fatalf("hook result = %q, want pending", gotStatus.Hooks["go-check"].LastResult)
	}
}

func TestEvaluateFileHooks_RunsLowestPendingHookIDFirst(t *testing.T) {
	homeDir := t.TempDir()
	workspaceRoot := t.TempDir()
	t.Setenv("HOME", homeDir)        // Unix
	t.Setenv("USERPROFILE", homeDir) // Windows

	cmd := exec.Command("git", "init")
	cmd.Dir = workspaceRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v\n%s", err, out)
	}

	hooksDir := filepath.Join(workspaceRoot, HooksDir)
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() failed: %v", err)
	}

	orderLog := filepath.Join(workspaceRoot, "hook-order.log")
	firstHookPath := filepath.Join(hooksDir, "01-first.sh")
	firstHook := `#!/bin/bash
#---
# name: Zulu First
# type: file
# pattern: "*.txt"
#---
echo "01-first" >> "hook-order.log"
exit 1
`
	if err := os.WriteFile(firstHookPath, []byte(firstHook), 0o755); err != nil {
		t.Fatalf("WriteFile(firstHook) failed: %v", err)
	}

	secondHookPath := filepath.Join(hooksDir, "02-second.sh")
	secondHook := `#!/bin/bash
#---
# name: Alpha Second
# type: file
# pattern: "*.txt"
#---
echo "02-second" >> "hook-order.log"
exit 0
`
	if err := os.WriteFile(secondHookPath, []byte(secondHook), 0o755); err != nil {
		t.Fatalf("WriteFile(secondHook) failed: %v", err)
	}

	if err := os.WriteFile(filepath.Join(workspaceRoot, "pending.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(pending.txt) failed: %v", err)
	}

	mgr := NewManager(workspaceRoot, "session-123")
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}

	eval := mgr.EvaluateFileHooks()
	if !eval.ShouldReprompt {
		t.Fatal("expected first pending hook failure to request reprompt")
	}
	if eval.HookFailure == nil {
		t.Fatal("expected hook failure metadata")
	}
	if eval.HookFailure.HookName != "Zulu First" {
		t.Fatalf("hook failure name = %q, want %q", eval.HookFailure.HookName, "Zulu First")
	}

	orderData, err := os.ReadFile(orderLog)
	if err != nil {
		t.Fatalf("ReadFile(orderLog) failed: %v", err)
	}
	if string(orderData) != "01-first\n" {
		t.Fatalf("hook execution order = %q, want only first hook", string(orderData))
	}

	status := LoadStatus(mgr.hooksDataDir)
	if strings.Join(status.PendingHooks, ",") != "01-first,02-second" {
		t.Fatalf("pendingHooks = %v, want [01-first 02-second]", status.PendingHooks)
	}
}
