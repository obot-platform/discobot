package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/thread"
)

func TestExecuteEnterPlanMode_SetsPlanMode(t *testing.T) {
	dataDir := t.TempDir()
	e := New(t.TempDir(), dataDir, "thread-1")
	toolCtx := &thread.ToolContext{ThreadID: "thread-1"}

	result, err := e.Execute(context.Background(), toolCtx, message.ToolCallPart{
		ToolCallID: "tc-enter",
		ToolName:   "EnterPlanMode",
		Input:      "{}",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Approval != nil {
		t.Fatal("EnterPlanMode should not require approval")
	}
	if !toolCtx.PlanMode {
		t.Fatal("expected PlanMode=true after EnterPlanMode")
	}

	textOut, ok := result.Result.Output.(message.TextOutput)
	if !ok {
		t.Fatalf("expected TextOutput result, got %T", result.Result.Output)
	}
	prefix := "Plan mode activated. Plan file: "
	if !strings.HasPrefix(textOut.Value, prefix) {
		t.Fatalf("expected plan file prefix in output, got %q", textOut.Value)
	}
	firstLine := strings.SplitN(textOut.Value, "\n", 2)[0]
	planFile := strings.TrimPrefix(firstLine, prefix)
	if filepath.Ext(planFile) != ".md" {
		t.Fatalf("expected markdown plan file, got %q", planFile)
	}
	expectedPrefix := filepath.Join(dataDir, "plans", "thread-1") + string(filepath.Separator)
	if !strings.HasPrefix(planFile, expectedPrefix) {
		t.Fatalf("expected plan file under %q, got %q", expectedPrefix, planFile)
	}
	if strings.Contains(filepath.Base(planFile), " ") {
		t.Fatalf("expected LLM-friendly filename with no spaces, got %q", filepath.Base(planFile))
	}
}

func TestExecuteExitPlanMode_AutoApprovesWhenPromptRequestNotPlan(t *testing.T) {
	dataDir := t.TempDir()
	threadID := "thread-1"
	planDir := filepath.Join(dataDir, "plans", threadID)
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planDir, "auto-plan.md"), []byte("## Auto plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := New(t.TempDir(), dataDir, threadID)
	toolCtx := &thread.ToolContext{
		ThreadID:              threadID,
		PlanMode:              true,
		PromptRequestPlanMode: false,
	}

	result, err := e.Execute(context.Background(), toolCtx, message.ToolCallPart{
		ToolCallID: "tc-exit-auto",
		ToolName:   "ExitPlanMode",
		Input:      "{}",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Approval != nil {
		t.Fatal("expected ExitPlanMode to skip approval when prompt request mode was not plan")
	}
	if toolCtx.PlanMode {
		t.Fatal("expected PlanMode=false after auto-approved ExitPlanMode")
	}
	if toolCtx.ModeChange == nil || *toolCtx.ModeChange != "build" {
		t.Fatalf("expected ModeChange=build, got %v", toolCtx.ModeChange)
	}

	textOut, ok := result.Result.Output.(message.TextOutput)
	if !ok {
		t.Fatalf("expected TextOutput result, got %T", result.Result.Output)
	}
	if !strings.Contains(textOut.Value, "Plan mode exited") {
		t.Fatalf("expected auto-exit confirmation in output, got %q", textOut.Value)
	}
}

func TestExecuteExitPlanMode_RequiresApprovalWhenPromptRequestPlan(t *testing.T) {
	dataDir := t.TempDir()
	threadID := "thread-1"
	planDir := filepath.Join(dataDir, "plans", threadID)
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planDir, "manual-plan.md"), []byte("## Manual plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := New(t.TempDir(), dataDir, threadID)
	toolCtx := &thread.ToolContext{
		ThreadID:              threadID,
		PlanMode:              true,
		PromptRequestPlanMode: true,
	}

	result, err := e.Execute(context.Background(), toolCtx, message.ToolCallPart{
		ToolCallID: "tc-exit-manual",
		ToolName:   "ExitPlanMode",
		Input:      "{}",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result.Approval == nil {
		t.Fatal("expected ExitPlanMode to request approval when prompt request mode was plan")
	}
	if !toolCtx.PlanMode {
		t.Fatal("expected PlanMode to remain true while waiting for approval")
	}
	if toolCtx.ModeChange != nil {
		t.Fatalf("expected ModeChange to be nil before approval, got %v", toolCtx.ModeChange)
	}

	var questions []map[string]any
	if err := json.Unmarshal(result.Approval.Questions, &questions); err != nil {
		t.Fatalf("failed to parse approval questions: %v", err)
	}
	if len(questions) != 1 {
		t.Fatalf("expected 1 approval question, got %d", len(questions))
	}
	if questions[0]["header"] != "Plan approval" {
		t.Fatalf("expected Plan approval header, got %v", questions[0]["header"])
	}
}
