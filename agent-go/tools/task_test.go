package tools

import (
	"context"
	"encoding/json"
	"iter"
	"testing"
	"time"

	"github.com/obot-platform/discobot/agent-go/agent"
	"github.com/obot-platform/discobot/agent-go/internal/api"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
	"github.com/obot-platform/discobot/agent-go/thread"
)

// --- Mock sub-agent ---

type mockSubAgent struct {
	promptFn        func(ctx context.Context, threadID string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error]
	finalResponseFn func(threadID string) (string, error)
}

func (m *mockSubAgent) Prompt(ctx context.Context, threadID string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
	if m.promptFn != nil {
		return m.promptFn(ctx, threadID, req)
	}
	return func(_ func(message.MessageChunk, error) bool) {}
}

func (m *mockSubAgent) Cancel(_ string) bool                                        { return false }
func (m *mockSubAgent) Messages(_, _ string) ([]message.UIMessage, error)           { return nil, nil }
func (m *mockSubAgent) ListModels(_ context.Context) ([]providers.ModelInfo, error) { return nil, nil }
func (m *mockSubAgent) ListThreads() ([]string, error)                              { return nil, nil }
func (m *mockSubAgent) HasInterruptedTurn(string) (bool, error)                     { return false, nil }
func (m *mockSubAgent) PendingQuestion(_ string) (*agent.PendingQuestion, error) {
	return nil, nil
}
func (m *mockSubAgent) SubmitAnswer(_, _ string, _ api.AnswerQuestionRequest) error {
	return nil
}
func (m *mockSubAgent) ListCommands() ([]agent.Command, error) { return nil, nil }
func (m *mockSubAgent) IsLeaf(_, _ string) (bool, error)       { return true, nil }
func (m *mockSubAgent) FinalResponse(threadID string) (string, error) {
	if m.finalResponseFn != nil {
		return m.finalResponseFn(threadID)
	}
	return "", nil
}

// --- Helpers ---

func makeTaskInput(t *testing.T, prompt string) json.RawMessage {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"prompt": prompt})
	return raw
}

func makeTaskCall(t *testing.T, prompt string) message.ToolCallPart {
	t.Helper()
	return message.ToolCallPart{
		ToolCallID: t.Name() + "-tc",
		ToolName:   "Task",
		Input:      string(makeTaskInput(t, prompt)),
	}
}

func waitHandle(t *testing.T, handle *thread.AsyncTaskHandle, timeout time.Duration) message.ToolResultPart {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	res, err := handle.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if res.Approval != nil {
		t.Fatalf("Wait returned unexpected approval: %#v", res.Approval)
	}
	return res.Result
}

func textOutput(res message.ToolResultPart) string {
	if out, ok := res.Output.(message.TextOutput); ok {
		return out.Value
	}
	return ""
}

func isErrorOutput(res message.ToolResultPart) bool {
	_, ok := res.Output.(message.ErrorTextOutput)
	return ok
}

// cleanupTask removes a task from globalTasks so tests don't interfere.
func cleanupTask(taskID string) {
	globalTasks.mu.Lock()
	delete(globalTasks.tasks, taskID)
	globalTasks.mu.Unlock()
}

// --- Tests ---

// TestTask_BasicSubAgent verifies that Execute("Task") launches the sub-agent and
// returns its FinalResponse output once the goroutine completes.
func TestTask_BasicSubAgent(t *testing.T) {
	const want = "sub-agent result: all done"

	subAgent := &mockSubAgent{
		promptFn: func(_ context.Context, _ string, _ agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			// No streaming chunks needed; FinalResponse provides the output.
			return func(_ func(message.MessageChunk, error) bool) {}
		},
		finalResponseFn: func(_ string) (string, error) {
			return want, nil
		},
	}

	exec := New(t.TempDir(), t.TempDir(), "parent-thread")
	toolCtx := &thread.ToolContext{ThreadID: "parent-thread", Agent: subAgent}

	result, err := exec.Execute(context.Background(), toolCtx, makeTaskCall(t, "do something"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Async == nil {
		t.Fatal("expected Async handle, got nil")
	}
	t.Cleanup(func() { cleanupTask(result.Async.TaskID) })

	res := waitHandle(t, result.Async, 5*time.Second)
	if got := textOutput(res); got != want {
		t.Errorf("output: got %q, want %q", got, want)
	}
}

// TestTask_NoSubAgent verifies that Execute("Task") returns an error result when no
// sub-agent is provided in ToolContext.
func TestTask_NoSubAgent(t *testing.T) {
	exec := New(t.TempDir(), t.TempDir(), "parent-thread")
	toolCtx := &thread.ToolContext{ThreadID: "parent-thread"}

	result, err := exec.Execute(context.Background(), toolCtx, makeTaskCall(t, "do something"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Async != nil {
		t.Error("expected no Async handle when sub-agent not configured")
	}
	if !isErrorOutput(result.Result) {
		t.Errorf("expected ErrorTextOutput, got %T", result.Result.Output)
	}
}

// TestTask_ForwardsPromptAndSubagentType verifies that the prompt text and
// subagent_type from the tool input are forwarded to the sub-agent's Prompt call.
func TestTask_ForwardsPromptAndSubagentType(t *testing.T) {
	const wantPrompt = "summarise the logs"
	const wantType = "log-analyst"

	var gotPrompt, gotType string
	subAgent := &mockSubAgent{
		promptFn: func(_ context.Context, _ string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			if len(req.UserParts) > 0 {
				if tp, ok := req.UserParts[0].(message.UITextPart); ok {
					gotPrompt = tp.Text
				}
			}
			gotType = req.SubagentType
			return func(_ func(message.MessageChunk, error) bool) {}
		},
		finalResponseFn: func(_ string) (string, error) { return "ok", nil },
	}

	exec := New(t.TempDir(), t.TempDir(), "parent-thread")
	toolCtx := &thread.ToolContext{ThreadID: "parent-thread", Agent: subAgent}

	raw, _ := json.Marshal(map[string]string{
		"prompt":        wantPrompt,
		"subagent_type": wantType,
	})
	call := message.ToolCallPart{
		ToolCallID: t.Name() + "-tc",
		ToolName:   "Task",
		Input:      string(raw),
	}

	result, err := exec.Execute(context.Background(), toolCtx, call)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Async == nil {
		t.Fatal("expected Async handle")
	}
	t.Cleanup(func() { cleanupTask(result.Async.TaskID) })

	waitHandle(t, result.Async, 5*time.Second)

	if gotPrompt != wantPrompt {
		t.Errorf("prompt: got %q, want %q", gotPrompt, wantPrompt)
	}
	if gotType != wantType {
		t.Errorf("subagent_type: got %q, want %q", gotType, wantType)
	}
}

// TestTask_Cancellation verifies that cancelling the Wait context propagates
// cancellation to the sub-agent goroutine via rec.cancel.
func TestTask_Cancellation(t *testing.T) {
	agentCancelled := make(chan struct{})

	subAgent := &mockSubAgent{
		promptFn: func(ctx context.Context, _ string, _ agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			return func(_ func(message.MessageChunk, error) bool) {
				<-ctx.Done()
				close(agentCancelled)
			}
		},
		finalResponseFn: func(_ string) (string, error) { return "", nil },
	}

	exec := New(t.TempDir(), t.TempDir(), "parent-thread")
	toolCtx := &thread.ToolContext{ThreadID: "parent-thread", Agent: subAgent}

	result, err := exec.Execute(context.Background(), toolCtx, makeTaskCall(t, "long running task"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Async == nil {
		t.Fatal("expected Async handle")
	}
	t.Cleanup(func() { cleanupTask(result.Async.TaskID) })

	// Cancel the Wait context after a brief delay so the goroutine has started.
	waitCtx, waitCancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		waitCancel()
	}()

	res, err := result.Async.Wait(waitCtx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !isErrorOutput(res.Result) {
		t.Errorf("expected ErrorTextOutput (cancelled), got %T", res.Result.Output)
	}

	// The sub-agent goroutine should have been cancelled too.
	select {
	case <-agentCancelled:
		// good
	case <-time.After(2 * time.Second):
		t.Error("sub-agent goroutine was not cancelled after Wait context was done")
	}
}

// TestTask_CancellationBeforeGoroutineStarts verifies there is no race between
// creating the cancel func and a very early cancellation. The context is created
// before the goroutine starts, so rec.cancel is always set.
func TestTask_CancellationBeforeGoroutineStarts(t *testing.T) {
	agentCancelled := make(chan struct{})

	subAgent := &mockSubAgent{
		promptFn: func(ctx context.Context, _ string, _ agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			return func(_ func(message.MessageChunk, error) bool) {
				<-ctx.Done()
				close(agentCancelled)
			}
		},
		finalResponseFn: func(_ string) (string, error) { return "", nil },
	}

	exec := New(t.TempDir(), t.TempDir(), "parent-thread")
	toolCtx := &thread.ToolContext{ThreadID: "parent-thread", Agent: subAgent}

	// Cancel the wait context immediately — before even calling Execute.
	waitCtx, waitCancel := context.WithCancel(context.Background())
	waitCancel()

	result, err := exec.Execute(context.Background(), toolCtx, makeTaskCall(t, "instant cancel"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Async == nil {
		t.Fatal("expected Async handle")
	}
	t.Cleanup(func() { cleanupTask(result.Async.TaskID) })

	res, err := result.Async.Wait(waitCtx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !isErrorOutput(res.Result) {
		t.Errorf("expected ErrorTextOutput (cancelled), got %T", res.Result.Output)
	}

	select {
	case <-agentCancelled:
		// good
	case <-time.After(2 * time.Second):
		t.Error("sub-agent goroutine was not cancelled")
	}
}

// TestTask_Resumption_InMemory verifies that ResumeAsync returns a handle backed
// by the original in-memory taskRecord when the process has not crashed (fast path).
func TestTask_Resumption_InMemory(t *testing.T) {
	const want = "in-memory result"

	subAgent := &mockSubAgent{
		promptFn: func(_ context.Context, _ string, _ agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			return func(_ func(message.MessageChunk, error) bool) {}
		},
		finalResponseFn: func(_ string) (string, error) { return want, nil },
	}

	exec := New(t.TempDir(), t.TempDir(), "parent-thread")
	toolCtx := &thread.ToolContext{ThreadID: "parent-thread", Agent: subAgent}

	// Launch the task.
	result, err := exec.Execute(context.Background(), toolCtx, makeTaskCall(t, "some work"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Async == nil {
		t.Fatal("expected Async handle")
	}
	taskID := result.Async.TaskID
	t.Cleanup(func() { cleanupTask(taskID) })

	// Wait for the goroutine to complete so the record is in "completed" state.
	waitHandle(t, result.Async, 5*time.Second)

	// ResumeAsync should find the record still in globalTasks and return a handle.
	call := makeTaskCall(t, "some work")
	resumed, err := exec.ResumeAsync(context.Background(), toolCtx, call, taskID, nil)
	if err != nil {
		t.Fatalf("ResumeAsync: %v", err)
	}

	// Either a completed sync result or an async handle that resolves immediately.
	var res message.ToolResultPart
	if resumed.Async != nil {
		res = waitHandle(t, resumed.Async, 5*time.Second)
	} else {
		res = resumed.Result
	}
	if got := textOutput(res); got != want {
		t.Errorf("output: got %q, want %q", got, want)
	}
}

// TestTask_Resumption_AlreadyCompleted simulates a process crash where the
// sub-agent thread finished before the crash. ResumeAsync should return the
// completed result synchronously via FinalResponse without restarting a goroutine.
func TestTask_Resumption_AlreadyCompleted(t *testing.T) {
	const want = "sub-agent already finished"

	subAgent := &mockSubAgent{
		// FinalResponse immediately returns the result — sub-agent was done.
		finalResponseFn: func(_ string) (string, error) { return want, nil },
	}

	exec := New(t.TempDir(), t.TempDir(), "parent-thread")
	toolCtx := &thread.ToolContext{ThreadID: "parent-thread", Agent: subAgent}

	// Use a task ID that is NOT in globalTasks (simulating a crash recovery).
	taskID := "crashed-completed-" + t.Name()

	call := makeTaskCall(t, "task that finished before crash")
	call.ToolCallID = t.Name() + "-recover"

	result, err := exec.ResumeAsync(context.Background(), toolCtx, call, taskID, nil)
	if err != nil {
		t.Fatalf("ResumeAsync: %v", err)
	}

	var res message.ToolResultPart
	if result.Async != nil {
		// Acceptable: implementation chose to wrap in async handle.
		t.Cleanup(func() { cleanupTask(result.Async.TaskID) })
		res = waitHandle(t, result.Async, 5*time.Second)
	} else {
		res = result.Result
	}

	if got := textOutput(res); got != want {
		t.Errorf("output: got %q, want %q", got, want)
	}
}

// TestTask_Resumption_MidTurn simulates a process crash where the sub-agent was
// mid-turn. ResumeAsync should restart the goroutine; DefaultAgent.Prompt detects
// the interrupted turn state and resumes it. Here we verify the goroutine restarts
// and the final output is delivered.
func TestTask_Resumption_MidTurn(t *testing.T) {
	const want = "resumed and completed"

	calls := 0
	subAgent := &mockSubAgent{
		promptFn: func(_ context.Context, _ string, _ agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			// Simulates a resumed turn completing successfully.
			return func(_ func(message.MessageChunk, error) bool) {}
		},
		finalResponseFn: func(_ string) (string, error) {
			calls++
			if calls == 1 {
				// First call: inside resumeTask's completion check —
				// return "" to indicate the sub-agent was mid-turn when crashed.
				return "", nil
			}
			// Subsequent calls: goroutine ran, sub-agent is done.
			return want, nil
		},
	}

	exec := New(t.TempDir(), t.TempDir(), "parent-thread")
	toolCtx := &thread.ToolContext{ThreadID: "parent-thread", Agent: subAgent}

	taskID := "crashed-midturn-" + t.Name()

	call := makeTaskCall(t, "task interrupted mid-turn")
	call.ToolCallID = t.Name() + "-midturn"

	result, err := exec.ResumeAsync(context.Background(), toolCtx, call, taskID, nil)
	if err != nil {
		t.Fatalf("ResumeAsync: %v", err)
	}
	if result.Async == nil {
		t.Fatal("expected Async handle for mid-turn recovery")
	}
	t.Cleanup(func() { cleanupTask(result.Async.TaskID) })

	res := waitHandle(t, result.Async, 5*time.Second)
	if got := textOutput(res); got != want {
		t.Errorf("output: got %q, want %q", got, want)
	}
}

// TestTask_SubThreadIDScheme verifies that the sub-thread ID is constructed as
// "<parentThreadID>.sub.<taskID>" so crash-recovery can reconstruct it.
func TestTask_SubThreadIDScheme(t *testing.T) {
	const parentThreadID = "parent-abc"
	var capturedSubThreadID string

	subAgent := &mockSubAgent{
		promptFn: func(_ context.Context, threadID string, _ agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			capturedSubThreadID = threadID
			return func(_ func(message.MessageChunk, error) bool) {}
		},
		finalResponseFn: func(threadID string) (string, error) {
			return "done: " + threadID, nil
		},
	}

	exec := New(t.TempDir(), t.TempDir(), parentThreadID)
	toolCtx := &thread.ToolContext{ThreadID: parentThreadID, Agent: subAgent}

	result, err := exec.Execute(context.Background(), toolCtx, makeTaskCall(t, "thread id test"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Async == nil {
		t.Fatal("expected Async handle")
	}
	taskID := result.Async.TaskID
	t.Cleanup(func() { cleanupTask(taskID) })

	waitHandle(t, result.Async, 5*time.Second)

	wantPrefix := parentThreadID + ".sub."
	if len(capturedSubThreadID) <= len(wantPrefix) || capturedSubThreadID[:len(wantPrefix)] != wantPrefix {
		t.Errorf("sub-thread ID %q does not start with %q", capturedSubThreadID, wantPrefix)
	}
	if got := capturedSubThreadID[len(wantPrefix):]; got != taskID {
		t.Errorf("sub-thread ID suffix: got %q, want taskID %q", got, taskID)
	}
}
