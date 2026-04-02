package hooks

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

func TestRecoverInterruptedHooks_NoOpWithoutRunning(t *testing.T) {
	hooksDataDir := t.TempDir()
	original := StatusFile{
		Hooks: map[string]HookRunStatus{
			"passed-hook": {
				HookID:              "passed-hook",
				HookName:            "Passed Hook",
				Type:                string(HookTypeFile),
				LastRunAt:           "2026-03-07T23:00:00Z",
				LastResult:          "success",
				LastExitCode:        0,
				OutputPath:          "/tmp/passed.log",
				RunCount:            4,
				FailCount:           1,
				ConsecutiveFailures: 0,
			},
		},
		PendingHooks:    []string{"queued-hook"},
		LastEvaluatedAt: "2026-03-07T23:05:00Z",
	}
	if err := SaveStatus(hooksDataDir, original); err != nil {
		t.Fatalf("SaveStatus() failed: %v", err)
	}

	if err := RecoverInterruptedHooks(hooksDataDir, []string{"passed-hook"}); err != nil {
		t.Fatalf("RecoverInterruptedHooks() failed: %v", err)
	}

	got := LoadStatus(hooksDataDir)
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("status changed unexpectedly: got %#v want %#v", got, original)
	}
}

func TestRecoverInterruptedHooks_ResetsRunningHooksAndRequeuesFileHooks(t *testing.T) {
	hooksDataDir := t.TempDir()
	status := StatusFile{
		Hooks: map[string]HookRunStatus{
			"file-hook": {
				HookID:              "file-hook",
				HookName:            "File Hook",
				Type:                string(HookTypeFile),
				LastRunAt:           "2026-03-07T23:10:00Z",
				LastResult:          "running",
				LastExitCode:        17,
				OutputPath:          "/tmp/file-hook.log",
				RunCount:            3,
				FailCount:           2,
				ConsecutiveFailures: 2,
			},
			"pre-commit-hook": {
				HookID:              "pre-commit-hook",
				HookName:            "Pre-commit Hook",
				Type:                string(HookTypePreCommit),
				LastRunAt:           "2026-03-07T23:11:00Z",
				LastResult:          "running",
				LastExitCode:        9,
				OutputPath:          "/tmp/pre-commit-hook.log",
				RunCount:            5,
				FailCount:           1,
				ConsecutiveFailures: 1,
			},
		},
		PendingHooks:    nil,
		LastEvaluatedAt: "2026-03-07T23:12:00Z",
	}
	if err := SaveStatus(hooksDataDir, status); err != nil {
		t.Fatalf("SaveStatus() failed: %v", err)
	}

	if err := RecoverInterruptedHooks(hooksDataDir, []string{"file-hook"}); err != nil {
		t.Fatalf("RecoverInterruptedHooks() failed: %v", err)
	}
	if err := RecoverInterruptedHooks(hooksDataDir, []string{"file-hook"}); err != nil {
		t.Fatalf("RecoverInterruptedHooks() second call failed: %v", err)
	}

	got := LoadStatus(hooksDataDir)
	if !slices.Equal(got.PendingHooks, []string{"file-hook"}) {
		t.Fatalf("pendingHooks mismatch: got %v want %v", got.PendingHooks, []string{"file-hook"})
	}

	fileHook := got.Hooks["file-hook"]
	if fileHook.LastResult != "pending" {
		t.Fatalf("file hook lastResult = %q, want pending", fileHook.LastResult)
	}
	if fileHook.LastExitCode != 17 || fileHook.OutputPath != "/tmp/file-hook.log" {
		t.Fatalf("file hook metadata changed unexpectedly: %#v", fileHook)
	}
	if fileHook.RunCount != 3 || fileHook.FailCount != 2 || fileHook.ConsecutiveFailures != 2 {
		t.Fatalf("file hook counters changed unexpectedly: %#v", fileHook)
	}

	preCommitHook := got.Hooks["pre-commit-hook"]
	if preCommitHook.LastResult != "pending" {
		t.Fatalf("pre-commit hook lastResult = %q, want pending", preCommitHook.LastResult)
	}
	if preCommitHook.LastExitCode != 9 || preCommitHook.OutputPath != "/tmp/pre-commit-hook.log" {
		t.Fatalf("pre-commit hook metadata changed unexpectedly: %#v", preCommitHook)
	}
	if preCommitHook.RunCount != 5 || preCommitHook.FailCount != 1 || preCommitHook.ConsecutiveFailures != 1 {
		t.Fatalf("pre-commit hook counters changed unexpectedly: %#v", preCommitHook)
	}
}

func TestManagerInit_RecoversInterruptedFileHooksOnStartup(t *testing.T) {
	homeDir := t.TempDir()
	workspaceRoot := filepath.Join(t.TempDir(), "workspace")
	t.Setenv("HOME", homeDir)        // Unix
	t.Setenv("USERPROFILE", homeDir) // Windows

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
exit 0
`
	if err := os.WriteFile(hookPath, []byte(hookSource), 0o755); err != nil {
		t.Fatalf("WriteFile() failed: %v", err)
	}

	hooksDataDir := GetHooksDataDir("session-123")
	status := StatusFile{
		Hooks: map[string]HookRunStatus{
			"go-check": {
				HookID:              "go-check",
				HookName:            "Go Check",
				Type:                string(HookTypeFile),
				LastRunAt:           "2026-03-07T23:20:00Z",
				LastResult:          "running",
				LastExitCode:        11,
				OutputPath:          "/tmp/go-check.log",
				RunCount:            2,
				FailCount:           1,
				ConsecutiveFailures: 1,
			},
		},
	}
	if err := SaveStatus(hooksDataDir, status); err != nil {
		t.Fatalf("SaveStatus() failed: %v", err)
	}

	mgr := NewManager(workspaceRoot, "session-123")
	if err := mgr.Init(); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}

	got := LoadStatus(hooksDataDir)
	if got.Hooks["go-check"].LastResult != "pending" {
		t.Fatalf("go-check lastResult = %q, want pending", got.Hooks["go-check"].LastResult)
	}
	if !slices.Equal(got.PendingHooks, []string{"go-check"}) {
		t.Fatalf("pendingHooks mismatch: got %v want %v", got.PendingHooks, []string{"go-check"})
	}
}

func TestAddPendingHooks_SortsHookIDsAlphaNumerically(t *testing.T) {
	hooksDataDir := t.TempDir()

	if err := AddPendingHooks(hooksDataDir, []string{"hook-2", "hook-10", "hook-1", "hook-2"}); err != nil {
		t.Fatalf("AddPendingHooks() failed: %v", err)
	}

	got := GetPendingHookIDs(hooksDataDir)
	want := []string{"hook-1", "hook-10", "hook-2"}
	if !slices.Equal(got, want) {
		t.Fatalf("pendingHooks mismatch: got %v want %v", got, want)
	}
}
