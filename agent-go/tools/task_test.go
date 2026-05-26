package tools

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/obot-platform/discobot/agent-go/agent"
	"github.com/obot-platform/discobot/agent-go/internal/api"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/thread"
)

// --- Mock sub-agent ---

type mockSubAgent struct {
	promptFn               func(ctx context.Context, threadID string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error]
	compactFn              func(ctx context.Context, threadID string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error]
	resetFn                func(ctx context.Context, threadID string) (agent.ThreadInfo, error)
	resumeFn               func(ctx context.Context, threadID string, req agent.PromptRequest) (agent.ResumeResult, error)
	finalResponseFn        func(threadID string) (string, error)
	pendingQuestionFn      func(threadID string) (*agent.PendingQuestion, error)
	submitAnswerFn         func(threadID, approvalID string, req api.AnswerQuestionRequest) error
	hasInterruptedTurnFn   func(threadID string) (bool, error)
	getThreadInfoFn        func(threadID string) (agent.ThreadInfo, error)
	getTokenUsageFn        func(threadID string) (agent.ThreadTokenUsageDetails, error)
	createThreadFn         func(ctx context.Context, req agent.CreateThreadRequest) (agent.ThreadInfo, error)
	validateSubagentTypeFn func(subagentType string) error
}

func (m *mockSubAgent) Prompt(ctx context.Context, threadID string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
	if m.promptFn != nil {
		return m.promptFn(ctx, threadID, req)
	}
	return func(_ func(message.MessageChunk, error) bool) {}
}
func (m *mockSubAgent) Compact(ctx context.Context, threadID string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
	if m.compactFn != nil {
		return m.compactFn(ctx, threadID, req)
	}
	return func(_ func(message.MessageChunk, error) bool) {}
}
func (m *mockSubAgent) Reset(ctx context.Context, threadID string) (agent.ThreadInfo, error) {
	if m.resetFn != nil {
		return m.resetFn(ctx, threadID)
	}
	return agent.ThreadInfo{ID: threadID}, nil
}
func (m *mockSubAgent) Resume(ctx context.Context, threadID string, req agent.PromptRequest) (agent.ResumeResult, error) {
	if m.resumeFn != nil {
		return m.resumeFn(ctx, threadID, req)
	}
	return agent.ResumeResult{Stream: func(_ func(message.MessageChunk, error) bool) {}}, nil
}

func (m *mockSubAgent) Cancel(_ string) bool                              { return false }
func (m *mockSubAgent) Messages(_, _ string) ([]message.UIMessage, error) { return nil, nil }
func (m *mockSubAgent) ListThreads() ([]string, error)                    { return nil, nil }
func (m *mockSubAgent) ListThreadInfos() ([]agent.ThreadInfo, error)      { return nil, nil }
func (m *mockSubAgent) GetThreadInfo(threadID string) (agent.ThreadInfo, error) {
	if m.getThreadInfoFn != nil {
		return m.getThreadInfoFn(threadID)
	}
	return agent.ThreadInfo{ID: threadID}, nil
}
func (m *mockSubAgent) GetThreadTokenUsageDetails(threadID string) (agent.ThreadTokenUsageDetails, error) {
	if m.getTokenUsageFn != nil {
		return m.getTokenUsageFn(threadID)
	}
	return agent.ThreadTokenUsageDetails{ThreadID: threadID}, nil
}
func (m *mockSubAgent) CreateThread(ctx context.Context, req agent.CreateThreadRequest) (agent.ThreadInfo, error) {
	if m.createThreadFn != nil {
		return m.createThreadFn(ctx, req)
	}
	return agent.ThreadInfo{ID: req.ID, Name: req.Name, LastMessage: req.LastMessage, Metadata: req.Metadata}, nil
}
func (m *mockSubAgent) UpdateThread(_ context.Context, threadID string, req agent.UpdateThreadRequest) (agent.ThreadInfo, error) {
	info := agent.ThreadInfo{ID: threadID, Metadata: req.Metadata}
	if req.Name != nil {
		info.Name = *req.Name
	}
	if req.ErrorMessage != nil {
		info.ErrorMessage = *req.ErrorMessage
	}
	return info, nil
}
func (m *mockSubAgent) DeleteThread(context.Context, string) error { return nil }
func (m *mockSubAgent) HasInterruptedTurn(threadID string) (bool, error) {
	if m.hasInterruptedTurnFn != nil {
		return m.hasInterruptedTurnFn(threadID)
	}
	return false, nil
}
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
func (m *mockSubAgent) ValidateSubagentType(subagentType string) error {
	if m.validateSubagentTypeFn != nil {
		return m.validateSubagentTypeFn(subagentType)
	}
	return nil
}
func (m *mockSubAgent) FinalResponse(threadID string) (string, error) {
	if m.finalResponseFn != nil {
		return m.finalResponseFn(threadID)
	}
	return "", nil
}

func TestCancelTaskCompletionCancelsRunningTask(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec := &taskRecord{
		taskID:      "task1",
		status:      "in_progress",
		subThreadID: "task1",
		cancel:      cancel,
	}
	globalTasks.mu.Lock()
	globalTasks.tasks["task1"] = rec
	globalTasks.mu.Unlock()
	t.Cleanup(func() {
		globalTasks.mu.Lock()
		delete(globalTasks.tasks, "task1")
		globalTasks.mu.Unlock()
	})

	cancelledID, ok := cancelTaskCompletion("task1")
	if !ok {
		t.Fatal("expected task cancellation to succeed")
	}
	if cancelledID != "task1" {
		t.Fatalf("expected task ID, got %q", cancelledID)
	}

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("expected task context to be cancelled")
	}
}

type storeBackedMockSubAgent struct {
	*mockSubAgent
	store *thread.Store
}

func (m *storeBackedMockSubAgent) CreateThread(_ context.Context, req agent.CreateThreadRequest) (agent.ThreadInfo, error) {
	if err := m.store.CreateThread(req.ID); err != nil {
		return agent.ThreadInfo{}, err
	}
	cfg, err := m.store.LoadConfig(req.ID)
	if err != nil {
		return agent.ThreadInfo{}, err
	}
	cfg.Name = req.Name
	cfg.LastMessage = req.LastMessage
	if len(req.Metadata) > 0 {
		if err := json.Unmarshal(req.Metadata, &cfg.Metadata); err != nil {
			return agent.ThreadInfo{}, err
		}
	}
	if err := m.store.SaveConfig(req.ID, cfg); err != nil {
		return agent.ThreadInfo{}, err
	}
	return agent.ThreadInfo{ID: req.ID, Name: cfg.Name, LastMessage: cfg.LastMessage, Metadata: cfg.Metadata.RawMessage()}, nil
}

func (m *storeBackedMockSubAgent) UpdateThread(_ context.Context, threadID string, req agent.UpdateThreadRequest) (agent.ThreadInfo, error) {
	cfg, err := m.store.LoadConfig(threadID)
	if err != nil {
		return agent.ThreadInfo{}, err
	}
	if req.ErrorMessage != nil {
		cfg.ErrorMessage = *req.ErrorMessage
	}
	if err := m.store.SaveConfig(threadID, cfg); err != nil {
		return agent.ThreadInfo{}, err
	}
	return agent.ThreadInfo{ID: threadID, ErrorMessage: cfg.ErrorMessage, Metadata: cfg.Metadata.RawMessage()}, nil
}

type recursiveTaskState struct {
	subThreadID   string
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
		if state.subThreadID == "" {
			execResult, err = a.exec.Execute(ctx, toolCtx, call)
			if err == nil && execResult.Async != nil {
				state.subThreadID, err = unmarshalTaskContinuation(execResult.Async.Continuation)
				if err != nil {
					yield(nil, err)
					return
				}
			}
		} else if state.pendingAnswer != nil {
			execResult, err = a.exec.ResumeAsync(ctx, toolCtx, call, state.subThreadID, state.pendingAnswer)
			state.pendingAnswer = nil
		} else {
			execResult, err = a.exec.ResumeAsync(ctx, toolCtx, call, state.subThreadID, nil)
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
func (a *recursiveTaskAgent) Resume(ctx context.Context, threadID string, req agent.PromptRequest) (agent.ResumeResult, error) {
	return agent.ResumeResult{Stream: a.Prompt(ctx, threadID, req)}, nil
}

func (a *recursiveTaskAgent) Compact(ctx context.Context, threadID string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
	return a.Prompt(ctx, threadID, req)
}
func (a *recursiveTaskAgent) Reset(_ context.Context, threadID string) (agent.ThreadInfo, error) {
	a.mu.Lock()
	delete(a.states, threadID)
	a.mu.Unlock()
	return agent.ThreadInfo{ID: threadID}, nil
}
func (a *recursiveTaskAgent) Cancel(_ string) bool                              { return false }
func (a *recursiveTaskAgent) Messages(_, _ string) ([]message.UIMessage, error) { return nil, nil }
func (a *recursiveTaskAgent) ListThreads() ([]string, error)                    { return nil, nil }
func (a *recursiveTaskAgent) ListThreadInfos() ([]agent.ThreadInfo, error)      { return nil, nil }
func (a *recursiveTaskAgent) GetThreadInfo(threadID string) (agent.ThreadInfo, error) {
	return agent.ThreadInfo{ID: threadID}, nil
}
func (a *recursiveTaskAgent) GetThreadTokenUsageDetails(threadID string) (agent.ThreadTokenUsageDetails, error) {
	return agent.ThreadTokenUsageDetails{ThreadID: threadID}, nil
}
func (a *recursiveTaskAgent) CreateThread(_ context.Context, req agent.CreateThreadRequest) (agent.ThreadInfo, error) {
	return agent.ThreadInfo{ID: req.ID, Name: req.Name, LastMessage: req.LastMessage, Metadata: req.Metadata}, nil
}
func (a *recursiveTaskAgent) UpdateThread(_ context.Context, threadID string, req agent.UpdateThreadRequest) (agent.ThreadInfo, error) {
	info := agent.ThreadInfo{ID: threadID, Metadata: req.Metadata}
	if req.ErrorMessage != nil {
		info.ErrorMessage = *req.ErrorMessage
	}
	return info, nil
}
func (a *recursiveTaskAgent) DeleteThread(context.Context, string) error { return nil }
func (a *recursiveTaskAgent) HasInterruptedTurn(string) (bool, error)    { return false, nil }
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

func makeTaskOutputCall(t *testing.T, taskID string, block bool) message.ToolCallPart {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"task_id": taskID, "block": block})
	if err != nil {
		t.Fatalf("marshal TaskOutput input: %v", err)
	}
	return message.ToolCallPart{
		ToolCallID: t.Name() + "-task-output",
		ToolName:   "TaskOutput",
		Input:      string(raw),
	}
}

func waitHandle(t *testing.T, handle *thread.AsyncContinuationHandle, timeout time.Duration) message.ToolResultPart {
	t.Helper()
	res := waitAsyncResult(t, handle, timeout)
	if res.Approval != nil {
		t.Fatalf("Wait returned unexpected approval: %#v", res.Approval)
	}
	return res.Result
}

func waitAsyncResult(t *testing.T, handle *thread.AsyncContinuationHandle, timeout time.Duration) thread.AsyncWaitResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	res, err := handle.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	return res
}

func continuationSubThreadID(t *testing.T, continuation json.RawMessage) string {
	t.Helper()
	subThreadID, err := unmarshalTaskContinuation(continuation)
	if err != nil {
		t.Fatalf("unmarshal continuation: %v", err)
	}
	return subThreadID
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

func TestTodoWriteReturnsMarkdownSummary(t *testing.T) {
	e := New(t.TempDir(), t.TempDir(), "default-thread")

	raw, err := json.Marshal(map[string]any{
		"todos": []map[string]string{
			{
				"content":    "Ship the first task",
				"activeForm": "Shipping the first task",
				"status":     "completed",
			},
			{
				"content":    "Investigate the second task",
				"activeForm": "Investigating the second task",
				"status":     "in_progress",
			},
			{
				"content":    "Queue the third task",
				"activeForm": "Queueing the third task",
				"status":     "pending",
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	result, err := e.Execute(context.Background(), &thread.ToolContext{ThreadID: "thread-123"}, message.ToolCallPart{
		ToolCallID: t.Name(),
		ToolName:   "TodoWrite",
		Input:      string(raw),
	})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	out, ok := result.Result.Output.(message.TextOutput)
	if !ok {
		t.Fatalf("expected TextOutput, got %T", result.Result.Output)
	}

	for _, want := range []string{
		"Todo list updated.",
		"Current status is 1 completed, 1 in progress, and 1 pending.",
		"### Current tasks",
		"- [x] Ship the first task",
		"- [ ] Investigate the second task _(in progress: Investigating the second task)_",
		"- [ ] Queue the third task",
	} {
		if !strings.Contains(out.Value, want) {
			t.Errorf("output %q missing %q", out.Value, want)
		}
	}
}

// cleanupTask removes a task from globalTasks so tests don't interfere.
func cleanupTask(subThreadID string) {
	globalTasks.mu.Lock()
	delete(globalTasks.tasks, subThreadID)
	globalTasks.mu.Unlock()
}

// --- Tests ---

func TestIsTaskRunning(t *testing.T) {
	taskID := "task-running-" + t.Name()
	if IsTaskRunning(taskID) {
		t.Fatal("missing task reported running")
	}

	rec := &taskRecord{taskID: taskID, status: "in_progress"}
	globalTasks.mu.Lock()
	globalTasks.tasks[taskID] = rec
	globalTasks.mu.Unlock()
	t.Cleanup(func() { cleanupTask(taskID) })

	if !IsTaskRunning(taskID) {
		t.Fatal("in-progress task was not reported running")
	}

	rec.mu.Lock()
	rec.status = "completed"
	rec.mu.Unlock()
	if IsTaskRunning(taskID) {
		t.Fatal("completed task reported running")
	}
}

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
	t.Cleanup(func() { cleanupTask(continuationSubThreadID(t, result.Async.Continuation)) })

	res := waitHandle(t, result.Async, 5*time.Second)
	if got := textOutput(res); got != want {
		t.Errorf("output: got %q, want %q", got, want)
	}
}

func TestTaskOutputReadsCompletedTaskFromPersistedThread(t *testing.T) {
	const taskID = "parent-thread.sub.persisted-complete"
	const want = "persisted final response"

	subAgent := &mockSubAgent{
		finalResponseFn: func(threadID string) (string, error) {
			if threadID != taskID {
				t.Fatalf("FinalResponse threadID = %q, want %q", threadID, taskID)
			}
			return want, nil
		},
	}
	exec := New(t.TempDir(), t.TempDir(), "parent-thread")

	result, err := exec.Execute(
		context.Background(),
		&thread.ToolContext{ThreadID: "parent-thread", Agent: subAgent},
		makeTaskOutputCall(t, taskID, true),
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := textOutput(result.Result); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestTaskOutputReportsPersistedInterruptedTask(t *testing.T) {
	const taskID = "parent-thread.sub.persisted-interrupted"
	metadata := thread.ConfigMetadata{Type: "task", TaskID: taskID}.RawMessage()
	subAgent := &mockSubAgent{
		finalResponseFn: func(string) (string, error) { return "", nil },
		getThreadInfoFn: func(threadID string) (agent.ThreadInfo, error) {
			if threadID != taskID {
				t.Fatalf("GetThreadInfo threadID = %q, want %q", threadID, taskID)
			}
			return agent.ThreadInfo{
				ID:       taskID,
				State:    agent.ThreadStateInterrupted,
				Metadata: metadata,
			}, nil
		},
	}
	exec := New(t.TempDir(), t.TempDir(), "parent-thread")

	result, err := exec.Execute(
		context.Background(),
		&thread.ToolContext{ThreadID: "parent-thread", Agent: subAgent},
		makeTaskOutputCall(t, taskID, false),
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := textOutput(result.Result)
	for _, want := range []string{
		"found on disk",
		"Persisted status: interrupted",
		`resume "` + taskID + `"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output %q missing %q", got, want)
		}
	}
}

func TestTask_ReservesUniqueSubThreadID(t *testing.T) {
	const parentThreadID = "parent-thread"
	const activeSubThreadID = parentThreadID + ".sub.collision"
	const uniqueSubThreadID = parentThreadID + ".sub.unique"

	oldSuffix := subThreadIDSuffix
	suffixes := []string{"collision", "unique"}
	subThreadIDSuffix = func() string {
		if len(suffixes) == 0 {
			t.Fatal("unexpected extra sub-thread ID allocation")
		}
		suffix := suffixes[0]
		suffixes = suffixes[1:]
		return suffix
	}
	t.Cleanup(func() { subThreadIDSuffix = oldSuffix })
	globalTasks.mu.Lock()
	globalTasks.tasks[activeSubThreadID] = &taskRecord{taskID: activeSubThreadID, status: "in_progress"}
	globalTasks.mu.Unlock()
	t.Cleanup(func() { cleanupTask(activeSubThreadID) })

	var createdThreadID string
	subAgent := &mockSubAgent{
		createThreadFn: func(_ context.Context, req agent.CreateThreadRequest) (agent.ThreadInfo, error) {
			createdThreadID = req.ID
			return agent.ThreadInfo{ID: req.ID, Name: req.Name, LastMessage: req.LastMessage, Metadata: req.Metadata}, nil
		},
		promptFn: func(_ context.Context, _ string, _ agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			return func(_ func(message.MessageChunk, error) bool) {}
		},
		finalResponseFn: func(_ string) (string, error) { return "ok", nil },
	}

	exec := New(t.TempDir(), t.TempDir(), parentThreadID)
	toolCtx := &thread.ToolContext{ThreadID: parentThreadID, Agent: subAgent}

	result, err := exec.Execute(context.Background(), toolCtx, makeTaskCall(t, "do something"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Async == nil {
		t.Fatal("expected Async handle, got nil")
	}
	t.Cleanup(func() { cleanupTask(continuationSubThreadID(t, result.Async.Continuation)) })

	if createdThreadID != uniqueSubThreadID {
		t.Fatalf("created thread ID: got %q, want %q", createdThreadID, uniqueSubThreadID)
	}
	if got := continuationSubThreadID(t, result.Async.Continuation); got != uniqueSubThreadID {
		t.Fatalf("continuation thread ID: got %q, want %q", got, uniqueSubThreadID)
	}
	waitHandle(t, result.Async, 5*time.Second)
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

	var gotPrompt, gotType, gotParentTaskID, gotModel string
	var gotDepth int
	subAgent := &mockSubAgent{
		promptFn: func(_ context.Context, _ string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			if len(req.UserParts) > 0 {
				if tp, ok := req.UserParts[0].(message.UITextPart); ok {
					gotPrompt = tp.Text
				}
			}
			gotType = req.SubagentType
			gotModel = req.Model
			gotParentTaskID = req.ParentTaskID
			gotDepth = req.SubagentDepth
			return func(_ func(message.MessageChunk, error) bool) {}
		},
		finalResponseFn: func(_ string) (string, error) { return "ok", nil },
	}

	exec := New(t.TempDir(), t.TempDir(), "parent-thread")
	toolCtx := &thread.ToolContext{
		ThreadID:         "parent-thread",
		Agent:            subAgent,
		CurrentTaskID:    "parent-task",
		SubagentDepth:    1,
		MaxSubagentDepth: 4,
		ProviderID:       "openai",
		ModelID:          "gpt-5.5",
	}

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
	t.Cleanup(func() { cleanupTask(continuationSubThreadID(t, result.Async.Continuation)) })

	waitHandle(t, result.Async, 5*time.Second)

	if gotPrompt != wantPrompt {
		t.Errorf("prompt: got %q, want %q", gotPrompt, wantPrompt)
	}
	if gotType != wantType {
		t.Errorf("subagent_type: got %q, want %q", gotType, wantType)
	}
	if gotModel != "openai/gpt-5.5" {
		t.Errorf("model: got %q, want %q", gotModel, "openai/gpt-5.5")
	}
	if gotParentTaskID == "" {
		t.Errorf("parent_task_id: got %q, want non-empty task id", gotParentTaskID)
	}
	if gotDepth != 2 {
		t.Errorf("subagent_depth: got %d, want 2", gotDepth)
	}
}

func TestTask_NormalizesGeneralSubagentAlias(t *testing.T) {
	var gotType string
	subAgent := &mockSubAgent{
		promptFn: func(_ context.Context, _ string, req agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			gotType = req.SubagentType
			return func(_ func(message.MessageChunk, error) bool) {}
		},
		finalResponseFn: func(_ string) (string, error) { return "ok", nil },
	}

	exec := New(t.TempDir(), t.TempDir(), "parent-thread")
	toolCtx := &thread.ToolContext{ThreadID: "parent-thread", Agent: subAgent}
	call := message.ToolCallPart{
		ToolCallID: t.Name() + "-tc",
		ToolName:   "Task",
		Input:      `{"description":"background task","prompt":"do it","subagent_type":"general"}`,
	}

	result, err := exec.Execute(context.Background(), toolCtx, call)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Async == nil {
		t.Fatal("expected Async handle")
	}
	t.Cleanup(func() { cleanupTask(continuationSubThreadID(t, result.Async.Continuation)) })

	waitHandle(t, result.Async, 5*time.Second)

	if gotType != "general-purpose" {
		t.Errorf("subagent_type: got %q, want %q", gotType, "general-purpose")
	}
}

func TestTask_ValidatesSubagentTypeBeforeCreatingThread(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	subAgent := &storeBackedMockSubAgent{
		store: store,
		mockSubAgent: &mockSubAgent{
			validateSubagentTypeFn: func(subagentType string) error {
				if subagentType != "missing" {
					t.Fatalf("subagent_type = %q, want missing", subagentType)
				}
				return errors.New(`sub-agent type "missing" not found in session config`)
			},
		},
	}

	exec := New(t.TempDir(), t.TempDir(), "parent-thread")
	toolCtx := &thread.ToolContext{ThreadID: "parent-thread", Agent: subAgent}
	call := message.ToolCallPart{
		ToolCallID: t.Name() + "-tc",
		ToolName:   "Task",
		Input:      `{"description":"bad task","prompt":"do it","subagent_type":"missing","run_in_background":true}`,
	}

	result, err := exec.Execute(context.Background(), toolCtx, call)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Async != nil {
		t.Fatal("expected validation to fail synchronously")
	}
	if !isErrorOutput(result.Result) {
		t.Fatalf("expected error result, got %T", result.Result.Output)
	}
	threads, err := store.ListThreads()
	if err != nil {
		t.Fatalf("ListThreads: %v", err)
	}
	if len(threads) != 0 {
		t.Fatalf("expected no child thread to be created, got %v", threads)
	}
}

func TestTask_ResumeSkipsNewTaskValidation(t *testing.T) {
	const want = "resumed task output"

	subAgent := &mockSubAgent{
		finalResponseFn: func(_ string) (string, error) { return want, nil },
		validateSubagentTypeFn: func(subagentType string) error {
			t.Fatalf("ValidateSubagentType called for resume with %q", subagentType)
			return nil
		},
	}

	exec := New(t.TempDir(), t.TempDir(), "parent-thread")
	toolCtx := &thread.ToolContext{ThreadID: "parent-thread", Agent: subAgent}
	call := message.ToolCallPart{
		ToolCallID: t.Name() + "-tc",
		ToolName:   "Task",
		Input:      `{"resume":"already-finished-task"}`,
	}

	result, err := exec.Execute(context.Background(), toolCtx, call)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Async != nil {
		t.Fatal("expected synchronous completed result")
	}
	if got := textOutput(result.Result); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestTask_ResumeInterruptedSubtaskUsesResume(t *testing.T) {
	const want = "resumed interrupted task"
	var promptCalled bool
	var gotResumeUserParts int
	var gotResumeModel, gotResumeType string

	subAgent := &mockSubAgent{
		promptFn: func(_ context.Context, _ string, _ agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			promptCalled = true
			return func(yield func(message.MessageChunk, error) bool) {
				yield(nil, agent.ErrInterruptedTurnRequiresResume)
			}
		},
		hasInterruptedTurnFn: func(_ string) (bool, error) { return true, nil },
		resumeFn: func(_ context.Context, _ string, req agent.PromptRequest) (agent.ResumeResult, error) {
			gotResumeUserParts = len(req.UserParts)
			gotResumeModel = req.Model
			gotResumeType = req.SubagentType
			return agent.ResumeResult{Stream: func(_ func(message.MessageChunk, error) bool) {}}, nil
		},
		finalResponseFn: func(_ string) (string, error) { return want, nil },
	}

	exec := New(t.TempDir(), t.TempDir(), "parent-thread")
	toolCtx := &thread.ToolContext{
		ThreadID:   "parent-thread",
		Agent:      subAgent,
		ProviderID: "openai",
		ModelID:    "gpt-5.5",
	}
	call := message.ToolCallPart{
		ToolCallID: t.Name() + "-tc",
		ToolName:   "Task",
		Input:      `{"description":"recover task","prompt":"continue existing work","subagent_type":"general-purpose"}`,
	}

	result, err := exec.Execute(context.Background(), toolCtx, call)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Async == nil {
		t.Fatal("expected Async handle")
	}
	t.Cleanup(func() { cleanupTask(continuationSubThreadID(t, result.Async.Continuation)) })

	res := waitHandle(t, result.Async, 5*time.Second)
	if promptCalled {
		t.Fatal("Prompt called for interrupted subtask; expected Resume")
	}
	if got := textOutput(res); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
	if gotResumeUserParts != 0 {
		t.Fatalf("resume user parts len = %d, want 0", gotResumeUserParts)
	}
	if gotResumeModel != "openai/gpt-5.5" {
		t.Fatalf("resume model = %q, want openai/gpt-5.5", gotResumeModel)
	}
	if gotResumeType != "general-purpose" {
		t.Fatalf("resume subagent type = %q, want general-purpose", gotResumeType)
	}
}

func TestTask_ContinueAfterAnswerUsesResume(t *testing.T) {
	const want = "answered task done"
	approvalID := "approval-1"
	var submitted bool
	var resumed bool

	subAgent := &mockSubAgent{
		pendingQuestionFn: func(_ string) (*agent.PendingQuestion, error) {
			if submitted {
				return nil, nil
			}
			return &agent.PendingQuestion{ApprovalID: approvalID}, nil
		},
		submitAnswerFn: func(_, gotApprovalID string, _ api.AnswerQuestionRequest) error {
			if gotApprovalID != approvalID {
				t.Fatalf("approvalID = %q, want %q", gotApprovalID, approvalID)
			}
			submitted = true
			return nil
		},
		resumeFn: func(_ context.Context, _ string, req agent.PromptRequest) (agent.ResumeResult, error) {
			resumed = true
			if len(req.UserParts) != 0 {
				t.Fatalf("resume user parts len = %d, want 0", len(req.UserParts))
			}
			return agent.ResumeResult{Stream: func(_ func(message.MessageChunk, error) bool) {}}, nil
		},
		finalResponseFn: func(_ string) (string, error) { return want, nil },
	}

	rec := &taskRecord{
		taskID:            t.Name(),
		status:            "waiting_for_answer",
		subThreadID:       t.Name(),
		pendingApprovalID: approvalID,
		done:              make(chan struct{}),
	}
	globalTasks.mu.Lock()
	globalTasks.tasks[rec.taskID] = rec
	globalTasks.mu.Unlock()
	t.Cleanup(func() { cleanupTask(rec.taskID) })

	exec := New(t.TempDir(), t.TempDir(), "parent-thread")
	result, err := exec.continueTask(context.Background(), &thread.ToolContext{ThreadID: "parent-thread", Agent: subAgent}, message.ToolCallPart{
		ToolCallID: t.Name() + "-tc",
		ToolName:   "Task",
		Input:      `{"resume":"` + rec.taskID + `"}`,
	}, marshalTaskContinuation(rec.taskID), &api.AnswerQuestionRequest{})
	if err != nil {
		t.Fatalf("continueTask: %v", err)
	}
	if result.Async == nil {
		t.Fatal("expected Async handle")
	}
	res := waitHandle(t, result.Async, 5*time.Second)
	if !submitted {
		t.Fatal("answer was not submitted")
	}
	if !resumed {
		t.Fatal("Resume was not called")
	}
	if got := textOutput(res); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestTaskOutputTimeoutDefaultsAndUsesMilliseconds(t *testing.T) {
	if got := taskOutputWaitTimeout(0); got != taskOutputDefaultTimeout {
		t.Fatalf("timeout 0 = %s, want %s", got, taskOutputDefaultTimeout)
	}
	if got := taskOutputWaitTimeout(60); got != 60*time.Millisecond {
		t.Fatalf("timeout 60 = %s, want 60ms", got)
	}
	if got := taskOutputWaitTimeout(600000); got != 10*time.Minute {
		t.Fatalf("timeout 600000 = %s, want 10m", got)
	}
}

func TestTaskOutputBlockWaitsForTaskCompletion(t *testing.T) {
	taskID := t.Name()
	done := make(chan struct{})
	rec := &taskRecord{
		taskID:      taskID,
		status:      "in_progress",
		subThreadID: taskID,
		done:        done,
	}
	globalTasks.mu.Lock()
	globalTasks.tasks[taskID] = rec
	globalTasks.mu.Unlock()
	t.Cleanup(func() { cleanupTask(taskID) })

	go func() {
		time.Sleep(50 * time.Millisecond)
		rec.mu.Lock()
		rec.status = "completed"
		rec.output = "done"
		rec.mu.Unlock()
		close(done)
	}()

	exec := New(t.TempDir(), t.TempDir(), "parent-thread")
	start := time.Now()
	result, err := exec.Execute(context.Background(), nil, message.ToolCallPart{
		ToolCallID: t.Name() + "-output",
		ToolName:   "TaskOutput",
		Input:      `{"task_id":"` + taskID + `","block":true,"timeout":600000}`,
	})
	if err != nil {
		t.Fatalf("TaskOutput execute: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 45*time.Millisecond {
		t.Fatalf("TaskOutput returned too early after %s", elapsed)
	}
	if got := textOutput(result.Result); got != "done" {
		t.Fatalf("TaskOutput = %q, want %q", got, "done")
	}
}

func TestTaskStopDoesNotWaitWhileHoldingTaskLock(t *testing.T) {
	taskID := t.Name()
	cancelled := make(chan struct{})
	rec := &taskRecord{
		taskID:      taskID,
		status:      "in_progress",
		subThreadID: taskID,
		done:        make(chan struct{}),
		cancel:      func() { close(cancelled) },
	}
	globalTasks.mu.Lock()
	globalTasks.tasks[taskID] = rec
	globalTasks.mu.Unlock()
	t.Cleanup(func() { cleanupTask(taskID) })

	exec := New(t.TempDir(), t.TempDir(), "parent-thread")
	result, err := exec.Execute(context.Background(), nil, message.ToolCallPart{
		ToolCallID: t.Name() + "-stop",
		ToolName:   "TaskStop",
		Input:      `{"task_id":"` + taskID + `"}`,
	})
	if err != nil {
		t.Fatalf("TaskStop execute: %v", err)
	}
	if got := textOutput(result.Result); !strings.Contains(got, "Task "+taskID+" stopped") {
		t.Fatalf("TaskStop output = %q", got)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("TaskStop did not call cancel")
	}
}

func TestTask_BackgroundFailurePersistsThreadError(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	subAgent := &storeBackedMockSubAgent{
		store: store,
		mockSubAgent: &mockSubAgent{
			promptFn: func(_ context.Context, _ string, _ agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
				return func(yield func(message.MessageChunk, error) bool) {
					yield(nil, errors.New("boom"))
				}
			},
		},
	}

	exec := New(t.TempDir(), t.TempDir(), "parent-thread")
	toolCtx := &thread.ToolContext{ThreadID: "parent-thread", Agent: subAgent}
	call := message.ToolCallPart{
		ToolCallID: t.Name() + "-tc",
		ToolName:   "Task",
		Input:      `{"description":"background task","prompt":"do it","subagent_type":"helper","run_in_background":true}`,
	}

	result, err := exec.Execute(context.Background(), toolCtx, call)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var payload struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(result.Result.Output.(message.JSONOutput).Value, &payload); err != nil {
		t.Fatalf("decode task result: %v", err)
	}
	t.Cleanup(func() { cleanupTask(payload.TaskID) })

	outputCall := message.ToolCallPart{
		ToolCallID: t.Name() + "-output",
		ToolName:   "TaskOutput",
		Input:      `{"task_id":"` + payload.TaskID + `","block":true,"timeout":1000}`,
	}
	if _, err := exec.Execute(context.Background(), toolCtx, outputCall); err != nil {
		t.Fatalf("TaskOutput execute: %v", err)
	}

	cfg, err := store.LoadConfig(payload.TaskID)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.ErrorMessage != "sub-agent failed: boom" {
		t.Fatalf("errorMessage = %q, want %q", cfg.ErrorMessage, "sub-agent failed: boom")
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

// TestTask_Cancellation verifies that cancelling the parent Wait context does
// not cancel the independent sub-agent goroutine.
func TestTask_Cancellation(t *testing.T) {
	agentCancelled := make(chan struct{})
	finishAgent := make(chan struct{})
	const want = "completed after parent stopped waiting"

	subAgent := &mockSubAgent{
		promptFn: func(ctx context.Context, _ string, _ agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			return func(_ func(message.MessageChunk, error) bool) {
				select {
				case <-ctx.Done():
					close(agentCancelled)
				case <-finishAgent:
				}
			}
		},
		finalResponseFn: func(_ string) (string, error) { return want, nil },
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
	subThreadID := continuationSubThreadID(t, result.Async.Continuation)
	t.Cleanup(func() { cleanupTask(subThreadID) })

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

	// The parent wait was cancelled, but the child task should keep running.
	select {
	case <-agentCancelled:
		t.Fatal("sub-agent goroutine was cancelled by parent Wait context")
	case <-time.After(50 * time.Millisecond):
	}

	close(finishAgent)
	result, err = exec.Execute(context.Background(), nil, message.ToolCallPart{
		ToolCallID: t.Name() + "-output",
		ToolName:   "TaskOutput",
		Input:      `{"task_id":"` + subThreadID + `","block":true,"timeout":1000}`,
	})
	if err != nil {
		t.Fatalf("TaskOutput execute: %v", err)
	}
	if got := textOutput(result.Result); got != want {
		t.Fatalf("TaskOutput = %q, want %q", got, want)
	}
}

// TestTask_CancellationBeforeGoroutineStarts verifies there is no race between
// creating the cancel func and a very early parent wait cancellation.
func TestTask_CancellationBeforeGoroutineStarts(t *testing.T) {
	agentCancelled := make(chan struct{})
	finishAgent := make(chan struct{})
	const want = "completed despite early parent cancellation"

	subAgent := &mockSubAgent{
		promptFn: func(ctx context.Context, _ string, _ agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			return func(_ func(message.MessageChunk, error) bool) {
				select {
				case <-ctx.Done():
					close(agentCancelled)
				case <-finishAgent:
				}
			}
		},
		finalResponseFn: func(_ string) (string, error) { return want, nil },
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
	subThreadID := continuationSubThreadID(t, result.Async.Continuation)
	t.Cleanup(func() { cleanupTask(subThreadID) })

	res, err := result.Async.Wait(waitCtx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !isErrorOutput(res.Result) {
		t.Errorf("expected ErrorTextOutput (cancelled), got %T", res.Result.Output)
	}

	select {
	case <-agentCancelled:
		t.Fatal("sub-agent goroutine was cancelled by parent Wait context")
	case <-time.After(50 * time.Millisecond):
	}

	close(finishAgent)
	result, err = exec.Execute(context.Background(), nil, message.ToolCallPart{
		ToolCallID: t.Name() + "-output",
		ToolName:   "TaskOutput",
		Input:      `{"task_id":"` + subThreadID + `","block":true,"timeout":1000}`,
	})
	if err != nil {
		t.Fatalf("TaskOutput execute: %v", err)
	}
	if got := textOutput(result.Result); got != want {
		t.Fatalf("TaskOutput = %q, want %q", got, want)
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
	subThreadID := continuationSubThreadID(t, result.Async.Continuation)
	t.Cleanup(func() { cleanupTask(subThreadID) })

	// Wait for the goroutine to complete so the record is in "completed" state.
	waitHandle(t, result.Async, 5*time.Second)

	// ResumeAsync should find the record still in globalTasks and return a handle.
	call := makeTaskCall(t, "some work")
	resumed, err := exec.ResumeAsync(context.Background(), toolCtx, call, subThreadID, nil)
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
	subThreadID := "crashed-completed-" + t.Name()

	call := makeTaskCall(t, "task that finished before crash")
	call.ToolCallID = t.Name() + "-recover"

	result, err := exec.ResumeAsync(context.Background(), toolCtx, call, subThreadID, nil)
	if err != nil {
		t.Fatalf("ResumeAsync: %v", err)
	}

	var res message.ToolResultPart
	if result.Async != nil {
		// Acceptable: implementation chose to wrap in async handle.
		t.Cleanup(func() { cleanupTask(continuationSubThreadID(t, result.Async.Continuation)) })
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

	subThreadID := "crashed-midturn-" + t.Name()

	call := makeTaskCall(t, "task interrupted mid-turn")
	call.ToolCallID = t.Name() + "-midturn"

	result, err := exec.ResumeAsync(context.Background(), toolCtx, call, subThreadID, nil)
	if err != nil {
		t.Fatalf("ResumeAsync: %v", err)
	}
	if result.Async == nil {
		t.Fatal("expected Async handle for mid-turn recovery")
	}
	t.Cleanup(func() { cleanupTask(continuationSubThreadID(t, result.Async.Continuation)) })

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
	subThreadID := continuationSubThreadID(t, result.Async.Continuation)
	t.Cleanup(func() { cleanupTask(subThreadID) })

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
	resumed, err := exec.ResumeAsync(context.Background(), toolCtx, makeTaskCall(t, "ask the sub-agent"), subThreadID, answerReq)
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

func TestTask_RunInBackgroundReturnsTaskIDAndTaskOutputFindsIt(t *testing.T) {
	const want = "background task complete"

	subAgent := &mockSubAgent{
		promptFn: func(_ context.Context, _ string, _ agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			return func(_ func(message.MessageChunk, error) bool) {}
		},
		finalResponseFn: func(_ string) (string, error) { return want, nil },
	}

	exec := New(t.TempDir(), t.TempDir(), "parent-thread")
	toolCtx := &thread.ToolContext{ThreadID: "parent-thread", Agent: subAgent}

	call := message.ToolCallPart{
		ToolCallID: t.Name() + "-tc",
		ToolName:   "Task",
		Input:      `{"description":"background task","prompt":"do it","subagent_type":"helper","run_in_background":true}`,
	}
	result, err := exec.Execute(context.Background(), toolCtx, call)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Async != nil {
		t.Fatal("expected background task to return immediately")
	}
	out, ok := result.Result.Output.(message.JSONOutput)
	if !ok {
		t.Fatalf("expected JSONOutput, got %T", result.Result.Output)
	}
	var payload struct {
		TaskID   string `json:"task_id"`
		ThreadID string `json:"thread_id"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(out.Value, &payload); err != nil {
		t.Fatalf("unmarshal task payload: %v", err)
	}
	if payload.TaskID == "" {
		t.Fatal("expected task_id in background task result")
	}
	if payload.TaskID != payload.ThreadID {
		t.Fatalf("expected thread_id to match task_id, got %+v", payload)
	}
	t.Cleanup(func() { cleanupTask(payload.TaskID) })

	outputCall := message.ToolCallPart{
		ToolCallID: t.Name() + "-output",
		ToolName:   "TaskOutput",
		Input:      `{"task_id":"` + payload.TaskID + `","block":true,"timeout":1000}`,
	}
	outputResult, err := exec.Execute(context.Background(), toolCtx, outputCall)
	if err != nil {
		t.Fatalf("TaskOutput execute: %v", err)
	}
	if got := textOutput(outputResult.Result); got != want {
		t.Fatalf("task output: got %q, want %q", got, want)
	}
}

func TestTask_ResumeInputReattachesToBackgroundTask(t *testing.T) {
	const want = "resumed background task complete"

	release := make(chan struct{})
	subAgent := &mockSubAgent{
		promptFn: func(_ context.Context, _ string, _ agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
			return func(_ func(message.MessageChunk, error) bool) {
				<-release
			}
		},
		finalResponseFn: func(_ string) (string, error) { return want, nil },
	}

	exec := New(t.TempDir(), t.TempDir(), "parent-thread")
	toolCtx := &thread.ToolContext{ThreadID: "parent-thread", Agent: subAgent}

	startCall := message.ToolCallPart{
		ToolCallID: t.Name() + "-start",
		ToolName:   "Task",
		Input:      `{"description":"background task","prompt":"do it","subagent_type":"helper","run_in_background":true}`,
	}
	started, err := exec.Execute(context.Background(), toolCtx, startCall)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	startedPayload, ok := started.Result.Output.(message.JSONOutput)
	if !ok {
		t.Fatalf("expected JSONOutput, got %T", started.Result.Output)
	}
	var startedInfo struct {
		TaskID string `json:"task_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(startedPayload.Value, &startedInfo); err != nil {
		t.Fatalf("unmarshal started payload: %v", err)
	}
	if startedInfo.TaskID == "" {
		t.Fatal("expected task_id for background task")
	}
	t.Cleanup(func() { cleanupTask(startedInfo.TaskID) })

	resumeCall := message.ToolCallPart{
		ToolCallID: t.Name() + "-resume",
		ToolName:   "Task",
		Input:      `{"description":"background task","prompt":"do it","subagent_type":"helper","run_in_background":true,"resume":"` + startedInfo.TaskID + `"}`,
	}
	resumed, err := exec.Execute(context.Background(), toolCtx, resumeCall)
	if err != nil {
		t.Fatalf("Execute resume: %v", err)
	}
	resumedPayload, ok := resumed.Result.Output.(message.JSONOutput)
	if !ok {
		t.Fatalf("expected JSONOutput from resume, got %T", resumed.Result.Output)
	}
	var resumedInfo struct {
		TaskID string `json:"task_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(resumedPayload.Value, &resumedInfo); err != nil {
		t.Fatalf("unmarshal resumed payload: %v", err)
	}
	if resumedInfo.TaskID != startedInfo.TaskID {
		t.Fatalf("expected resumed task_id %q, got %q", startedInfo.TaskID, resumedInfo.TaskID)
	}
	if resumedInfo.Status != "in_progress" {
		t.Fatalf("expected resumed task status in_progress, got %q", resumedInfo.Status)
	}

	close(release)

	outputCall := message.ToolCallPart{
		ToolCallID: t.Name() + "-output",
		ToolName:   "TaskOutput",
		Input:      `{"task_id":"` + startedInfo.TaskID + `","block":true,"timeout":1000}`,
	}
	outputResult, err := exec.Execute(context.Background(), toolCtx, outputCall)
	if err != nil {
		t.Fatalf("TaskOutput execute: %v", err)
	}
	if got := textOutput(outputResult.Result); got != want {
		t.Fatalf("task output: got %q, want %q", got, want)
	}
}

func TestTask_BootstrapsThreadMetadataAndEmitsThreadUpdate(t *testing.T) {
	store := thread.NewStore(t.TempDir())
	subAgent := &storeBackedMockSubAgent{
		mockSubAgent: &mockSubAgent{
			promptFn: func(_ context.Context, _ string, _ agent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
				return func(_ func(message.MessageChunk, error) bool) {}
			},
			finalResponseFn: func(_ string) (string, error) { return "ok", nil },
		},
		store: store,
	}

	exec := New(t.TempDir(), t.TempDir(), "parent-thread")
	var emitted []message.MessageChunk
	toolCtx := &thread.ToolContext{
		ThreadID: "parent-thread",
		Agent:    subAgent,
		EmitChunk: func(chunk message.MessageChunk, err error) bool {
			if err != nil {
				t.Fatalf("unexpected emitted error: %v", err)
			}
			emitted = append(emitted, chunk)
			return true
		},
	}

	call := message.ToolCallPart{
		ToolCallID: t.Name() + "-tc",
		ToolName:   "Task",
		Input:      `{"description":"Investigate task flow","prompt":"inspect the child thread","subagent_type":"helper","run_in_background":true}`,
	}
	result, err := exec.Execute(context.Background(), toolCtx, call)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	payload := result.Result.Output.(message.JSONOutput)
	var info struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(payload.Value, &info); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	t.Cleanup(func() { cleanupTask(info.TaskID) })

	cfg, err := store.LoadConfig(info.TaskID)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.LastMessage != "inspect the child thread" {
		t.Fatalf("lastMessage = %q", cfg.LastMessage)
	}
	if cfg.Metadata.Type != "task" {
		t.Fatalf("metadata type = %#v", cfg.Metadata.Type)
	}
	if cfg.Metadata.Prompt != "inspect the child thread" {
		t.Fatalf("metadata prompt = %#v", cfg.Metadata.Prompt)
	}
	if cfg.Metadata.Model != "" {
		t.Fatalf("metadata model = %#v", cfg.Metadata.Model)
	}
	if len(emitted) == 0 {
		t.Fatal("expected a thread update chunk for the new task thread")
	}
	update, ok := emitted[0].(message.ThreadUpdateChunk)
	if !ok {
		t.Fatalf("expected ThreadUpdateChunk, got %T", emitted[0])
	}
	if update.Data.Thread.ID != info.TaskID {
		t.Fatalf("thread update id = %q, want %q", update.Data.Thread.ID, info.TaskID)
	}
	if len(update.Data.Thread.Metadata) == 0 {
		t.Fatal("expected thread update metadata")
	}

	outputCall := message.ToolCallPart{
		ToolCallID: t.Name() + "-output",
		ToolName:   "TaskOutput",
		Input:      `{"task_id":"` + info.TaskID + `","block":true,"timeout":1000}`,
	}
	if _, err := exec.Execute(context.Background(), toolCtx, outputCall); err != nil {
		t.Fatalf("TaskOutput execute: %v", err)
	}
}

func TestTask_ResumeAsync_CrashRecoveryAfterSubAgentQuestion(t *testing.T) {
	const (
		approvalID  = "sub-approval-crash"
		question    = "Continue with the risky step?"
		finalText   = "resumed after crash"
		subThreadID = "paused-task-after-crash"
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
	result, err := exec.ResumeAsync(context.Background(), toolCtx, call, subThreadID, answerReq)
	if err != nil {
		t.Fatalf("ResumeAsync: %v", err)
	}
	if result.Async == nil {
		t.Fatal("expected Async handle for crash recovery")
	}
	t.Cleanup(func() { cleanupTask(subThreadID) })

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
	subThreadID := continuationSubThreadID(t, result.Async.Continuation)
	t.Cleanup(func() { cleanupTask(subThreadID) })

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

	resumed, err := rootExec.ResumeAsync(context.Background(), toolCtx, call, subThreadID, &api.AnswerQuestionRequest{
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
// "<parentThreadID>.sub.<subThreadID>" so crash-recovery can reconstruct it.
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
	subThreadID := continuationSubThreadID(t, result.Async.Continuation)
	t.Cleanup(func() { cleanupTask(subThreadID) })

	waitHandle(t, result.Async, 5*time.Second)

	wantPrefix := parentThreadID + ".sub."
	if len(capturedSubThreadID) <= len(wantPrefix) || capturedSubThreadID[:len(wantPrefix)] != wantPrefix {
		t.Errorf("sub-thread ID %q does not start with %q", capturedSubThreadID, wantPrefix)
	}
	if capturedSubThreadID != subThreadID {
		t.Errorf("captured sub-thread ID: got %q, want %q", capturedSubThreadID, subThreadID)
	}
}
