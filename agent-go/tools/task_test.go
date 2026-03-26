package tools

import (
	"context"
	"encoding/json"
	"iter"
	"sync"
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
	promptFn          func(ctx context.Context, threadID string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error]
	finalResponseFn   func(threadID string) (string, error)
	pendingQuestionFn func(threadID string) (*agent.PendingQuestion, error)
	submitAnswerFn    func(threadID, approvalID string, req api.AnswerQuestionRequest) error
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
func (m *mockSubAgent) PendingQuestion(threadID string) (*agent.PendingQuestion, error) {
	if m.pendingQuestionFn != nil {
		return m.pendingQuestionFn(threadID)
	}
	return nil, nil
}
func (m *mockSubAgent) SubmitAnswer(threadID, approvalID string, req api.AnswerQuestionRequest) error {
	if m.submitAnswerFn != nil {
		return m.submitAnswerFn(threadID, approvalID, req)
	}
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

type recursiveTaskState struct {
	taskID        string
	pending       *agent.PendingQuestion
	pendingAnswer *api.AnswerQuestionRequest
	final         string
}

type recursiveTaskAgent struct {
	child        agent.Agent
	exec         *Executor
	questionID   string
	questionText string
	finalText    string

	mu     sync.Mutex
	states map[string]*recursiveTaskState
}

func newRecursiveTaskAgent(exec *Executor, child agent.Agent) *recursiveTaskAgent {
	return &recursiveTaskAgent{
		child:  child,
		exec:   exec,
		states: make(map[string]*recursiveTaskState),
	}
}

func (a *recursiveTaskAgent) state(threadID string) *recursiveTaskState {
	a.mu.Lock()
	defer a.mu.Unlock()
	state, ok := a.states[threadID]
	if !ok {
		state = &recursiveTaskState{}
		a.states[threadID] = state
	}
	return state
}

func (a *recursiveTaskAgent) Prompt(ctx context.Context, threadID string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
	return func(yield func(message.MessageChunk, error) bool) {
		state := a.state(threadID)
		if state.final != "" || state.pending != nil {
			return
		}

		if a.child == nil {
			if a.questionText != "" {
				state.pending = &agent.PendingQuestion{
					ApprovalID: a.questionID,
					Questions: []api.AskUserQuestion{{
						Question: a.questionText,
						Header:   "Deep question",
						Options: []api.AskUserQuestionOption{
							{Label: "Yes", Description: "Continue"},
							{Label: "No", Description: "Stop"},
						},
					}},
				}
				return
			}
			state.final = a.finalText
			return
		}

		call := message.ToolCallPart{
			ToolCallID: "nested-task-call",
			ToolName:   "Task",
			Input:      `{"prompt":"delegate deeper"}`,
		}
		toolCtx := &thread.ToolContext{
			ThreadID:         threadID,
			Agent:            a.child,
			SubagentDepth:    req.SubagentDepth,
			MaxSubagentDepth: 4,
			CurrentTaskID:    req.ParentTaskID,
		}

		var execResult thread.ToolExecuteResult
		var err error
		if state.taskID == "" {
			execResult, err = a.exec.Execute(ctx, toolCtx, call)
			if err == nil && execResult.Async != nil {
				state.taskID = execResult.Async.TaskID
			}
		} else if state.pendingAnswer != nil {
			execResult, err = a.exec.ResumeAsync(ctx, toolCtx, call, state.taskID, state.pendingAnswer)
			state.pendingAnswer = nil
		} else {
			execResult, err = a.exec.ResumeAsync(ctx, toolCtx, call, state.taskID, nil)
		}
		if err != nil {
			yield(nil, err)
			return
		}
		if execResult.Async == nil {
			if out, ok := execResult.Result.Output.(message.TextOutput); ok {
				state.final = out.Value
			}
			return
		}

		waitResult, err := execResult.Async.Wait(ctx)
		if err != nil {
			yield(nil, err)
			return
		}
		if waitResult.Approval != nil {
			var questions []api.AskUserQuestion
			if err := json.Unmarshal(waitResult.Approval.Questions, &questions); err != nil {
				yield(nil, err)
				return
			}
			state.pending = &agent.PendingQuestion{
				ApprovalID: "nested-approval-" + threadID,
				Questions:  questions,
			}
			return
		}
		if out, ok := waitResult.Result.Output.(message.TextOutput); ok {
			state.final = out.Value
		}
	}
}

func (a *recursiveTaskAgent) Cancel(_ string) bool                              { return false }
func (a *recursiveTaskAgent) Messages(_, _ string) ([]message.UIMessage, error) { return nil, nil }
func (a *recursiveTaskAgent) ListModels(_ context.Context) ([]providers.ModelInfo, error) {
	return nil, nil
}
func (a *recursiveTaskAgent) ListThreads() ([]string, error)        { return nil, nil }
func (a *recursiveTaskAgent) InterruptedThreads() ([]string, error) { return nil, nil }
func (a *recursiveTaskAgent) PendingQuestion(threadID string) (*agent.PendingQuestion, error) {
	return a.state(threadID).pending, nil
}
func (a *recursiveTaskAgent) SubmitAnswer(threadID, approvalID string, req api.AnswerQuestionRequest) error {
	state := a.state(threadID)
	if state.pending == nil || state.pending.ApprovalID != approvalID {
		return nil
	}
	state.pending = nil
	if a.child == nil {
		state.final = a.finalText
		return nil
	}
	state.pendingAnswer = &req
	return nil
}
func (a *recursiveTaskAgent) ListCommands() ([]agent.Command, error) { return nil, nil }
func (a *recursiveTaskAgent) IsLeaf(_, _ string) (bool, error)       { return true, nil }
func (a *recursiveTaskAgent) FinalResponse(threadID string) (string, error) {
	return a.state(threadID).final, nil
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
	res := waitAsyncResult(t, handle, timeout)
	if res.Approval != nil {
		t.Fatalf("Wait returned unexpected approval: %#v", res.Approval)
	}
	return res.Result
}

func waitAsyncResult(t *testing.T, handle *thread.AsyncTaskHandle, timeout time.Duration) thread.AsyncWaitResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	res, err := handle.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	return res
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

// TestTask_ForwardsPromptAndSubagentType verifies that the prompt text,
// subagent_type, and nesting metadata from the tool input/context are forwarded
// to the sub-agent's Prompt call.
func TestTask_ForwardsPromptAndSubagentType(t *testing.T) {
	const wantPrompt = "summarise the logs"
	const wantType = "log-analyst"

	var gotPrompt, gotType, gotParentTaskID string
	var gotDepth int
	subAgent := &mockSubAgent{
		promptFn: func(_ context.Context, _ string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			if len(req.UserParts) > 0 {
				if tp, ok := req.UserParts[0].(message.UITextPart); ok {
					gotPrompt = tp.Text
				}
			}
			gotType = req.SubagentType
			gotParentTaskID = req.ParentTaskID
			gotDepth = req.SubagentDepth
			return func(_ func(message.MessageChunk, error) bool) {}
		},
		finalResponseFn: func(_ string) (string, error) { return "ok", nil },
	}

	exec := New(t.TempDir(), t.TempDir(), "parent-thread")
	toolCtx := &thread.ToolContext{ThreadID: "parent-thread", Agent: subAgent, CurrentTaskID: "parent-task", SubagentDepth: 1, MaxSubagentDepth: 4}

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
	if gotParentTaskID == "" {
		t.Errorf("parent_task_id: got %q, want non-empty task id", gotParentTaskID)
	}
	if gotDepth != 2 {
		t.Errorf("subagent_depth: got %d, want 2", gotDepth)
	}
}

func TestTask_RejectsCallsPastMaxDepth(t *testing.T) {
	exec := New(t.TempDir(), t.TempDir(), "parent-thread")
	toolCtx := &thread.ToolContext{ThreadID: "parent-thread", SubagentDepth: 4, MaxSubagentDepth: 4}

	result, err := exec.Execute(context.Background(), toolCtx, makeTaskCall(t, "too deep"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Async != nil {
		t.Fatal("expected no Async handle at max depth")
	}
	if !isErrorOutput(result.Result) {
		t.Fatalf("expected error output, got %T", result.Result.Output)
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

func TestTask_SubAgentQuestionPropagatesApproval(t *testing.T) {
	const (
		approvalID = "sub-approval-1"
		question   = "Which option should I use?"
		finalText  = "completed after answer"
	)

	answered := false
	gotAnswers := map[string]string(nil)
	gotApprovalID := ""

	subAgent := &mockSubAgent{
		promptFn: func(_ context.Context, _ string, _ agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			return func(_ func(message.MessageChunk, error) bool) {}
		},
		pendingQuestionFn: func(_ string) (*agent.PendingQuestion, error) {
			if answered {
				return nil, nil
			}
			return &agent.PendingQuestion{
				ApprovalID: approvalID,
				Questions: []api.AskUserQuestion{{
					Question: question,
					Header:   "Option",
					Options: []api.AskUserQuestionOption{
						{Label: "A", Description: "Use option A"},
						{Label: "B", Description: "Use option B"},
					},
				}},
			}, nil
		},
		submitAnswerFn: func(_ string, submittedApprovalID string, req api.AnswerQuestionRequest) error {
			answered = true
			gotApprovalID = submittedApprovalID
			gotAnswers = req.Answers
			return nil
		},
		finalResponseFn: func(_ string) (string, error) {
			if answered {
				return finalText, nil
			}
			return "", nil
		},
	}

	exec := New(t.TempDir(), t.TempDir(), "parent-thread")
	toolCtx := &thread.ToolContext{ThreadID: "parent-thread", Agent: subAgent}

	result, err := exec.Execute(context.Background(), toolCtx, makeTaskCall(t, "ask the sub-agent"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Async == nil {
		t.Fatal("expected Async handle")
	}
	taskID := result.Async.TaskID
	t.Cleanup(func() { cleanupTask(taskID) })

	waitResult := waitAsyncResult(t, result.Async, 5*time.Second)
	if waitResult.Approval == nil {
		t.Fatal("expected approval request from sub-agent task")
	}

	var questions []api.AskUserQuestion
	if err := json.Unmarshal(waitResult.Approval.Questions, &questions); err != nil {
		t.Fatalf("unmarshal approval questions: %v", err)
	}
	if len(questions) != 1 || questions[0].Question != question {
		t.Fatalf("unexpected approval questions: %#v", questions)
	}

	answerReq := &api.AnswerQuestionRequest{
		Answers: map[string]string{question: "A"},
	}
	resumed, err := exec.ResumeAsync(context.Background(), toolCtx, makeTaskCall(t, "ask the sub-agent"), taskID, answerReq)
	if err != nil {
		t.Fatalf("ResumeAsync: %v", err)
	}
	if resumed.Async == nil {
		t.Fatal("expected Async handle after answering sub-agent question")
	}

	res := waitHandle(t, resumed.Async, 5*time.Second)
	if got := textOutput(res); got != finalText {
		t.Fatalf("output: got %q, want %q", got, finalText)
	}
	if gotApprovalID != approvalID {
		t.Fatalf("approval ID: got %q, want %q", gotApprovalID, approvalID)
	}
	if gotAnswers[question] != "A" {
		t.Fatalf("answers: got %#v", gotAnswers)
	}
}

func TestTask_ResumeAsync_CrashRecoveryAfterSubAgentQuestion(t *testing.T) {
	const (
		approvalID = "sub-approval-crash"
		question   = "Continue with the risky step?"
		finalText  = "resumed after crash"
		taskID     = "paused-task-after-crash"
	)

	answered := false
	gotApprovalID := ""
	gotAnswers := map[string]string(nil)

	subAgent := &mockSubAgent{
		pendingQuestionFn: func(_ string) (*agent.PendingQuestion, error) {
			if answered {
				return nil, nil
			}
			return &agent.PendingQuestion{
				ApprovalID: approvalID,
				Questions: []api.AskUserQuestion{{
					Question: question,
					Header:   "Confirmation",
					Options: []api.AskUserQuestionOption{
						{Label: "Yes", Description: "Continue"},
						{Label: "No", Description: "Stop"},
					},
				}},
			}, nil
		},
		submitAnswerFn: func(_ string, submittedApprovalID string, req api.AnswerQuestionRequest) error {
			answered = true
			gotApprovalID = submittedApprovalID
			gotAnswers = req.Answers
			return nil
		},
		promptFn: func(_ context.Context, _ string, _ agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			return func(_ func(message.MessageChunk, error) bool) {}
		},
		finalResponseFn: func(_ string) (string, error) {
			if answered {
				return finalText, nil
			}
			return "", nil
		},
	}

	exec := New(t.TempDir(), t.TempDir(), "parent-thread")
	toolCtx := &thread.ToolContext{ThreadID: "parent-thread", Agent: subAgent}

	call := makeTaskCall(t, "recover paused sub-agent")
	call.ToolCallID = t.Name() + "-recover"

	answerReq := &api.AnswerQuestionRequest{
		Answers: map[string]string{question: "Yes"},
	}
	result, err := exec.ResumeAsync(context.Background(), toolCtx, call, taskID, answerReq)
	if err != nil {
		t.Fatalf("ResumeAsync: %v", err)
	}
	if result.Async == nil {
		t.Fatal("expected Async handle for crash recovery")
	}
	t.Cleanup(func() { cleanupTask(taskID) })

	res := waitHandle(t, result.Async, 5*time.Second)
	if got := textOutput(res); got != finalText {
		t.Fatalf("output: got %q, want %q", got, finalText)
	}
	if gotApprovalID != approvalID {
		t.Fatalf("approval ID: got %q, want %q", gotApprovalID, approvalID)
	}
	if gotAnswers[question] != "Yes" {
		t.Fatalf("answers: got %#v", gotAnswers)
	}
}

func TestTask_ThreeLevelsDeepQuestionPropagatesAndResumes(t *testing.T) {
	const question = "Should the third level continue?"
	const finalText = "third level completed"

	leafExec := New(t.TempDir(), t.TempDir(), "leaf-thread")
	leaf := newRecursiveTaskAgent(leafExec, nil)
	leaf.questionID = "leaf-question"
	leaf.questionText = question
	leaf.finalText = finalText

	level2Exec := New(t.TempDir(), t.TempDir(), "level2-thread")
	level2 := newRecursiveTaskAgent(level2Exec, leaf)

	level1Exec := New(t.TempDir(), t.TempDir(), "level1-thread")
	level1 := newRecursiveTaskAgent(level1Exec, level2)

	rootExec := New(t.TempDir(), t.TempDir(), "root-thread")
	toolCtx := &thread.ToolContext{ThreadID: "root-thread", Agent: level1, MaxSubagentDepth: 4}

	call := makeTaskCall(t, "go three levels deep")
	result, err := rootExec.Execute(context.Background(), toolCtx, call)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Async == nil {
		t.Fatal("expected Async handle")
	}
	taskID := result.Async.TaskID
	t.Cleanup(func() { cleanupTask(taskID) })

	waitResult := waitAsyncResult(t, result.Async, 5*time.Second)
	if waitResult.Approval == nil {
		t.Fatal("expected approval from third-level sub-agent")
	}

	var questions []api.AskUserQuestion
	if err := json.Unmarshal(waitResult.Approval.Questions, &questions); err != nil {
		t.Fatalf("unmarshal approval questions: %v", err)
	}
	if len(questions) != 1 || questions[0].Question != question {
		t.Fatalf("unexpected approval questions: %#v", questions)
	}

	resumed, err := rootExec.ResumeAsync(context.Background(), toolCtx, call, taskID, &api.AnswerQuestionRequest{
		Answers: map[string]string{question: "Yes"},
	})
	if err != nil {
		t.Fatalf("ResumeAsync: %v", err)
	}
	if resumed.Async == nil {
		t.Fatal("expected Async handle after answering deep question")
	}

	res := waitHandle(t, resumed.Async, 5*time.Second)
	if got := textOutput(res); got != finalText {
		t.Fatalf("output: got %q, want %q", got, finalText)
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
