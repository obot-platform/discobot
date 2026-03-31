package thread

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"testing"
	"time"

	"github.com/obot-platform/discobot/agent-go/internal/api"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
	"github.com/obot-platform/discobot/agent-go/providers/transport"
)

// --- Mock provider ---

// mockProvider lets tests control the sequence of Complete() calls.
// Each call to Complete pops the next response from the responses slice.
type mockProvider struct {
	responses [][]message.ProviderMessageChunk // one per Complete() call
	callIndex int
	requests  []providers.CompleteRequest // captured requests
}

func (m *mockProvider) ID() string { return "mock" }

func (m *mockProvider) Complete(_ context.Context, req providers.CompleteRequest) iter.Seq2[message.ProviderMessageChunk, error] {
	m.requests = append(m.requests, req)
	idx := m.callIndex
	m.callIndex++
	return func(yield func(message.ProviderMessageChunk, error) bool) {
		if idx >= len(m.responses) {
			yield(nil, fmt.Errorf("no more mock responses"))
			return
		}
		for _, chunk := range m.responses[idx] {
			if !yield(chunk, nil) {
				return
			}
		}
	}
}

func (m *mockProvider) DefaultModels() map[string]providers.ModelRef { return nil }
func (m *mockProvider) ListModels(_ context.Context) ([]providers.ModelInfo, error) {
	return nil, nil
}

// --- Mock executor ---

type mockExecutor struct {
	results map[string]message.ToolResultPart // keyed by toolCallID
}

func (m *mockExecutor) Execute(_ context.Context, _ *ToolContext, call message.ToolCallPart) (ToolExecuteResult, error) {
	if result, ok := m.results[call.ToolCallID]; ok {
		return ToolExecuteResult{Result: result}, nil
	}
	return ToolExecuteResult{Result: message.ToolResultPart{
		ToolCallID: call.ToolCallID,
		ToolName:   call.ToolName,
		Output:     message.TextOutput{Value: "default output"},
	}}, nil
}

func (m *mockExecutor) ResolveAnswer(_ *ToolContext, _ message.ToolCallPart, _ api.AnswerQuestionRequest) (ToolExecuteResult, error) {
	return ToolExecuteResult{}, fmt.Errorf("no approvals in mock")
}

func (m *mockExecutor) ResumeAsync(_ context.Context, _ *ToolContext, _ message.ToolCallPart, _ string, _ *api.AnswerQuestionRequest) (ToolExecuteResult, error) {
	return ToolExecuteResult{}, fmt.Errorf("no async in mock executor")
}

func (m *mockExecutor) Continue(ctx context.Context, toolCtx *ToolContext, call message.ToolCallPart, continuation json.RawMessage, req *api.AnswerQuestionRequest) (ToolExecuteResult, error) {
	if req != nil {
		return m.ResolveAnswer(toolCtx, call, *req)
	}
	if len(continuation) > 0 {
		return m.ResumeAsync(ctx, toolCtx, call, "", nil)
	}
	return ToolExecuteResult{}, fmt.Errorf("no continuation in mock executor")
}

func (m *mockExecutor) SetPlanMode(_ bool)   {}
func (m *mockExecutor) SetThreadID(_ string) {}

// --- Helper to collect all chunks ---

func collectChunks(t *testing.T, seq iter.Seq2[message.MessageChunk, error]) []message.MessageChunk {
	t.Helper()
	var chunks []message.MessageChunk
	for chunk, err := range seq {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if chunk != nil {
			chunks = append(chunks, chunk)
		}
	}
	return chunks
}

func toolResultParts(msgs []message.Message) []message.ToolResultPart {
	var results []message.ToolResultPart
	for _, msg := range msgs {
		if msg.Role != "tool" {
			continue
		}
		for _, part := range msg.Parts {
			if result, ok := part.(message.ToolResultPart); ok {
				results = append(results, result)
			}
		}
	}
	return results
}

func testContinuation(subThreadID string) json.RawMessage {
	data, _ := json.Marshal(map[string]string{"subThreadId": subThreadID})
	return data
}

func testContinuationID(t *testing.T, continuation json.RawMessage) string {
	t.Helper()
	var decoded struct {
		SubThreadID string `json:"subThreadId"`
	}
	if err := json.Unmarshal(continuation, &decoded); err != nil {
		t.Fatalf("decode continuation: %v", err)
	}
	return decoded.SubThreadID
}

// --- Tests ---

func TestRunTurn_SimpleTextResponse(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "Hello!"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	chunks := collectChunks(t, RunTurn(
		context.Background(), prov, &mockExecutor{}, store,
		threadID, "", TurnConfig{
			Model:     "test-model",
			UserParts: []message.Part{message.TextPart{Text: "hi"}},
		},
	))

	hasTextDelta := false
	for _, c := range chunks {
		if _, ok := c.(message.TextDeltaChunk); ok {
			hasTextDelta = true
		}
	}
	if !hasTextDelta {
		t.Error("missing TextDeltaChunk")
	}

	// Verify messages were saved to the thread.
	threads, _ := store.ListThreads()
	if len(threads) != 1 {
		t.Fatalf("expected 1 thread, got %d", len(threads))
	}

	// Verify turn.json was cleaned up.
	state, err := store.LoadTurnState(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if state != nil {
		t.Error("expected turn.json to be deleted after turn completes")
	}
}

func TestRunTurn_WithToolCall(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	toolCallID := "tc1"
	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			// Step 0: model calls a tool
			{
				message.StreamStartChunk{},
				message.ToolCallChunk{
					ToolCallID: toolCallID,
					ToolName:   "read_file",
					Input:      `{"path":"test.txt"}`,
				},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "tool-calls"}},
			},
			// Step 1: model produces text after tool result
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "File contents: hello"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	executor := &mockExecutor{
		results: map[string]message.ToolResultPart{
			toolCallID: {
				ToolCallID: toolCallID,
				ToolName:   "read_file",
				Output:     message.TextOutput{Value: "hello"},
			},
		},
	}

	chunks := collectChunks(t, RunTurn(
		context.Background(), prov, executor, store,
		threadID, "", TurnConfig{
			Model:     "test-model",
			UserParts: []message.Part{message.TextPart{Text: "read test.txt"}},
			Tools:     []providers.ToolDefinition{{Name: "read_file"}},
		},
	))

	// Verify the provider was called twice (two steps).
	if prov.callIndex != 2 {
		t.Errorf("expected 2 provider calls, got %d", prov.callIndex)
	}

	// Verify tool output chunk was yielded.
	hasToolOutput := false
	for _, c := range chunks {
		if _, ok := c.(message.ToolOutputAvailableChunk); ok {
			hasToolOutput = true
		}
	}
	if !hasToolOutput {
		t.Error("missing ToolOutputAvailableChunk")
	}

	// Verify step 1 request includes tool result in history.
	if len(prov.requests) < 2 {
		t.Fatal("expected at least 2 requests")
	}
	step1Msgs := prov.requests[1].Messages
	// Should have: user, assistant (tool call), tool (result)
	if len(step1Msgs) < 3 {
		t.Fatalf("expected at least 3 messages in step 1, got %d", len(step1Msgs))
	}
	lastMsg := step1Msgs[len(step1Msgs)-1]
	if lastMsg.Role != "tool" {
		t.Errorf("expected last message role=tool, got %s", lastMsg.Role)
	}
}

func TestRunTurn_MultiStepToolCalls(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			// Step 0: first tool call
			{
				message.StreamStartChunk{},
				message.ToolCallChunk{ToolCallID: "tc1", ToolName: "tool_a", Input: `{}`},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "tool-calls"}},
			},
			// Step 1: second tool call
			{
				message.StreamStartChunk{},
				message.ToolCallChunk{ToolCallID: "tc2", ToolName: "tool_b", Input: `{}`},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "tool-calls"}},
			},
			// Step 2: final text
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "done"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	chunks := collectChunks(t, RunTurn(
		context.Background(), prov, &mockExecutor{}, store,
		threadID, "", TurnConfig{
			Model:     "test-model",
			UserParts: []message.Part{message.TextPart{Text: "go"}},
		},
	))

	if prov.callIndex != 3 {
		t.Errorf("expected 3 provider calls, got %d", prov.callIndex)
	}

	// Count tool outputs
	toolOutputCount := 0
	for _, c := range chunks {
		if _, ok := c.(message.ToolOutputAvailableChunk); ok {
			toolOutputCount++
		}
	}
	if toolOutputCount != 2 {
		t.Errorf("expected 2 tool outputs, got %d", toolOutputCount)
	}
}

func TestRunTurn_FinishStepAfterToolOutput(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			// Step 0: first tool call (finish-step suppressed for first step)
			{
				message.StreamStartChunk{},
				message.ToolCallChunk{ToolCallID: "tc1", ToolName: "tool_a", Input: `{}`},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "tool-calls"}},
			},
			// Step 1: second tool call
			{
				message.StreamStartChunk{},
				message.ToolCallChunk{ToolCallID: "tc2", ToolName: "tool_b", Input: `{}`},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "tool-calls"}},
			},
			// Step 2: final text
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "done"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	chunks := collectChunks(t, RunTurn(
		context.Background(), prov, &mockExecutor{}, store,
		threadID, "", TurnConfig{
			Model:     "test-model",
			UserParts: []message.Part{message.TextPart{Text: "go"}},
		},
	))

	toolOutputIdx := -1
	finishStepIdx := -1
	startStepCount := 0
	nextStartStepIdx := -1
	for i, c := range chunks {
		switch v := c.(type) {
		case message.ToolOutputAvailableChunk:
			if v.ToolCallID == "tc2" {
				toolOutputIdx = i
			}
		case message.FinishStepChunk:
			if finishStepIdx == -1 {
				finishStepIdx = i
			}
		case message.StartStepChunk:
			startStepCount++
			if startStepCount == 2 {
				nextStartStepIdx = i
			}
		}
	}

	if toolOutputIdx == -1 {
		t.Fatal("missing ToolOutputAvailableChunk for tc2")
	}
	if finishStepIdx == -1 {
		t.Fatal("missing FinishStepChunk for step 1")
	}
	if nextStartStepIdx == -1 {
		t.Fatal("missing StartStepChunk for step 2")
	}
	if toolOutputIdx >= finishStepIdx || finishStepIdx >= nextStartStepIdx {
		t.Fatalf(
			"expected tool output before finish-step before next start-step, got toolOutput=%d finishStep=%d nextStartStep=%d",
			toolOutputIdx,
			finishStepIdx,
			nextStartStepIdx,
		)
	}
}

func TestRunTurn_ProviderExecutedToolSkipped(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	pe := true
	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.ToolCallChunk{
					ToolCallID:       "tc1",
					ToolName:         "code_interpreter",
					Input:            `{"code":"1+1"}`,
					ProviderExecuted: &pe,
				},
				message.ToolResultChunk{
					ToolCallID: "tc1",
					ToolName:   "code_interpreter",
					Result:     json.RawMessage(`"2"`),
				},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "The answer is 2"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	// Executor should NOT be called for provider-executed tools.
	executor := &mockExecutor{results: map[string]message.ToolResultPart{}}

	chunks := collectChunks(t, RunTurn(
		context.Background(), prov, executor, store,
		threadID, "", TurnConfig{
			Model:     "test-model",
			UserParts: []message.Part{message.TextPart{Text: "compute 1+1"}},
		},
	))

	// Should complete in one step (no external tool execution needed).
	if prov.callIndex != 1 {
		t.Errorf("expected 1 provider call, got %d", prov.callIndex)
	}

	hasTextDelta := false
	for _, c := range chunks {
		if _, ok := c.(message.TextDeltaChunk); ok {
			hasTextDelta = true
		}
	}
	if !hasTextDelta {
		t.Error("missing TextDeltaChunk")
	}
}

func TestRunTurn_WithExistingHistory(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	// Pre-populate a conversation.
	if err := store.SaveMessage(threadID, StoredMessage{
		ID:      "msg1",
		Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "hi"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMessage(threadID, StoredMessage{
		ID:       "msg2",
		ParentID: "msg1",
		Message:  message.Message{Role: "assistant", Parts: []message.Part{message.TextPart{Text: "hello"}}},
	}); err != nil {
		t.Fatal(err)
	}

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "Sure!"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	collectChunks(t, RunTurn(
		context.Background(), prov, &mockExecutor{}, store,
		threadID, "msg2", TurnConfig{
			Model:     "test-model",
			UserParts: []message.Part{message.TextPart{Text: "help me"}},
		},
	))

	// The request should contain: existing user, existing assistant, new user.
	if len(prov.requests) != 1 {
		t.Fatal("expected 1 provider call")
	}
	msgs := prov.requests[0].Messages
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages in request, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[1].Role != "assistant" || msgs[2].Role != "user" {
		t.Errorf("unexpected roles: %s, %s, %s", msgs[0].Role, msgs[1].Role, msgs[2].Role)
	}
}

func TestRunTurn_ContextCancellation(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	ctx, cancel := context.WithCancel(context.Background())

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "partial"},
				// No TextEnd or Finish — simulate long response
			},
		},
	}

	chunks := make([]message.MessageChunk, 0)
	for chunk, err := range RunTurn(
		ctx, prov, &mockExecutor{}, store,
		threadID, "", TurnConfig{
			Model:     "test-model",
			UserParts: []message.Part{message.TextPart{Text: "hi"}},
		},
	) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		chunks = append(chunks, chunk)
		// Cancel after receiving the first text delta.
		if _, ok := chunk.(message.TextDeltaChunk); ok {
			cancel()
			break
		}
	}

	_ = cancel // suppress lint
	if len(chunks) == 0 {
		t.Error("expected at least some chunks before cancellation")
	}
}

func TestRunTurn_ToolExecutionError(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.ToolCallChunk{ToolCallID: "tc1", ToolName: "fail_tool", Input: `{}`},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "tool-calls"}},
			},
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "tool failed"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	// Executor returns error for this tool.
	executor := &errorExecutor{err: fmt.Errorf("permission denied")}

	chunks := collectChunks(t, RunTurn(
		context.Background(), prov, executor, store,
		threadID, "", TurnConfig{
			Model:     "test-model",
			UserParts: []message.Part{message.TextPart{Text: "run it"}},
		},
	))

	// Should have a ToolOutputErrorChunk.
	hasToolError := false
	for _, c := range chunks {
		if e, ok := c.(message.ToolOutputErrorChunk); ok {
			hasToolError = true
			if e.ErrorText != "permission denied" {
				t.Errorf("expected error text 'permission denied', got %q", e.ErrorText)
			}
		}
	}
	if !hasToolError {
		t.Error("missing ToolOutputErrorChunk")
	}

	// Turn should still continue (error result sent to model).
	if prov.callIndex != 2 {
		t.Errorf("expected 2 provider calls, got %d", prov.callIndex)
	}
}

type errorExecutor struct {
	err error
}

func (e *errorExecutor) Execute(_ context.Context, _ *ToolContext, _ message.ToolCallPart) (ToolExecuteResult, error) {
	return ToolExecuteResult{}, e.err
}

func (e *errorExecutor) ResolveAnswer(_ *ToolContext, _ message.ToolCallPart, _ api.AnswerQuestionRequest) (ToolExecuteResult, error) {
	return ToolExecuteResult{}, fmt.Errorf("no approvals in error executor")
}

func (e *errorExecutor) ResumeAsync(_ context.Context, _ *ToolContext, _ message.ToolCallPart, _ string, _ *api.AnswerQuestionRequest) (ToolExecuteResult, error) {
	return ToolExecuteResult{}, fmt.Errorf("no async in error executor")
}

func (e *errorExecutor) Continue(ctx context.Context, toolCtx *ToolContext, call message.ToolCallPart, continuation json.RawMessage, req *api.AnswerQuestionRequest) (ToolExecuteResult, error) {
	if req != nil {
		return e.ResolveAnswer(toolCtx, call, *req)
	}
	if len(continuation) > 0 {
		return e.ResumeAsync(ctx, toolCtx, call, "", nil)
	}
	return ToolExecuteResult{}, fmt.Errorf("no continuation in error executor")
}

func (e *errorExecutor) SetPlanMode(_ bool)   {}
func (e *errorExecutor) SetThreadID(_ string) {}

// --- Crash Recovery Tests ---

func TestRunTurn_TurnStateExistsDuringTurn(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.ToolCallChunk{ToolCallID: "tc1", ToolName: "tool_a", Input: `{}`},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "tool-calls"}},
			},
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "done"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	collectChunks(t, RunTurn(
		context.Background(), prov, &mockExecutor{}, store,
		threadID, "", TurnConfig{
			Model:     "test-model",
			UserParts: []message.Part{message.TextPart{Text: "go"}},
		},
	))

	// After turn completes, turn.json should be gone.
	state, _ := store.LoadTurnState(threadID)
	if state != nil {
		t.Error("turn.json should be deleted after successful turn")
	}
}

func TestRunTurn_MessagesPersistedIncrementally(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.ToolCallChunk{ToolCallID: "tc1", ToolName: "tool_a", Input: `{}`},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "tool-calls"}},
			},
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "done"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	collectChunks(t, RunTurn(
		context.Background(), prov, &mockExecutor{}, store,
		threadID, "", TurnConfig{
			Model:     "test-model",
			UserParts: []message.Part{message.TextPart{Text: "go"}},
		},
	))

	// Should have 4 messages: user, assistant (tool call), tool (result), assistant (text)
	// Walk backwards from any leaf to verify chain.
	threads, _ := store.ListThreads()
	if len(threads) != 1 {
		t.Fatalf("expected 1 thread, got %d", len(threads))
	}
}

func TestResumeTurn_CrashedMidStreaming(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	// Simulate: turn started, user message saved, but streaming crashed.
	userMsgID := "user-msg-1"
	if err := store.SaveMessage(threadID, StoredMessage{
		ID:      userMsgID,
		Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "hello"}}},
	}); err != nil {
		t.Fatal(err)
	}

	turnState := &TurnState{
		ID:          "turn1",
		ThreadID:    threadID,
		LeafID:      "",
		CurrentStep: 0,
		Phase:       PhaseStreaming,
		LeafMsgID:   userMsgID,
		Config: TurnConfig{
			Model: "test-model",
		},
	}
	if err := store.SaveTurnState(threadID, *turnState); err != nil {
		t.Fatal(err)
	}

	// No step-result file exists (crashed mid-streaming).
	// On resume, the completion should be restarted.
	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "Hi there!"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	chunks := collectChunks(t, ResumeTurn(
		context.Background(), prov, &mockExecutor{}, store, turnState,
	))

	if prov.callIndex != 1 {
		t.Errorf("expected 1 provider call (restart), got %d", prov.callIndex)
	}

	hasTextDelta := false
	for _, c := range chunks {
		if _, ok := c.(message.TextDeltaChunk); ok {
			hasTextDelta = true
		}
	}
	if !hasTextDelta {
		t.Error("missing TextDeltaChunk after resume")
	}

	// turn.json should be cleaned up.
	state, _ := store.LoadTurnState(threadID)
	if state != nil {
		t.Error("turn.json should be deleted after resumed turn completes")
	}
}

func TestResumeTurn_CrashedMidToolExecution(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	// Simulate: step 0 streaming completed, 2 tool calls, only 1 completed.
	userMsgID := "user-msg-1"
	if err := store.SaveMessage(threadID, StoredMessage{
		ID:      userMsgID,
		Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "do stuff"}}},
	}); err != nil {
		t.Fatal(err)
	}

	turnID := "turn1"
	turnState := &TurnState{
		ID:          turnID,
		ThreadID:    threadID,
		CurrentStep: 0,
		Phase:       PhaseTools,
		LeafMsgID:   userMsgID,
		Config: TurnConfig{
			Model: "test-model",
		},
	}
	if err := store.SaveTurnState(threadID, *turnState); err != nil {
		t.Fatal(err)
	}

	// Create step result with 2 tool calls.
	stepResult := StepResult{
		AssistantMessage: message.Message{
			Role: "assistant",
			Parts: []message.Part{
				message.ToolCallPart{ToolCallID: "tc1", ToolName: "bash", Input: `{"cmd":"ls"}`},
				message.ToolCallPart{ToolCallID: "tc2", ToolName: "bash", Input: `{"cmd":"pwd"}`},
			},
		},
		ToolCalls: []ToolCallInfo{
			{ToolCallID: "tc1", ToolName: "bash", Input: `{"cmd":"ls"}`},
			{ToolCallID: "tc2", ToolName: "bash", Input: `{"cmd":"pwd"}`},
		},
	}

	// Need step file to create dirs.
	sf, _ := store.CreateStepFile(threadID, turnID, 0)
	sf.Close()

	if err := store.SaveStepResult(threadID, turnID, 0, stepResult); err != nil {
		t.Fatal(err)
	}

	// Only tc1 completed before crash.
	toolResults := StepToolResults{
		Results: []message.ToolResultPart{
			{
				ToolCallID: "tc1",
				ToolName:   "bash",
				Output:     message.TextOutput{Value: "file1.txt"},
			},
		},
	}
	if err := store.SaveToolResults(threadID, turnID, 0, toolResults); err != nil {
		t.Fatal(err)
	}

	// Resume should: fill in tc2 with "interrupted" error, continue to next step.
	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "recovered"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	chunks := collectChunks(t, ResumeTurn(
		context.Background(), prov, &mockExecutor{}, store, turnState,
	))

	// Provider should be called once (for step 1, after recovery).
	if prov.callIndex != 1 {
		t.Errorf("expected 1 provider call, got %d", prov.callIndex)
	}

	// The request should include the tool result (including the interrupted one).
	if len(prov.requests) != 1 {
		t.Fatal("expected 1 request")
	}
	msgs := prov.requests[0].Messages
	results := toolResultParts(msgs)
	if len(results) != 2 {
		t.Fatalf("expected 2 tool results, got %d", len(results))
	}

	// tc2 should have the "interrupted" error.
	tc2Result := results[1]
	errOut, ok := tc2Result.Output.(message.ErrorTextOutput)
	if !ok {
		t.Fatalf("expected ErrorTextOutput for interrupted tool, got %T", tc2Result.Output)
	}
	if errOut.Value != "interrupted by transient system failure" {
		t.Errorf("expected interrupted message, got %q", errOut.Value)
	}

	hasTextDelta := false
	for _, c := range chunks {
		if _, ok := c.(message.TextDeltaChunk); ok {
			hasTextDelta = true
		}
	}
	if !hasTextDelta {
		t.Error("missing TextDeltaChunk after resume")
	}

	// turn.json should be cleaned up.
	state, _ := store.LoadTurnState(threadID)
	if state != nil {
		t.Error("turn.json should be deleted after resumed turn completes")
	}
}

func TestResumeTurn_CrashedAfterStreamingNoTools(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	// Simulate: step result saved (no tool calls), but messages weren't saved before crash.
	userMsgID := "user-msg-1"
	if err := store.SaveMessage(threadID, StoredMessage{
		ID:      userMsgID,
		Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "hi"}}},
	}); err != nil {
		t.Fatal(err)
	}

	turnID := "turn1"
	turnState := &TurnState{
		ID:          turnID,
		ThreadID:    threadID,
		CurrentStep: 0,
		Phase:       PhaseStreaming,
		LeafMsgID:   userMsgID,
		Config:      TurnConfig{Model: "test-model"},
	}
	if err := store.SaveTurnState(threadID, *turnState); err != nil {
		t.Fatal(err)
	}

	// Step result exists with no tool calls.
	sf, _ := store.CreateStepFile(threadID, turnID, 0)
	sf.Close()
	if err := store.SaveStepResult(threadID, turnID, 0, StepResult{
		AssistantMessage: message.Message{
			Role:  "assistant",
			Parts: []message.Part{message.TextPart{Text: "hello!"}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Resume should: save the assistant message, delete turn.json, no provider calls needed.
	prov := &mockProvider{}

	collectChunks(t, ResumeTurn(
		context.Background(), prov, &mockExecutor{}, store, turnState,
	))

	// No provider calls needed — the recovery saved the message.
	// The step loop may or may not be entered depending on currentStep advancement.
	// But turn.json should be cleaned up.
	state, _ := store.LoadTurnState(threadID)
	if state != nil {
		t.Error("turn.json should be deleted after resumed turn completes")
	}
}

// TestResumeTurn_CrashedAfterCompletionBeforeToolExecution tests scenario #2:
// The LLM completion finished and step-result.json was saved (with tool calls),
// but we crashed before any tools were executed (no tool-results.json).
// Recovery should NOT re-run the LLM completion — it should use the existing
// step result and execute tools, then continue to the next LLM call.
func TestResumeTurn_CrashedAfterCompletionBeforeToolExecution(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	userMsgID := "user-msg-1"
	if err := store.SaveMessage(threadID, StoredMessage{
		ID:      userMsgID,
		Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "read the file"}}},
	}); err != nil {
		t.Fatal(err)
	}

	turnID := "turn1"
	turnState := &TurnState{
		ID:          turnID,
		ThreadID:    threadID,
		CurrentStep: 0,
		// Phase is still "streaming" — crash happened after SaveStepResult
		// but before SaveTurnState set phase to "tools".
		Phase:     PhaseStreaming,
		LeafMsgID: userMsgID,
		Config:    TurnConfig{Model: "test-model"},
	}
	if err := store.SaveTurnState(threadID, *turnState); err != nil {
		t.Fatal(err)
	}

	// Step result exists with tool calls (completion finished successfully).
	sf, _ := store.CreateStepFile(threadID, turnID, 0)
	sf.Close()

	stepResult := StepResult{
		AssistantMessage: message.Message{
			Role: "assistant",
			Parts: []message.Part{
				message.ToolCallPart{ToolCallID: "tc1", ToolName: "read_file", Input: `{"path":"test.txt"}`},
			},
		},
		ToolCalls: []ToolCallInfo{
			{ToolCallID: "tc1", ToolName: "read_file", Input: `{"path":"test.txt"}`},
		},
	}
	if err := store.SaveStepResult(threadID, turnID, 0, stepResult); err != nil {
		t.Fatal(err)
	}

	// NO tool-results.json — no tools were started before the crash.
	// The tool should be EXECUTED (not marked interrupted) since it never started.

	// Provider should be called exactly ONCE — for the NEXT step after
	// recovery executes the tool and completes step 0.
	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			// Step 1: final text response after tool execution
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "recovered from crash"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	// Executor returns a specific result so we can verify it was called.
	executorResult := message.ToolResultPart{
		ToolCallID: "tc1",
		ToolName:   "read_file",
		Output:     message.TextOutput{Value: "file contents here"},
	}
	exec := &mockExecutor{results: map[string]message.ToolResultPart{
		"tc1": executorResult,
	}}

	chunks := collectChunks(t, ResumeTurn(
		context.Background(), prov, exec, store, turnState,
	))

	// Critical: provider should be called only once (for step 1).
	// The step 0 completion should NOT be re-run.
	if prov.callIndex != 1 {
		t.Errorf("expected 1 provider call (step 1 only, not re-running step 0), got %d", prov.callIndex)
	}

	// The request to the provider should include the EXECUTED tool result in history.
	if len(prov.requests) != 1 {
		t.Fatal("expected 1 request")
	}
	msgs := prov.requests[0].Messages
	// Should have: user, assistant (tool call), tool (executed result)
	if len(msgs) < 3 {
		t.Fatalf("expected at least 3 messages in request, got %d", len(msgs))
	}

	// Find the tool message and verify the tool was executed (not interrupted).
	var toolMsg *message.Message
	for i := range msgs {
		if msgs[i].Role == "tool" {
			toolMsg = &msgs[i]
			break
		}
	}
	if toolMsg == nil {
		t.Fatal("expected tool message in history")
	}
	if len(toolMsg.Parts) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(toolMsg.Parts))
	}
	tc1Result, ok := toolMsg.Parts[0].(message.ToolResultPart)
	if !ok {
		t.Fatalf("expected ToolResultPart, got %T", toolMsg.Parts[0])
	}
	textOut, ok := tc1Result.Output.(message.TextOutput)
	if !ok {
		t.Fatalf("expected TextOutput (tool was executed), got %T", tc1Result.Output)
	}
	if textOut.Value != "file contents here" {
		t.Errorf("expected executed tool output, got %q", textOut.Value)
	}

	// Verify text was received from the resumed step.
	hasTextDelta := false
	for _, c := range chunks {
		if _, ok := c.(message.TextDeltaChunk); ok {
			hasTextDelta = true
		}
	}
	if !hasTextDelta {
		t.Error("missing TextDeltaChunk after resume")
	}

	// turn.json should be cleaned up.
	state, _ := store.LoadTurnState(threadID)
	if state != nil {
		t.Error("turn.json should be deleted after resumed turn completes")
	}
}

// TestResumeTurn_CrashedAfterAllToolsBeforeNextStep tests scenario #4:
// All tool calls completed (tool-results.json is complete), but we crashed
// before the turn state was updated for the next step.
// Recovery should save the messages and continue to the next LLM call
// without re-executing any tools.
func TestResumeTurn_CrashedAfterAllToolsBeforeNextStep(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	userMsgID := "user-msg-1"
	if err := store.SaveMessage(threadID, StoredMessage{
		ID:      userMsgID,
		Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "run both tools"}}},
	}); err != nil {
		t.Fatal(err)
	}

	turnID := "turn1"
	turnState := &TurnState{
		ID:          turnID,
		ThreadID:    threadID,
		CurrentStep: 0,
		Phase:       PhaseTools, // phase was updated to tools
		LeafMsgID:   userMsgID,
		Config:      TurnConfig{Model: "test-model"},
	}
	if err := store.SaveTurnState(threadID, *turnState); err != nil {
		t.Fatal(err)
	}

	// Step result with 2 tool calls.
	sf, _ := store.CreateStepFile(threadID, turnID, 0)
	sf.Close()

	stepResult := StepResult{
		AssistantMessage: message.Message{
			Role: "assistant",
			Parts: []message.Part{
				message.ToolCallPart{ToolCallID: "tc1", ToolName: "bash", Input: `{"cmd":"ls"}`},
				message.ToolCallPart{ToolCallID: "tc2", ToolName: "bash", Input: `{"cmd":"pwd"}`},
			},
		},
		ToolCalls: []ToolCallInfo{
			{ToolCallID: "tc1", ToolName: "bash", Input: `{"cmd":"ls"}`},
			{ToolCallID: "tc2", ToolName: "bash", Input: `{"cmd":"pwd"}`},
		},
	}
	if err := store.SaveStepResult(threadID, turnID, 0, stepResult); err != nil {
		t.Fatal(err)
	}

	// ALL tool results present — both completed before the crash.
	toolResults := StepToolResults{
		Results: []message.ToolResultPart{
			{
				ToolCallID: "tc1",
				ToolName:   "bash",
				Output:     message.TextOutput{Value: "file1.txt\nfile2.txt"},
			},
			{
				ToolCallID: "tc2",
				ToolName:   "bash",
				Output:     message.TextOutput{Value: "/workspace"},
			},
		},
	}
	if err := store.SaveToolResults(threadID, turnID, 0, toolResults); err != nil {
		t.Fatal(err)
	}

	// Provider should be called once — for the next step after recovery.
	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "done"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	// Executor should NOT be called — all tools already completed.
	callCount := 0
	executor := &countingExecutor{count: &callCount}

	chunks := collectChunks(t, ResumeTurn(
		context.Background(), prov, executor, store, turnState,
	))

	// No tools should have been re-executed.
	if callCount != 0 {
		t.Errorf("expected 0 tool executions (all completed before crash), got %d", callCount)
	}

	// Provider should be called once (for the next step).
	if prov.callIndex != 1 {
		t.Errorf("expected 1 provider call, got %d", prov.callIndex)
	}

	// The request should have both tool results (not interrupted errors).
	if len(prov.requests) != 1 {
		t.Fatal("expected 1 request")
	}
	msgs := prov.requests[0].Messages
	results := toolResultParts(msgs)
	if len(results) != 2 {
		t.Fatalf("expected 2 tool results, got %d", len(results))
	}

	// Both should be successful results, not interrupted errors.
	for i, result := range results {
		if _, isErr := result.Output.(message.ErrorTextOutput); isErr {
			t.Errorf("part[%d]: expected successful result, got error", i)
		}
	}

	hasTextDelta := false
	for _, c := range chunks {
		if _, ok := c.(message.TextDeltaChunk); ok {
			hasTextDelta = true
		}
	}
	if !hasTextDelta {
		t.Error("missing TextDeltaChunk after resume")
	}

	state, _ := store.LoadTurnState(threadID)
	if state != nil {
		t.Error("turn.json should be deleted after resumed turn completes")
	}
}

// TestResumeTurn_CrashedAfterCompletionWithToolCalls_PhaseTools tests scenario #2 variant:
// Same as CrashedAfterCompletionBeforeToolExecution but with phase already set to "tools".
// This verifies the tools phase with no tool-results.json yet — all tools get interrupted.
func TestResumeTurn_CrashedAfterCompletionWithToolCalls_PhaseTools(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	userMsgID := "user-msg-1"
	if err := store.SaveMessage(threadID, StoredMessage{
		ID:      userMsgID,
		Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "do it"}}},
	}); err != nil {
		t.Fatal(err)
	}

	turnID := "turn1"
	turnState := &TurnState{
		ID:          turnID,
		ThreadID:    threadID,
		CurrentStep: 0,
		// Phase is "tools" — SaveTurnState succeeded, but executor.Execute
		// was never called before crash.
		Phase:     PhaseTools,
		LeafMsgID: userMsgID,
		Config:    TurnConfig{Model: "test-model"},
	}
	if err := store.SaveTurnState(threadID, *turnState); err != nil {
		t.Fatal(err)
	}

	sf, _ := store.CreateStepFile(threadID, turnID, 0)
	sf.Close()

	stepResult := StepResult{
		AssistantMessage: message.Message{
			Role: "assistant",
			Parts: []message.Part{
				message.ToolCallPart{ToolCallID: "tc1", ToolName: "bash", Input: `{"cmd":"ls"}`},
				message.ToolCallPart{ToolCallID: "tc2", ToolName: "bash", Input: `{"cmd":"pwd"}`},
			},
		},
		ToolCalls: []ToolCallInfo{
			{ToolCallID: "tc1", ToolName: "bash", Input: `{"cmd":"ls"}`},
			{ToolCallID: "tc2", ToolName: "bash", Input: `{"cmd":"pwd"}`},
		},
	}
	if err := store.SaveStepResult(threadID, turnID, 0, stepResult); err != nil {
		t.Fatal(err)
	}

	// No tool-results.json — crash happened before any tool was executed.
	// Both tools should be EXECUTED (not marked interrupted).

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "after recovery"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	callCount := 0
	exec := &countingExecutor{count: &callCount}

	collectChunks(t, ResumeTurn(
		context.Background(), prov, exec, store, turnState,
	))

	// Both tools should have been executed.
	if callCount != 2 {
		t.Errorf("expected 2 tool executions (both never started), got %d", callCount)
	}

	// Provider called once (next step only, NOT re-running step 0 completion).
	if prov.callIndex != 1 {
		t.Errorf("expected 1 provider call, got %d", prov.callIndex)
	}

	// Both tools should have successful execution results (not interrupted).
	msgs := prov.requests[0].Messages
	for i, result := range toolResultParts(msgs) {
		if _, isErr := result.Output.(message.ErrorTextOutput); isErr {
			t.Errorf("part[%d]: expected executed result, got interrupted error", i)
		}
	}

	state, _ := store.LoadTurnState(threadID)
	if state != nil {
		t.Error("turn.json should be deleted after resumed turn completes")
	}
}

// TestResumeTurn_MultiStep_CrashedOnSecondStep tests crash during a multi-step turn.
// Step 0 completed fully (tools executed, messages saved, step advanced to 1).
// Step 1 crashes mid-streaming. On resume, step 0 should NOT be re-run.
func TestResumeTurn_MultiStep_CrashedOnSecondStep(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	// Pre-populate: user → assistant (tool call) → tool (result)
	// These represent step 0 which completed successfully.
	userMsgID := "user-msg-1"
	if err := store.SaveMessage(threadID, StoredMessage{
		ID:      userMsgID,
		Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "multi-step"}}},
	}); err != nil {
		t.Fatal(err)
	}

	assistantMsgID := "asst-msg-1"
	if err := store.SaveMessage(threadID, StoredMessage{
		ID:       assistantMsgID,
		ParentID: userMsgID,
		Message: message.Message{
			Role: "assistant",
			Parts: []message.Part{
				message.ToolCallPart{ToolCallID: "tc1", ToolName: "bash", Input: `{"cmd":"ls"}`},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	toolMsgID := "tool-msg-1"
	if err := store.SaveMessage(threadID, StoredMessage{
		ID:       toolMsgID,
		ParentID: assistantMsgID,
		Message: message.Message{
			Role: "tool",
			Parts: []message.Part{
				message.ToolResultPart{ToolCallID: "tc1", ToolName: "bash", Output: message.TextOutput{Value: "file.txt"}},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	turnID := "turn1"
	turnState := &TurnState{
		ID:          turnID,
		ThreadID:    threadID,
		CurrentStep: 1, // Step 0 done, crashed during step 1 streaming
		Phase:       PhaseStreaming,
		LeafMsgID:   toolMsgID,
		Config:      TurnConfig{Model: "test-model"},
	}
	if err := store.SaveTurnState(threadID, *turnState); err != nil {
		t.Fatal(err)
	}

	// No step-result for step 1 (crashed mid-streaming).

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			// Step 1: re-run completion from where we left off
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "final answer"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	chunks := collectChunks(t, ResumeTurn(
		context.Background(), prov, &mockExecutor{}, store, turnState,
	))

	// Provider should be called once (for step 1 restart).
	if prov.callIndex != 1 {
		t.Errorf("expected 1 provider call, got %d", prov.callIndex)
	}

	// The request should include the full history: user, assistant, tool.
	msgs := prov.requests[0].Messages
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages in history (user, assistant, tool), got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[1].Role != "assistant" || msgs[2].Role != "tool" {
		t.Errorf("unexpected roles: %s, %s, %s", msgs[0].Role, msgs[1].Role, msgs[2].Role)
	}

	hasTextDelta := false
	for _, c := range chunks {
		if _, ok := c.(message.TextDeltaChunk); ok {
			hasTextDelta = true
		}
	}
	if !hasTextDelta {
		t.Error("missing TextDeltaChunk after resume")
	}

	state, _ := store.LoadTurnState(threadID)
	if state != nil {
		t.Error("turn.json should be deleted after resumed turn completes")
	}
}

// TestResumeTurn_CrashedMidTools_MixedRecovery tests the mixed scenario:
// 3 tool calls, tool 1 completed, tool 2 was in-progress (interrupted),
// tool 3 never started (should be executed fresh).
func TestResumeTurn_CrashedMidTools_MixedRecovery(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	userMsgID := "user-msg-1"
	if err := store.SaveMessage(threadID, StoredMessage{
		ID:      userMsgID,
		Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "do three things"}}},
	}); err != nil {
		t.Fatal(err)
	}

	turnID := "turn1"
	turnState := &TurnState{
		ID:          turnID,
		ThreadID:    threadID,
		CurrentStep: 0,
		Phase:       PhaseTools,
		LeafMsgID:   userMsgID,
		Config:      TurnConfig{Model: "test-model"},
	}
	if err := store.SaveTurnState(threadID, *turnState); err != nil {
		t.Fatal(err)
	}

	// 3 tool calls in the step result.
	sf, _ := store.CreateStepFile(threadID, turnID, 0)
	sf.Close()

	stepResult := StepResult{
		AssistantMessage: message.Message{
			Role: "assistant",
			Parts: []message.Part{
				message.ToolCallPart{ToolCallID: "tc1", ToolName: "bash", Input: `{"cmd":"ls"}`},
				message.ToolCallPart{ToolCallID: "tc2", ToolName: "bash", Input: `{"cmd":"pwd"}`},
				message.ToolCallPart{ToolCallID: "tc3", ToolName: "bash", Input: `{"cmd":"whoami"}`},
			},
		},
		ToolCalls: []ToolCallInfo{
			{ToolCallID: "tc1", ToolName: "bash", Input: `{"cmd":"ls"}`},
			{ToolCallID: "tc2", ToolName: "bash", Input: `{"cmd":"pwd"}`},
			{ToolCallID: "tc3", ToolName: "bash", Input: `{"cmd":"whoami"}`},
		},
	}
	if err := store.SaveStepResult(threadID, turnID, 0, stepResult); err != nil {
		t.Fatal(err)
	}

	// Only tc1 completed before crash. tc2 was in-progress, tc3 never started.
	toolResults := StepToolResults{
		Results: []message.ToolResultPart{
			{
				ToolCallID: "tc1",
				ToolName:   "bash",
				Output:     message.TextOutput{Value: "file1.txt"},
			},
		},
	}
	if err := store.SaveToolResults(threadID, turnID, 0, toolResults); err != nil {
		t.Fatal(err)
	}

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "done"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	// Track which tools the executor is called for.
	callCount := 0
	exec := &countingExecutor{count: &callCount}

	collectChunks(t, ResumeTurn(
		context.Background(), prov, exec, store, turnState,
	))

	// Only tc3 should be executed. tc1 was already done, tc2 was interrupted.
	if callCount != 1 {
		t.Errorf("expected 1 tool execution (tc3 only), got %d", callCount)
	}

	// Verify the tool message sent to the provider.
	if len(prov.requests) != 1 {
		t.Fatal("expected 1 provider request")
	}
	msgs := prov.requests[0].Messages
	results := toolResultParts(msgs)
	if len(results) != 3 {
		t.Fatalf("expected 3 tool results, got %d", len(results))
	}

	// tc1: completed (saved result reused).
	tc1 := results[0]
	if _, isErr := tc1.Output.(message.ErrorTextOutput); isErr {
		t.Error("tc1: should be completed result, not error")
	}
	if tc1.ToolCallID != "tc1" {
		t.Errorf("tc1: expected toolCallId 'tc1', got %q", tc1.ToolCallID)
	}

	// tc2: interrupted (was in-progress when crash happened).
	tc2 := results[1]
	errOut, ok := tc2.Output.(message.ErrorTextOutput)
	if !ok {
		t.Fatalf("tc2: expected ErrorTextOutput (interrupted), got %T", tc2.Output)
	}
	if errOut.Value != "interrupted by transient system failure" {
		t.Errorf("tc2: expected interrupted message, got %q", errOut.Value)
	}

	// tc3: executed (never started before crash).
	tc3 := results[2]
	if _, isErr := tc3.Output.(message.ErrorTextOutput); isErr {
		t.Error("tc3: should be executed result, not error")
	}

	state, _ := store.LoadTurnState(threadID)
	if state != nil {
		t.Error("turn.json should be deleted after resumed turn completes")
	}
}

// countingExecutor tracks how many times Execute was called.
type countingExecutor struct {
	count *int
}

func (e *countingExecutor) Execute(_ context.Context, _ *ToolContext, call message.ToolCallPart) (ToolExecuteResult, error) {
	*e.count++
	return ToolExecuteResult{Result: message.ToolResultPart{
		ToolCallID: call.ToolCallID,
		ToolName:   call.ToolName,
		Output:     message.TextOutput{Value: "output"},
	}}, nil
}

func (e *countingExecutor) ResolveAnswer(_ *ToolContext, _ message.ToolCallPart, _ api.AnswerQuestionRequest) (ToolExecuteResult, error) {
	return ToolExecuteResult{}, fmt.Errorf("no approvals in counting executor")
}

func (e *countingExecutor) ResumeAsync(_ context.Context, _ *ToolContext, _ message.ToolCallPart, _ string, _ *api.AnswerQuestionRequest) (ToolExecuteResult, error) {
	return ToolExecuteResult{}, fmt.Errorf("no async in counting executor")
}

func (e *countingExecutor) Continue(ctx context.Context, toolCtx *ToolContext, call message.ToolCallPart, continuation json.RawMessage, req *api.AnswerQuestionRequest) (ToolExecuteResult, error) {
	if req != nil {
		return e.ResolveAnswer(toolCtx, call, *req)
	}
	if len(continuation) > 0 {
		return e.ResumeAsync(ctx, toolCtx, call, "", nil)
	}
	return ToolExecuteResult{}, fmt.Errorf("no continuation in counting executor")
}

func (e *countingExecutor) SetPlanMode(_ bool)   {}
func (e *countingExecutor) SetThreadID(_ string) {}

// --- Async Executor Mock ---

// asyncMockExecutor returns AsyncContinuationHandle for tool calls in asyncToolIDs,
// and sync results for everything else. ResumeAsync behaviour is configurable.
type asyncMockExecutor struct {
	// asyncToolIDs maps toolCallID → taskID for tools that should run async.
	asyncToolIDs map[string]string
	// results maps toolCallID → result (used for both sync and async Wait).
	results map[string]message.ToolResultPart
	// waitApprovals maps toolCallID → approval questions returned by Wait.
	waitApprovals map[string]json.RawMessage
	// waitErrors maps toolCallID → error returned by Wait.
	waitErrors map[string]error
	// resumeResults maps taskID → result for ResumeAsync (nil Async = completed).
	resumeResults map[string]ToolExecuteResult
	// resumeErrors maps taskID → error for ResumeAsync.
	resumeErrors map[string]error
	// resumeRequests captures answer payloads passed to ResumeAsync.
	resumeRequests map[string]api.AnswerQuestionRequest
}

func (e *asyncMockExecutor) Execute(_ context.Context, _ *ToolContext, call message.ToolCallPart) (ToolExecuteResult, error) {
	if taskID, ok := e.asyncToolIDs[call.ToolCallID]; ok {
		result := e.results[call.ToolCallID]
		waitApproval := e.waitApprovals[call.ToolCallID]
		waitErr := e.waitErrors[call.ToolCallID]
		continuation := testContinuation(taskID)
		return ToolExecuteResult{
			Async: &AsyncContinuationHandle{
				Continuation: continuation,
				Wait: func(_ context.Context) (AsyncWaitResult, error) {
					if waitErr != nil {
						return AsyncWaitResult{}, waitErr
					}
					if len(waitApproval) > 0 {
						return AsyncWaitResult{
							Approval: &ApprovalRequest{Questions: waitApproval, Continuation: continuation},
						}, nil
					}
					return AsyncWaitResult{Result: result}, nil
				},
			},
		}, nil
	}
	if result, ok := e.results[call.ToolCallID]; ok {
		return ToolExecuteResult{Result: result}, nil
	}
	return ToolExecuteResult{Result: message.ToolResultPart{
		ToolCallID: call.ToolCallID,
		ToolName:   call.ToolName,
		Output:     message.TextOutput{Value: "default output"},
	}}, nil
}

func (e *asyncMockExecutor) ResolveAnswer(_ *ToolContext, _ message.ToolCallPart, _ api.AnswerQuestionRequest) (ToolExecuteResult, error) {
	return ToolExecuteResult{}, fmt.Errorf("no approvals in async mock")
}

func (e *asyncMockExecutor) ResumeAsync(_ context.Context, _ *ToolContext, _ message.ToolCallPart, taskID string, req *api.AnswerQuestionRequest) (ToolExecuteResult, error) {
	if req != nil {
		if e.resumeRequests == nil {
			e.resumeRequests = make(map[string]api.AnswerQuestionRequest)
		}
		e.resumeRequests[taskID] = *req
	}
	if err, ok := e.resumeErrors[taskID]; ok {
		return ToolExecuteResult{}, err
	}
	if result, ok := e.resumeResults[taskID]; ok {
		return result, nil
	}
	return ToolExecuteResult{}, fmt.Errorf("unknown task: %s", taskID)
}

func (e *asyncMockExecutor) Continue(ctx context.Context, toolCtx *ToolContext, call message.ToolCallPart, continuation json.RawMessage, req *api.AnswerQuestionRequest) (ToolExecuteResult, error) {
	if req != nil {
		var decoded struct {
			SubThreadID string `json:"subThreadId"`
		}
		if err := json.Unmarshal(continuation, &decoded); err != nil {
			return ToolExecuteResult{}, err
		}
		return e.ResumeAsync(ctx, toolCtx, call, decoded.SubThreadID, req)
	}
	var decoded struct {
		SubThreadID string `json:"subThreadId"`
	}
	if err := json.Unmarshal(continuation, &decoded); err != nil {
		return ToolExecuteResult{}, err
	}
	return e.ResumeAsync(ctx, toolCtx, call, decoded.SubThreadID, nil)
}

func (e *asyncMockExecutor) SetPlanMode(_ bool)   {}
func (e *asyncMockExecutor) SetThreadID(_ string) {}

// --- Async Tests ---

func TestRunTurn_AsyncTool_Single(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			// Step 0: one async tool call
			{
				message.StreamStartChunk{},
				message.ToolCallChunk{ToolCallID: "tc1", ToolName: "launch_task", Input: `{"task":"do stuff"}`},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "tool-calls"}},
			},
			// Step 1: final text response
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "Done!"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	exec := &asyncMockExecutor{
		asyncToolIDs: map[string]string{"tc1": "task-abc"},
		results: map[string]message.ToolResultPart{
			"tc1": {ToolCallID: "tc1", ToolName: "launch_task", Output: message.TextOutput{Value: "task completed"}},
		},
	}

	chunks := collectChunks(t, RunTurn(context.Background(), prov, exec, store, threadID, "", TurnConfig{
		Model:     "test-model",
		UserParts: []message.Part{message.TextPart{Text: "run a task"}},
	}))

	// Should see tool output chunk from the async result and final text.
	var hasToolOutput, hasFinalText bool
	for _, c := range chunks {
		switch v := c.(type) {
		case message.ToolOutputAvailableChunk:
			if v.ToolCallID == "tc1" {
				hasToolOutput = true
			}
		case message.TextDeltaChunk:
			if v.Delta == "Done!" {
				hasFinalText = true
			}
		}
	}

	if !hasToolOutput {
		t.Error("expected tool output chunk for async tool tc1")
	}
	if !hasFinalText {
		t.Error("expected final text response after async tool completion")
	}

	// Verify the LLM received the tool result in the second call's history.
	if len(prov.requests) < 2 {
		t.Fatalf("expected at least 2 LLM calls, got %d", len(prov.requests))
	}
	lastMsg := prov.requests[1].Messages[len(prov.requests[1].Messages)-1]
	if lastMsg.Role != "tool" {
		t.Errorf("expected tool message in history, got role=%s", lastMsg.Role)
	}
}

func TestRunTurn_AsyncTool_MultipleParallel(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			// Step 0: three async tool calls
			{
				message.StreamStartChunk{},
				message.ToolCallChunk{ToolCallID: "tc1", ToolName: "launch_task", Input: `{"id":1}`},
				message.ToolCallChunk{ToolCallID: "tc2", ToolName: "launch_task", Input: `{"id":2}`},
				message.ToolCallChunk{ToolCallID: "tc3", ToolName: "launch_task", Input: `{"id":3}`},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "tool-calls"}},
			},
			// Step 1: final response
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "All done"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	exec := &asyncMockExecutor{
		asyncToolIDs: map[string]string{
			"tc1": "task-1",
			"tc2": "task-2",
			"tc3": "task-3",
		},
		results: map[string]message.ToolResultPart{
			"tc1": {ToolCallID: "tc1", ToolName: "launch_task", Output: message.TextOutput{Value: "result-1"}},
			"tc2": {ToolCallID: "tc2", ToolName: "launch_task", Output: message.TextOutput{Value: "result-2"}},
			"tc3": {ToolCallID: "tc3", ToolName: "launch_task", Output: message.TextOutput{Value: "result-3"}},
		},
	}

	chunks := collectChunks(t, RunTurn(context.Background(), prov, exec, store, threadID, "", TurnConfig{
		Model:     "test-model",
		UserParts: []message.Part{message.TextPart{Text: "run tasks"}},
	}))

	// Count tool output chunks.
	toolOutputs := 0
	for _, c := range chunks {
		if _, ok := c.(message.ToolOutputAvailableChunk); ok {
			toolOutputs++
		}
	}
	if toolOutputs != 3 {
		t.Errorf("expected 3 tool output chunks, got %d", toolOutputs)
	}

	// Verify tool results appear in correct order in history.
	if len(prov.requests) < 2 {
		t.Fatalf("expected at least 2 LLM calls, got %d", len(prov.requests))
	}
	msgs := prov.requests[1].Messages
	results := toolResultParts(msgs)
	if len(results) != 3 {
		t.Fatalf("expected 3 tool results, got %d", len(results))
	}
	gotIDs := map[string]bool{}
	for _, tr := range results {
		gotIDs[tr.ToolCallID] = true
	}
	for _, expectedID := range []string{"tc1", "tc2", "tc3"} {
		if !gotIDs[expectedID] {
			t.Errorf("missing toolCallID=%s", expectedID)
		}
	}
}

func TestRunTurn_AsyncTool_MixedSyncAsync(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			// Step 0: sync tool, async tool, sync tool
			{
				message.StreamStartChunk{},
				message.ToolCallChunk{ToolCallID: "tc1", ToolName: "read_file", Input: `{}`},
				message.ToolCallChunk{ToolCallID: "tc2", ToolName: "launch_task", Input: `{}`},
				message.ToolCallChunk{ToolCallID: "tc3", ToolName: "write_file", Input: `{}`},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "tool-calls"}},
			},
			// Step 1: final response
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "Done"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	exec := &asyncMockExecutor{
		asyncToolIDs: map[string]string{"tc2": "task-bg"},
		results: map[string]message.ToolResultPart{
			"tc1": {ToolCallID: "tc1", ToolName: "read_file", Output: message.TextOutput{Value: "file content"}},
			"tc2": {ToolCallID: "tc2", ToolName: "launch_task", Output: message.TextOutput{Value: "task done"}},
			"tc3": {ToolCallID: "tc3", ToolName: "write_file", Output: message.TextOutput{Value: "written"}},
		},
	}

	chunks := collectChunks(t, RunTurn(context.Background(), prov, exec, store, threadID, "", TurnConfig{
		Model:     "test-model",
		UserParts: []message.Part{message.TextPart{Text: "mix"}},
	}))

	// All three should produce tool output chunks.
	toolOutputs := map[string]bool{}
	for _, c := range chunks {
		if v, ok := c.(message.ToolOutputAvailableChunk); ok {
			toolOutputs[v.ToolCallID] = true
		}
	}
	for _, id := range []string{"tc1", "tc2", "tc3"} {
		if !toolOutputs[id] {
			t.Errorf("missing tool output for %s", id)
		}
	}

	msgs := prov.requests[1].Messages
	results := toolResultParts(msgs)
	if len(results) != 3 {
		t.Fatalf("expected 3 tool results, got %d", len(results))
	}
	gotIDs := map[string]bool{}
	for _, tr := range results {
		gotIDs[tr.ToolCallID] = true
	}
	for _, expectedID := range []string{"tc1", "tc2", "tc3"} {
		if !gotIDs[expectedID] {
			t.Errorf("missing %s", expectedID)
		}
	}
}

func TestRunTurn_AsyncTool_WaitError(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			// Step 0: one async tool
			{
				message.StreamStartChunk{},
				message.ToolCallChunk{ToolCallID: "tc1", ToolName: "launch_task", Input: `{}`},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "tool-calls"}},
			},
			// Step 1: final response (LLM handles the error)
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "The task failed"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	exec := &asyncMockExecutor{
		asyncToolIDs: map[string]string{"tc1": "task-fail"},
		results:      map[string]message.ToolResultPart{},
		waitErrors:   map[string]error{"tc1": fmt.Errorf("sub-agent crashed")},
	}

	chunks := collectChunks(t, RunTurn(context.Background(), prov, exec, store, threadID, "", TurnConfig{
		Model:     "test-model",
		UserParts: []message.Part{message.TextPart{Text: "run"}},
	}))

	// Should see a tool output error chunk.
	var hasToolError bool
	for _, c := range chunks {
		if v, ok := c.(message.ToolOutputErrorChunk); ok {
			if v.ToolCallID == "tc1" {
				hasToolError = true
			}
		}
	}
	if !hasToolError {
		t.Error("expected tool output error chunk for tc1")
	}
}

func TestResumeTurn_CrashedDuringAsyncWait(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"
	turnID := "turn1"

	// Save user message.
	if err := store.SaveMessage(threadID, StoredMessage{
		ID: "msg-user", ParentID: "",
		Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "run tasks"}}},
	}); err != nil {
		t.Fatal(err)
	}

	// Create step file to ensure directory exists.
	sf, _ := store.CreateStepFile(threadID, turnID, 0)
	sf.Close()

	// Step result: two async tool calls.
	assistantMsg := message.Message{
		Role: "assistant",
		Parts: []message.Part{
			message.ToolCallPart{ToolCallID: "tc1", ToolName: "launch_task", Input: `{}`},
			message.ToolCallPart{ToolCallID: "tc2", ToolName: "launch_task", Input: `{}`},
		},
	}
	if err := store.SaveStepResult(threadID, turnID, 0, StepResult{
		AssistantMessage: assistantMsg,
		ToolCalls: []ToolCallInfo{
			{ToolCallID: "tc1", ToolName: "launch_task", Input: "{}"},
			{ToolCallID: "tc2", ToolName: "launch_task", Input: "{}"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// tc1 completed before crash — saved in tool results.
	if err := store.SaveToolResults(threadID, turnID, 0, StepToolResults{
		Results: []message.ToolResultPart{
			{ToolCallID: "tc1", ToolName: "launch_task", Output: message.TextOutput{Value: "result-1"}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Both were async tasks — saved in async.json.
	if err := store.SaveAsyncContinuations(threadID, turnID, 0, StepAsyncContinuations{
		Continuations: []AsyncContinuationInfo{
			{ToolCallID: "tc1", ToolName: "launch_task", Continuation: testContinuation("task-1"), Input: "{}"},
			{ToolCallID: "tc2", ToolName: "launch_task", Continuation: testContinuation("task-2"), Input: "{}"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Turn state: crashed during waiting_for_async.
	turnState := &TurnState{
		ID:          turnID,
		ThreadID:    threadID,
		LeafID:      "",
		Config:      TurnConfig{Model: "test-model"},
		CurrentStep: 0,
		Phase:       PhaseWaitingForAsync,
		LeafMsgID:   "msg-user",
	}
	if err := store.SaveTurnState(threadID, *turnState); err != nil {
		t.Fatal(err)
	}

	// Provider: step 1 final response.
	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "All tasks done"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	// Executor: tc1 already completed (won't be called), tc2 resumes with a result.
	exec := &asyncMockExecutor{
		resumeResults: map[string]ToolExecuteResult{
			// task-1 won't be resumed because tc1 is already in completedTools.
			"task-2": {Result: message.ToolResultPart{
				ToolCallID: "tc2", ToolName: "launch_task",
				Output: message.TextOutput{Value: "result-2"},
			}},
		},
	}

	chunks := collectChunks(t, ResumeTurn(context.Background(), prov, exec, store, turnState))

	// Should see the final text from the next LLM step.
	var hasFinalText bool
	for _, c := range chunks {
		if v, ok := c.(message.TextDeltaChunk); ok && v.Delta == "All tasks done" {
			hasFinalText = true
		}
	}
	if !hasFinalText {
		t.Error("expected final text after async recovery")
	}

	// Turn state should be cleaned up.
	ts, _ := store.LoadTurnState(threadID)
	if ts != nil {
		t.Error("expected turn state to be deleted after completion")
	}
}

func TestResumeTurn_CrashedDuringAsyncWait_TaskLost(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"
	turnID := "turn1"

	// Save user message.
	if err := store.SaveMessage(threadID, StoredMessage{
		ID: "msg-user", ParentID: "",
		Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "run"}}},
	}); err != nil {
		t.Fatal(err)
	}

	// Create step file to ensure directory exists.
	sf, _ := store.CreateStepFile(threadID, turnID, 0)
	sf.Close()

	// Step result: one async tool call.
	assistantMsg := message.Message{
		Role: "assistant",
		Parts: []message.Part{
			message.ToolCallPart{ToolCallID: "tc1", ToolName: "launch_task", Input: `{}`},
		},
	}
	if err := store.SaveStepResult(threadID, turnID, 0, StepResult{
		AssistantMessage: assistantMsg,
		ToolCalls:        []ToolCallInfo{{ToolCallID: "tc1", ToolName: "launch_task", Input: "{}"}},
	}); err != nil {
		t.Fatal(err)
	}

	// Async tasks file.
	if err := store.SaveAsyncContinuations(threadID, turnID, 0, StepAsyncContinuations{
		Continuations: []AsyncContinuationInfo{{ToolCallID: "tc1", ToolName: "launch_task", Continuation: testContinuation("task-lost"), Input: "{}"}},
	}); err != nil {
		t.Fatal(err)
	}

	turnState := &TurnState{
		ID: turnID, ThreadID: threadID, LeafID: "",
		Config: TurnConfig{Model: "test-model"}, CurrentStep: 0,
		Phase: PhaseWaitingForAsync, LeafMsgID: "msg-user",
	}
	if err := store.SaveTurnState(threadID, *turnState); err != nil {
		t.Fatal(err)
	}

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "Handled"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	// ResumeAsync returns an error — task is gone.
	exec := &asyncMockExecutor{
		resumeErrors: map[string]error{"task-lost": fmt.Errorf("task expired")},
	}

	chunks := collectChunks(t, ResumeTurn(context.Background(), prov, exec, store, turnState))

	// Should still proceed — the error result is passed to the LLM.
	var hasFinalText bool
	for _, c := range chunks {
		if v, ok := c.(message.TextDeltaChunk); ok && v.Delta == "Handled" {
			hasFinalText = true
		}
	}
	if !hasFinalText {
		t.Error("expected final text after task-lost recovery")
	}

	// Verify the LLM received an error tool result.
	if len(prov.requests) < 1 {
		t.Fatal("expected at least 1 LLM call")
	}
	msgs := prov.requests[0].Messages
	toolMsg := msgs[len(msgs)-1]
	if toolMsg.Role != "tool" {
		t.Fatalf("expected tool message, got %s", toolMsg.Role)
	}
	tr := toolMsg.Parts[0].(message.ToolResultPart)
	if _, ok := tr.Output.(message.ErrorTextOutput); !ok {
		t.Error("expected ErrorTextOutput for lost task")
	}
}

func TestResumeTurn_CrashedDuringToolPhase_WithAsyncTasks(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"
	turnID := "turn1"

	// Save user message.
	if err := store.SaveMessage(threadID, StoredMessage{
		ID: "msg-user", ParentID: "",
		Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "run"}}},
	}); err != nil {
		t.Fatal(err)
	}

	// Create step file to ensure directory exists.
	sf, _ := store.CreateStepFile(threadID, turnID, 0)
	sf.Close()

	// Step result: one sync (completed) + one async.
	assistantMsg := message.Message{
		Role: "assistant",
		Parts: []message.Part{
			message.ToolCallPart{ToolCallID: "tc1", ToolName: "read_file", Input: `{}`},
			message.ToolCallPart{ToolCallID: "tc2", ToolName: "launch_task", Input: `{}`},
		},
	}
	if err := store.SaveStepResult(threadID, turnID, 0, StepResult{
		AssistantMessage: assistantMsg,
		ToolCalls: []ToolCallInfo{
			{ToolCallID: "tc1", ToolName: "read_file", Input: "{}"},
			{ToolCallID: "tc2", ToolName: "launch_task", Input: "{}"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// tc1 completed before crash.
	if err := store.SaveToolResults(threadID, turnID, 0, StepToolResults{
		Results: []message.ToolResultPart{
			{ToolCallID: "tc1", ToolName: "read_file", Output: message.TextOutput{Value: "file data"}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// tc2 was launched as async before crash.
	if err := store.SaveAsyncContinuations(threadID, turnID, 0, StepAsyncContinuations{
		Continuations: []AsyncContinuationInfo{
			{ToolCallID: "tc2", ToolName: "launch_task", Continuation: testContinuation("task-bg"), Input: "{}"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Phase is still "tools" — crash happened during the tool loop, before entering wait phase.
	turnState := &TurnState{
		ID: turnID, ThreadID: threadID, LeafID: "",
		Config: TurnConfig{Model: "test-model"}, CurrentStep: 0,
		Phase: PhaseTools, LeafMsgID: "msg-user",
	}
	if err := store.SaveTurnState(threadID, *turnState); err != nil {
		t.Fatal(err)
	}

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "Complete"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	// ResumeAsync: tc2's task still running, returns Wait handle.
	exec := &asyncMockExecutor{
		resumeResults: map[string]ToolExecuteResult{
			"task-bg": {Async: &AsyncContinuationHandle{
				Continuation: testContinuation("task-bg"),
				Wait: func(_ context.Context) (AsyncWaitResult, error) {
					return AsyncWaitResult{Result: message.ToolResultPart{
						ToolCallID: "tc2", ToolName: "launch_task",
						Output: message.TextOutput{Value: "task result"},
					}}, nil
				},
			}},
		},
	}

	chunks := collectChunks(t, ResumeTurn(context.Background(), prov, exec, store, turnState))

	var hasFinalText bool
	for _, c := range chunks {
		if v, ok := c.(message.TextDeltaChunk); ok && v.Delta == "Complete" {
			hasFinalText = true
		}
	}
	if !hasFinalText {
		t.Error("expected final text after recovery from tools phase with async")
	}

	// Verify both tool results reached the LLM.
	if len(prov.requests) < 1 {
		t.Fatal("expected at least 1 LLM call")
	}
	msgs := prov.requests[0].Messages
	results := toolResultParts(msgs)
	if len(results) != 2 {
		t.Fatalf("expected 2 tool results, got %d", len(results))
	}
	// tc1 first, tc2 second (original order).
	tr1 := results[0]
	tr2 := results[1]
	if tr1.ToolCallID != "tc1" || tr2.ToolCallID != "tc2" {
		t.Errorf("expected tc1,tc2 order, got %s,%s", tr1.ToolCallID, tr2.ToolCallID)
	}
}

func TestRunTurn_AsyncToolApprovalBubblesToParentAndResumes(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread-async-approval"

	questionsJSON := json.RawMessage(`[{"question":"Proceed with the sub-agent plan?"}]`)

	initialProvider := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.ToolCallChunk{ToolCallID: "tc1", ToolName: "Task", Input: `{"prompt":"delegate"}`},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "tool-calls"}},
			},
		},
	}
	initialExec := &asyncMockExecutor{
		asyncToolIDs:  map[string]string{"tc1": "task-subagent"},
		waitApprovals: map[string]json.RawMessage{"tc1": questionsJSON},
	}

	chunks := collectChunks(t, RunTurn(context.Background(), initialProvider, initialExec, store, threadID, "", TurnConfig{
		Model:     "test-model",
		UserParts: []message.Part{message.TextPart{Text: "delegate this"}},
	}))

	var hasApprovalChunk bool
	for _, c := range chunks {
		if approval, ok := c.(message.ToolApprovalRequestChunk); ok && approval.ToolCallID == "tc1" {
			hasApprovalChunk = true
		}
	}
	if !hasApprovalChunk {
		t.Fatal("expected approval chunk for async task")
	}

	turnState, err := store.LoadTurnState(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if turnState == nil || turnState.Phase != PhaseWaitingForAnswer {
		t.Fatalf("expected waiting_for_answer turn state, got %#v", turnState)
	}

	question, err := store.LoadQuestion(threadID, turnState.ID, turnState.PendingApprovalID)
	if err != nil {
		t.Fatal(err)
	}
	if question == nil {
		t.Fatal("expected pending question to be saved")
	}
	if got := testContinuationID(t, question.Continuation); got != "task-subagent" {
		t.Fatalf("expected pending question continuation taskID=task-subagent, got %q", got)
	}
	if question.ResumePhase != PhaseWaitingForAsync {
		t.Fatalf("expected pending question resumePhase=%s, got %s", PhaseWaitingForAsync, question.ResumePhase)
	}

	if err := store.SaveAnswer(threadID, turnState.ID, QuestionAnswer{
		ApprovalID: turnState.PendingApprovalID,
		Answers:    map[string]string{"Proceed with the sub-agent plan?": "Yes"},
	}); err != nil {
		t.Fatal(err)
	}

	resumeProvider := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "Delegation complete"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}
	resumeExec := &asyncMockExecutor{
		resumeResults: map[string]ToolExecuteResult{
			"task-subagent": {
				Result: message.ToolResultPart{
					ToolCallID: "tc1",
					ToolName:   "Task",
					Output:     message.TextOutput{Value: "sub-agent finished"},
				},
			},
		},
	}

	resumedChunks := collectChunks(t, ResumeTurn(context.Background(), resumeProvider, resumeExec, store, turnState))

	var hasToolOutput, hasFinalText bool
	for _, c := range resumedChunks {
		switch chunk := c.(type) {
		case message.ToolOutputAvailableChunk:
			if chunk.ToolCallID == "tc1" {
				hasToolOutput = true
			}
		case message.TextDeltaChunk:
			if chunk.Delta == "Delegation complete" {
				hasFinalText = true
			}
		}
	}
	if !hasToolOutput {
		t.Fatal("expected tool output after answering async approval")
	}
	if !hasFinalText {
		t.Fatal("expected final assistant text after async approval resume")
	}

	req, ok := resumeExec.resumeRequests["task-subagent"]
	if !ok {
		t.Fatal("expected ResumeAsync to receive the saved answer")
	}
	if req.Answers["Proceed with the sub-agent plan?"] != "Yes" {
		t.Fatalf("unexpected resume answers: %#v", req.Answers)
	}
}

// --- Approval Mock Executor ---

// approvalMockExecutor returns Approval for specific tool calls,
// sync results for others, and resolves approvals via ResolveApproval.
type approvalMockExecutor struct {
	// approvalTools maps toolCallID → Questions JSON for tools that need approval.
	approvalTools map[string]json.RawMessage
	// resolvedResults maps toolCallID → resolved result (returned by ResolveApproval).
	resolvedResults map[string]message.ToolResultPart
	// resolveAsyncSubThreadID causes ResolveAnswer to continue asynchronously.
	resolveAsyncSubThreadID string
	// resolveAsyncResult is returned when the async answer continuation completes.
	resolveAsyncResult message.ToolResultPart
	// syncResults maps toolCallID → sync result for non-approval tools.
	syncResults map[string]message.ToolResultPart
	// asyncToolIDs maps toolCallID → taskID for tools that should run async.
	asyncToolIDs map[string]string
	// asyncResults maps toolCallID → result for async Wait.
	asyncResults map[string]message.ToolResultPart
}

func (e *approvalMockExecutor) Execute(_ context.Context, _ *ToolContext, call message.ToolCallPart) (ToolExecuteResult, error) {
	if questions, ok := e.approvalTools[call.ToolCallID]; ok {
		return ToolExecuteResult{
			Approval: &ApprovalRequest{Questions: questions},
		}, nil
	}
	if taskID, ok := e.asyncToolIDs[call.ToolCallID]; ok {
		result := e.asyncResults[call.ToolCallID]
		continuation := testContinuation(taskID)
		return ToolExecuteResult{
			Async: &AsyncContinuationHandle{
				Continuation: continuation,
				Wait: func(_ context.Context) (AsyncWaitResult, error) {
					return AsyncWaitResult{Result: result}, nil
				},
			},
		}, nil
	}
	if result, ok := e.syncResults[call.ToolCallID]; ok {
		return ToolExecuteResult{Result: result}, nil
	}
	return ToolExecuteResult{Result: message.ToolResultPart{
		ToolCallID: call.ToolCallID,
		ToolName:   call.ToolName,
		Output:     message.TextOutput{Value: "default"},
	}}, nil
}

func (e *approvalMockExecutor) ResolveAnswer(_ *ToolContext, call message.ToolCallPart, _ api.AnswerQuestionRequest) (ToolExecuteResult, error) {
	if e.resolveAsyncSubThreadID != "" {
		continuation := testContinuation(e.resolveAsyncSubThreadID)
		return ToolExecuteResult{
			Async: &AsyncContinuationHandle{
				Continuation: continuation,
				Wait: func(_ context.Context) (AsyncWaitResult, error) {
					return AsyncWaitResult{Result: e.resolveAsyncResult}, nil
				},
			},
		}, nil
	}
	if result, ok := e.resolvedResults[call.ToolCallID]; ok {
		return ToolExecuteResult{Result: result}, nil
	}
	return ToolExecuteResult{}, fmt.Errorf("no resolved result for %s", call.ToolCallID)
}

func (e *approvalMockExecutor) ResumeAsync(_ context.Context, _ *ToolContext, _ message.ToolCallPart, _ string, _ *api.AnswerQuestionRequest) (ToolExecuteResult, error) {
	return ToolExecuteResult{}, fmt.Errorf("no async resume in approval mock")
}

func (e *approvalMockExecutor) Continue(ctx context.Context, toolCtx *ToolContext, call message.ToolCallPart, continuation json.RawMessage, req *api.AnswerQuestionRequest) (ToolExecuteResult, error) {
	if req != nil {
		return e.ResolveAnswer(toolCtx, call, *req)
	}
	if len(continuation) > 0 {
		var decoded struct {
			SubThreadID string `json:"subThreadId"`
		}
		if err := json.Unmarshal(continuation, &decoded); err != nil {
			return ToolExecuteResult{}, err
		}
		return e.ResumeAsync(ctx, toolCtx, call, decoded.SubThreadID, nil)
	}
	return ToolExecuteResult{}, fmt.Errorf("no continuation in approval mock")
}

func (e *approvalMockExecutor) SetPlanMode(_ bool)   {}
func (e *approvalMockExecutor) SetThreadID(_ string) {}

type nestedApprovalContinuation struct {
	LocalApprovalID string `json:"localApprovalId"`
	Stage           int    `json:"stage"`
}

type nestedApprovalExecutor struct {
	resolvedLocalApprovalIDs []string
}

func encodeNestedApprovalContinuation(id string, stage int) json.RawMessage {
	data, _ := json.Marshal(nestedApprovalContinuation{
		LocalApprovalID: id,
		Stage:           stage,
	})
	return data
}

func decodeNestedApprovalContinuation(t *testing.T, data json.RawMessage) nestedApprovalContinuation {
	t.Helper()
	var decoded nestedApprovalContinuation
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode nested approval continuation: %v", err)
	}
	return decoded
}

func (e *nestedApprovalExecutor) Execute(_ context.Context, _ *ToolContext, _ message.ToolCallPart) (ToolExecuteResult, error) {
	return ToolExecuteResult{
		Approval: &ApprovalRequest{
			Questions:    json.RawMessage(`[{"question":"Parent approval?"}]`),
			Continuation: encodeNestedApprovalContinuation("child-local-B", 1),
		},
	}, nil
}

func (e *nestedApprovalExecutor) Continue(_ context.Context, _ *ToolContext, call message.ToolCallPart, continuation json.RawMessage, req *api.AnswerQuestionRequest) (ToolExecuteResult, error) {
	decoded := nestedApprovalContinuation{}
	if err := json.Unmarshal(continuation, &decoded); err != nil {
		return ToolExecuteResult{}, err
	}
	e.resolvedLocalApprovalIDs = append(e.resolvedLocalApprovalIDs, decoded.LocalApprovalID)
	if req == nil {
		return ToolExecuteResult{}, fmt.Errorf("missing answer for %s", decoded.LocalApprovalID)
	}
	switch decoded.Stage {
	case 1:
		return ToolExecuteResult{
			Approval: &ApprovalRequest{
				Questions:    json.RawMessage(`[{"question":"Leaf approval?"}]`),
				Continuation: encodeNestedApprovalContinuation("leaf-local-A", 2),
			},
		}, nil
	case 2:
		return ToolExecuteResult{
			Result: message.ToolResultPart{
				ToolCallID: call.ToolCallID,
				ToolName:   call.ToolName,
				Output:     message.TextOutput{Value: "nested approvals resolved"},
			},
		}, nil
	default:
		return ToolExecuteResult{}, fmt.Errorf("unknown continuation stage %d", decoded.Stage)
	}
}

func (e *nestedApprovalExecutor) SetPlanMode(_ bool)   {}
func (e *nestedApprovalExecutor) SetThreadID(_ string) {}

// --- Approval Flow Tests ---

// TestRunTurn_ApprovalPause verifies that when a tool returns an ApprovalRequest,
// the turn pauses with PhaseWaitingForAnswer, the question is persisted,
// and a ToolApprovalRequestChunk is yielded.
func TestRunTurn_ApprovalPause(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			// Step 0: model calls a tool that needs approval
			{
				message.StreamStartChunk{},
				message.ToolCallChunk{ToolCallID: "tc1", ToolName: "dangerous_tool", Input: `{"action":"delete"}`},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "tool-calls"}},
			},
		},
	}

	questionsJSON := json.RawMessage(`[{"question":"Are you sure?"}]`)
	exec := &approvalMockExecutor{
		approvalTools: map[string]json.RawMessage{
			"tc1": questionsJSON,
		},
	}

	chunks := collectChunks(t, RunTurn(
		context.Background(), prov, exec, store,
		threadID, "", TurnConfig{
			Model:     "test-model",
			UserParts: []message.Part{message.TextPart{Text: "delete it"}},
		},
	))

	// Should yield a ToolApprovalRequestChunk.
	var hasApprovalChunk bool
	for _, c := range chunks {
		if ac, ok := c.(message.ToolApprovalRequestChunk); ok {
			hasApprovalChunk = true
			if ac.ToolCallID != "tc1" {
				t.Errorf("expected approval chunk for tc1, got %s", ac.ToolCallID)
			}
		}
	}
	if !hasApprovalChunk {
		t.Error("missing ToolApprovalRequestChunk")
	}

	// Turn state should still exist with PhaseWaitingForAnswer.
	state, err := store.LoadTurnState(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if state == nil {
		t.Fatal("expected turn state to persist (turn is paused)")
	}
	if state.Phase != PhaseWaitingForAnswer {
		t.Errorf("expected phase=%s, got %s", PhaseWaitingForAnswer, state.Phase)
	}

	// Question should be persisted.
	question, err := store.LoadQuestion(threadID, state.ID, state.PendingApprovalID)
	if err != nil {
		t.Fatal(err)
	}
	if question == nil {
		t.Fatal("expected question to be persisted")
	}
	if question.ToolCallID != "tc1" {
		t.Errorf("expected question toolCallId=tc1, got %s", question.ToolCallID)
	}
	if question.ResumePhase != PhaseTools {
		t.Errorf("expected question resumePhase=%s, got %s", PhaseTools, question.ResumePhase)
	}

	// Approval request should be persisted as a separate tool message.
	leaf, err := store.FindLeaf(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if leaf == "" {
		t.Fatal("expected leaf message")
	}
	leafMsg, err := store.LoadMessage(threadID, leaf)
	if err != nil {
		t.Fatal(err)
	}
	if leafMsg.Message.Role != "tool" {
		t.Errorf("expected leaf message role=tool, got %s", leafMsg.Message.Role)
	}
	// Check for approval part in tool message.
	hasApprovalPart := false
	for _, p := range leafMsg.Message.Parts {
		if _, ok := p.(message.ToolApprovalRequest); ok {
			hasApprovalPart = true
		}
	}
	if !hasApprovalPart {
		t.Error("expected assistant message to contain ToolApprovalRequest part")
	}
}

func TestResumeTurn_NestedApprovalIDsAreRemappedPerBoundary(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread-nested-approval"

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.ToolCallChunk{ToolCallID: "tc1", ToolName: "Task", Input: `{"prompt":"delegate"}`},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "tool-calls"}},
			},
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "complete"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	exec := &nestedApprovalExecutor{}
	collectChunks(t, RunTurn(
		context.Background(), prov, exec, store,
		threadID, "", TurnConfig{
			Model:     "test-model",
			UserParts: []message.Part{message.TextPart{Text: "run nested approvals"}},
		},
	))

	firstState, err := store.LoadTurnState(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if firstState == nil || firstState.Phase != PhaseWaitingForAnswer {
		t.Fatalf("expected first waiting_for_answer state, got %#v", firstState)
	}
	firstApprovalID := firstState.PendingApprovalID
	firstQuestion, err := store.LoadQuestion(threadID, firstState.ID, firstApprovalID)
	if err != nil {
		t.Fatal(err)
	}
	if firstQuestion == nil {
		t.Fatal("expected first pending question")
	}
	firstContinuation := decodeNestedApprovalContinuation(t, firstQuestion.Continuation)
	if firstContinuation.LocalApprovalID != "child-local-B" {
		t.Fatalf("expected first local approval ID child-local-B, got %q", firstContinuation.LocalApprovalID)
	}
	if firstApprovalID == firstContinuation.LocalApprovalID {
		t.Fatalf("turn approval ID should differ from local approval ID, both were %q", firstApprovalID)
	}

	if err := store.SaveAnswer(threadID, firstState.ID, QuestionAnswer{
		ApprovalID: firstApprovalID,
		Answers:    map[string]string{"Parent approval?": "Yes"},
	}); err != nil {
		t.Fatal(err)
	}

	collectChunks(t, ResumeTurn(context.Background(), prov, exec, store, firstState))

	secondState, err := store.LoadTurnState(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if secondState == nil || secondState.Phase != PhaseWaitingForAnswer {
		t.Fatalf("expected second waiting_for_answer state, got %#v", secondState)
	}
	secondApprovalID := secondState.PendingApprovalID
	if secondApprovalID == firstApprovalID {
		t.Fatalf("expected remapped approval ID for nested boundary, both were %q", secondApprovalID)
	}
	secondQuestion, err := store.LoadQuestion(threadID, secondState.ID, secondApprovalID)
	if err != nil {
		t.Fatal(err)
	}
	if secondQuestion == nil {
		t.Fatal("expected second pending question")
	}
	secondContinuation := decodeNestedApprovalContinuation(t, secondQuestion.Continuation)
	if secondContinuation.LocalApprovalID != "leaf-local-A" {
		t.Fatalf("expected second local approval ID leaf-local-A, got %q", secondContinuation.LocalApprovalID)
	}
	if secondApprovalID == secondContinuation.LocalApprovalID {
		t.Fatalf("turn approval ID should differ from nested local approval ID, both were %q", secondApprovalID)
	}

	if len(exec.resolvedLocalApprovalIDs) != 1 || exec.resolvedLocalApprovalIDs[0] != "child-local-B" {
		t.Fatalf("expected first continuation to resolve child-local-B, got %#v", exec.resolvedLocalApprovalIDs)
	}

	if err := store.SaveAnswer(threadID, secondState.ID, QuestionAnswer{
		ApprovalID: secondApprovalID,
		Answers:    map[string]string{"Leaf approval?": "Yes"},
	}); err != nil {
		t.Fatal(err)
	}

	chunks := collectChunks(t, ResumeTurn(context.Background(), prov, exec, store, secondState))

	if len(exec.resolvedLocalApprovalIDs) != 2 {
		t.Fatalf("expected two resolved local approvals, got %#v", exec.resolvedLocalApprovalIDs)
	}
	if exec.resolvedLocalApprovalIDs[0] != "child-local-B" || exec.resolvedLocalApprovalIDs[1] != "leaf-local-A" {
		t.Fatalf("unexpected local approval resolution order: %#v", exec.resolvedLocalApprovalIDs)
	}

	var hasFinalText bool
	for _, c := range chunks {
		if td, ok := c.(message.TextDeltaChunk); ok && td.Delta == "complete" {
			hasFinalText = true
		}
	}
	if !hasFinalText {
		t.Fatal("expected final text after nested approval remapping")
	}
}

// TestResumeTurn_ApprovalAnswered verifies that when an answer is available,
// ResumeTurn resolves the approval and continues the turn.
func TestResumeTurn_ApprovalAnswered(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"
	turnID := "turn1"

	// Save user message.
	if err := store.SaveMessage(threadID, StoredMessage{
		ID:      "msg-user",
		Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "delete it"}}},
	}); err != nil {
		t.Fatal(err)
	}

	// Save assistant message (with tool call + approval part).
	assistantMsgID := "msg-asst"
	if err := store.SaveMessage(threadID, StoredMessage{
		ID:       assistantMsgID,
		ParentID: "msg-user",
		Message: message.Message{
			Role: "assistant",
			Parts: []message.Part{
				message.ToolCallPart{ToolCallID: "tc1", ToolName: "dangerous_tool", Input: `{"action":"delete"}`},
				message.ToolApprovalRequest{ApprovalID: "tc1", ToolCallID: "tc1"},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Create step file and step result.
	sf, _ := store.CreateStepFile(threadID, turnID, 0)
	sf.Close()

	if err := store.SaveStepResult(threadID, turnID, 0, StepResult{
		AssistantMessage: message.Message{
			Role: "assistant",
			Parts: []message.Part{
				message.ToolCallPart{ToolCallID: "tc1", ToolName: "dangerous_tool", Input: `{"action":"delete"}`},
			},
		},
		ToolCalls: []ToolCallInfo{
			{ToolCallID: "tc1", ToolName: "dangerous_tool", Input: `{"action":"delete"}`},
		},
	}); err != nil {
		t.Fatal(err)
	}

	approvalID := "approval-1"
	// Save question.
	if err := store.SaveQuestion(threadID, turnID, PendingQuestionState{
		ApprovalID:  approvalID,
		ToolCallID:  "tc1",
		StepIndex:   0,
		ResumePhase: PhaseTools,
		Questions:   json.RawMessage(`[{"question":"Are you sure?"}]`),
	}); err != nil {
		t.Fatal(err)
	}

	// Save answer.
	if err := store.SaveAnswer(threadID, turnID, QuestionAnswer{
		ApprovalID: approvalID,
		Answers:    map[string]string{"q1": "yes"},
	}); err != nil {
		t.Fatal(err)
	}

	// Turn state: paused waiting for answer.
	turnState := &TurnState{
		ID:                turnID,
		ThreadID:          threadID,
		CurrentStep:       0,
		Phase:             PhaseWaitingForAnswer,
		LeafMsgID:         assistantMsgID,
		Config:            TurnConfig{Model: "test-model"},
		PendingApprovalID: approvalID,
	}
	if err := store.SaveTurnState(threadID, *turnState); err != nil {
		t.Fatal(err)
	}

	// Provider: step 1 after approval resolution.
	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "Deleted successfully"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	exec := &approvalMockExecutor{
		resolvedResults: map[string]message.ToolResultPart{
			"tc1": {
				ToolCallID: "tc1",
				ToolName:   "dangerous_tool",
				Output:     message.TextOutput{Value: "item deleted"},
			},
		},
	}

	chunks := collectChunks(t, ResumeTurn(context.Background(), prov, exec, store, turnState))

	// The resolved tool result must be yielded so consumers can observe the
	// approval outcome (e.g. CLI detecting ExitPlanMode approval).
	var hasToolOutput bool
	for _, c := range chunks {
		if v, ok := c.(message.ToolOutputAvailableChunk); ok && v.ToolCallID == "tc1" {
			hasToolOutput = true
		}
	}
	if !hasToolOutput {
		t.Error("expected ToolOutputAvailableChunk for resolved approval result")
	}

	// Should see final text.
	var hasFinalText bool
	for _, c := range chunks {
		if v, ok := c.(message.TextDeltaChunk); ok && v.Delta == "Deleted successfully" {
			hasFinalText = true
		}
	}
	if !hasFinalText {
		t.Error("expected final text after approval resolution")
	}

	// Turn state should be cleaned up.
	state, _ := store.LoadTurnState(threadID)
	if state != nil {
		t.Error("expected turn state to be deleted after completion")
	}

	// Question/answer files are preserved as historical data (not deleted after completion).

	// The LLM should have received the resolved tool result.
	if len(prov.requests) < 1 {
		t.Fatal("expected at least 1 LLM call")
	}
	msgs := prov.requests[0].Messages
	results := toolResultParts(msgs)
	if len(results) != 1 {
		t.Fatalf("expected 1 tool result, got %d", len(results))
	}
	tr := results[0]
	textOut, ok := tr.Output.(message.TextOutput)
	if !ok {
		t.Fatalf("expected TextOutput, got %T", tr.Output)
	}
	if textOut.Value != "item deleted" {
		t.Errorf("expected resolved output 'item deleted', got %q", textOut.Value)
	}
}

func TestResumeTurn_ApprovalAnswered_LiveContinuationSkipsReplayPreamble(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"
	turnID := "turn1"

	if err := store.SaveMessage(threadID, StoredMessage{
		ID:      "msg-user",
		Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "delete it"}}},
	}); err != nil {
		t.Fatal(err)
	}

	assistantMsgID := "msg-asst"
	if err := store.SaveMessage(threadID, StoredMessage{
		ID:       assistantMsgID,
		ParentID: "msg-user",
		Message: message.Message{
			Role: "assistant",
			Parts: []message.Part{
				message.ToolCallPart{ToolCallID: "tc1", ToolName: "dangerous_tool", Input: `{"action":"delete"}`},
				message.ToolApprovalRequest{ApprovalID: "tc1", ToolCallID: "tc1"},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	sf, _ := store.CreateStepFile(threadID, turnID, 0)
	sf.Close()

	if err := store.SaveStepResult(threadID, turnID, 0, StepResult{
		AssistantMessage: message.Message{
			Role: "assistant",
			Parts: []message.Part{
				message.ToolCallPart{ToolCallID: "tc1", ToolName: "dangerous_tool", Input: `{"action":"delete"}`},
			},
		},
		ToolCalls: []ToolCallInfo{
			{ToolCallID: "tc1", ToolName: "dangerous_tool", Input: `{"action":"delete"}`},
		},
	}); err != nil {
		t.Fatal(err)
	}

	approvalID := "approval-1"
	if err := store.SaveQuestion(threadID, turnID, PendingQuestionState{
		ApprovalID:  approvalID,
		ToolCallID:  "tc1",
		StepIndex:   0,
		ResumePhase: PhaseTools,
		Questions:   json.RawMessage(`[{"question":"Are you sure?"}]`),
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.SaveAnswer(threadID, turnID, QuestionAnswer{
		ApprovalID: approvalID,
		Answers:    map[string]string{"q1": "yes"},
	}); err != nil {
		t.Fatal(err)
	}

	turnState := &TurnState{
		ID:                turnID,
		ThreadID:          threadID,
		CurrentStep:       0,
		Phase:             PhaseWaitingForAnswer,
		LeafMsgID:         assistantMsgID,
		Config:            TurnConfig{Model: "test-model"},
		PendingApprovalID: approvalID,
	}
	if err := store.SaveTurnState(threadID, *turnState); err != nil {
		t.Fatal(err)
	}

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{{
			message.StreamStartChunk{},
			message.TextStartChunk{ID: "t1"},
			message.TextDeltaChunk{ID: "t1", Delta: "Deleted successfully"},
			message.TextEndChunk{ID: "t1"},
			message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
		}},
	}

	exec := &approvalMockExecutor{
		resolvedResults: map[string]message.ToolResultPart{
			"tc1": {
				ToolCallID: "tc1",
				ToolName:   "dangerous_tool",
				Output:     message.TextOutput{Value: "item deleted"},
			},
		},
	}

	chunks := collectChunks(t, ResumeTurn(
		context.Background(),
		prov,
		exec,
		store,
		turnState,
		&ToolContext{},
	))

	for _, c := range chunks {
		if _, ok := c.(message.UserMessageChunk); ok {
			t.Fatal("did not expect UserMessageChunk during live approval continuation")
		}
		if _, ok := c.(message.StartChunk); ok {
			t.Fatal("did not expect StartChunk during live approval continuation")
		}
		if _, ok := c.(message.ToolApprovalRequestChunk); ok {
			t.Fatal("did not expect ToolApprovalRequestChunk during live approval continuation")
		}
	}

	var hasToolOutput bool
	var hasFinalText bool
	for _, c := range chunks {
		if v, ok := c.(message.ToolOutputAvailableChunk); ok && v.ToolCallID == "tc1" {
			hasToolOutput = true
		}
		if v, ok := c.(message.TextDeltaChunk); ok && v.Delta == "Deleted successfully" {
			hasFinalText = true
		}
	}
	if !hasToolOutput {
		t.Error("expected ToolOutputAvailableChunk for resolved approval result")
	}
	if !hasFinalText {
		t.Error("expected final text after approval resolution")
	}
}

func TestResumeTurn_ApprovalAnswered_DoesNotReplayPreambleDuringRecovery(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"
	turnID := "turn1"

	userMsg := message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "delete it"}}}
	if err := store.SaveMessage(threadID, StoredMessage{
		ID:      "msg-user",
		Message: userMsg,
	}); err != nil {
		t.Fatal(err)
	}

	assistantMsgID := "msg-asst"
	if err := store.SaveMessage(threadID, StoredMessage{
		ID:       assistantMsgID,
		ParentID: "msg-user",
		Message: message.Message{
			Role: "assistant",
			Parts: []message.Part{
				message.ToolCallPart{ToolCallID: "tc1", ToolName: "dangerous_tool", Input: `{"action":"delete"}`},
				message.ToolApprovalRequest{ApprovalID: "tc1", ToolCallID: "tc1"},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	sf, _ := store.CreateStepFile(threadID, turnID, 0)
	sf.Close()

	if err := store.SaveStepResult(threadID, turnID, 0, StepResult{
		AssistantMessage: message.Message{
			Role: "assistant",
			Parts: []message.Part{
				message.ToolCallPart{ToolCallID: "tc1", ToolName: "dangerous_tool", Input: `{"action":"delete"}`},
			},
		},
		ToolCalls: []ToolCallInfo{
			{ToolCallID: "tc1", ToolName: "dangerous_tool", Input: `{"action":"delete"}`},
		},
	}); err != nil {
		t.Fatal(err)
	}

	approvalID := "approval-1"
	if err := store.SaveQuestion(threadID, turnID, PendingQuestionState{
		ApprovalID:  approvalID,
		ToolCallID:  "tc1",
		StepIndex:   0,
		ResumePhase: PhaseTools,
		Questions:   json.RawMessage(`[{"question":"Are you sure?"}]`),
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.SaveAnswer(threadID, turnID, QuestionAnswer{
		ApprovalID: approvalID,
		Answers:    map[string]string{"q1": "yes"},
	}); err != nil {
		t.Fatal(err)
	}

	turnState := &TurnState{
		ID:                turnID,
		ThreadID:          threadID,
		CurrentStep:       0,
		Phase:             PhaseWaitingForAnswer,
		LeafMsgID:         assistantMsgID,
		Config:            TurnConfig{Model: "test-model", UserMessage: userMsg},
		PendingApprovalID: approvalID,
	}
	if err := store.SaveTurnState(threadID, *turnState); err != nil {
		t.Fatal(err)
	}

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{{
			message.StreamStartChunk{},
			message.TextStartChunk{ID: "t1"},
			message.TextDeltaChunk{ID: "t1", Delta: "Deleted successfully"},
			message.TextEndChunk{ID: "t1"},
			message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
		}},
	}

	exec := &approvalMockExecutor{
		resolvedResults: map[string]message.ToolResultPart{
			"tc1": {
				ToolCallID: "tc1",
				ToolName:   "dangerous_tool",
				Output:     message.TextOutput{Value: "item deleted"},
			},
		},
	}

	chunks := collectChunks(t, ResumeTurn(
		context.Background(),
		prov,
		exec,
		store,
		turnState,
		&ToolContext{},
	))

	var hasUserMessage bool
	var hasStart bool
	var hasApprovalRequest bool
	for _, c := range chunks {
		switch c.(type) {
		case message.UserMessageChunk:
			hasUserMessage = true
		case message.StartChunk:
			hasStart = true
		case message.ToolApprovalRequestChunk:
			hasApprovalRequest = true
		}
	}
	if hasUserMessage {
		t.Fatal("did not expect replayed user message during recovery")
	}
	if hasStart {
		t.Fatal("did not expect replayed start chunk during recovery")
	}
	if hasApprovalRequest {
		t.Fatal("did not expect replayed approval request during recovery")
	}
}

// TestResumeTurn_ApprovalNoAnswer verifies that when no answer is available yet,
// ResumeTurn returns immediately (turn stays paused).
func TestResumeTurn_ApprovalNoAnswer(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"
	turnID := "turn1"

	// Save user message.
	if err := store.SaveMessage(threadID, StoredMessage{
		ID:      "msg-user",
		Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "delete"}}},
	}); err != nil {
		t.Fatal(err)
	}

	assistantMsgID := "msg-asst"
	if err := store.SaveMessage(threadID, StoredMessage{
		ID:       assistantMsgID,
		ParentID: "msg-user",
		Message: message.Message{
			Role: "assistant",
			Parts: []message.Part{
				message.ToolCallPart{ToolCallID: "tc1", ToolName: "dangerous_tool", Input: `{}`},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	sf, _ := store.CreateStepFile(threadID, turnID, 0)
	sf.Close()

	if err := store.SaveStepResult(threadID, turnID, 0, StepResult{
		AssistantMessage: message.Message{
			Role: "assistant",
			Parts: []message.Part{
				message.ToolCallPart{ToolCallID: "tc1", ToolName: "dangerous_tool", Input: `{}`},
			},
		},
		ToolCalls: []ToolCallInfo{
			{ToolCallID: "tc1", ToolName: "dangerous_tool", Input: `{}`},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Save question but NO answer.
	approvalID := "approval-1"
	if err := store.SaveQuestion(threadID, turnID, PendingQuestionState{
		ApprovalID:  approvalID,
		ToolCallID:  "tc1",
		StepIndex:   0,
		ResumePhase: PhaseTools,
		Questions:   json.RawMessage(`[{"question":"Are you sure?"}]`),
	}); err != nil {
		t.Fatal(err)
	}

	turnState := &TurnState{
		ID:                turnID,
		ThreadID:          threadID,
		CurrentStep:       0,
		Phase:             PhaseWaitingForAnswer,
		LeafMsgID:         assistantMsgID,
		Config:            TurnConfig{Model: "test-model"},
		PendingApprovalID: approvalID,
	}
	if err := store.SaveTurnState(threadID, *turnState); err != nil {
		t.Fatal(err)
	}

	// Provider should NOT be called.
	prov := &mockProvider{}

	exec := &approvalMockExecutor{}

	chunks := collectChunks(t, ResumeTurn(context.Background(), prov, exec, store, turnState))

	// Waiting-for-answer resumes stay silent on a live stream until an answer arrives.
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks while still waiting for answer, got %d", len(chunks))
	}

	// Provider should not have been called.
	if prov.callIndex != 0 {
		t.Errorf("expected 0 provider calls, got %d", prov.callIndex)
	}

	// Turn state should still exist.
	state, _ := store.LoadTurnState(threadID)
	if state == nil {
		t.Error("expected turn state to persist (still waiting for answer)")
	}
	if state != nil && state.Phase != PhaseWaitingForAnswer {
		t.Errorf("expected phase=%s, got %s", PhaseWaitingForAnswer, state.Phase)
	}
}

// TestRunTurn_ApprovalWithPriorSyncTools verifies that sync tools before
// the approval tool complete and have their results persisted, while the
// approval tool pauses the turn.
func TestRunTurn_ApprovalWithPriorSyncTools(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			// Step 0: sync tool tc1, then approval tool tc2
			{
				message.StreamStartChunk{},
				message.ToolCallChunk{ToolCallID: "tc1", ToolName: "read_file", Input: `{"path":"a.txt"}`},
				message.ToolCallChunk{ToolCallID: "tc2", ToolName: "dangerous_tool", Input: `{"action":"rm"}`},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "tool-calls"}},
			},
		},
	}

	exec := &approvalMockExecutor{
		approvalTools: map[string]json.RawMessage{
			"tc2": json.RawMessage(`[{"question":"Delete?"}]`),
		},
		syncResults: map[string]message.ToolResultPart{
			"tc1": {
				ToolCallID: "tc1",
				ToolName:   "read_file",
				Output:     message.TextOutput{Value: "file contents"},
			},
		},
	}

	chunks := collectChunks(t, RunTurn(
		context.Background(), prov, exec, store,
		threadID, "", TurnConfig{
			Model:     "test-model",
			UserParts: []message.Part{message.TextPart{Text: "read then delete"}},
		},
	))

	// Should yield both tool output for tc1 and approval chunk for tc2.
	var hasToolOutput, hasApproval bool
	for _, c := range chunks {
		switch v := c.(type) {
		case message.ToolOutputAvailableChunk:
			if v.ToolCallID == "tc1" {
				hasToolOutput = true
			}
		case message.ToolApprovalRequestChunk:
			if v.ToolCallID == "tc2" {
				hasApproval = true
			}
		}
	}
	if !hasToolOutput {
		t.Error("missing tool output for tc1 (sync tool before approval)")
	}
	if !hasApproval {
		t.Error("missing approval chunk for tc2")
	}

	// Turn state should be waiting for answer.
	state, err := store.LoadTurnState(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if state == nil {
		t.Fatal("expected turn state to persist")
	}
	if state.Phase != PhaseWaitingForAnswer {
		t.Errorf("expected phase=%s, got %s", PhaseWaitingForAnswer, state.Phase)
	}

	// Tool results should contain tc1's result.
	toolResults, err := store.LoadToolResults(threadID, state.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(toolResults.Results) != 1 {
		t.Fatalf("expected 1 tool result (tc1), got %d", len(toolResults.Results))
	}
	if toolResults.Results[0].ToolCallID != "tc1" {
		t.Errorf("expected tc1 in tool results, got %s", toolResults.Results[0].ToolCallID)
	}
}

// TestRunTurn_ApprovalWithPendingAsync verifies that async tools in-flight
// when an approval is hit are waited for before pausing.
func TestRunTurn_ApprovalWithPendingAsync(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			// Step 0: async tool tc1, approval tool tc2
			{
				message.StreamStartChunk{},
				message.ToolCallChunk{ToolCallID: "tc1", ToolName: "launch_task", Input: `{}`},
				message.ToolCallChunk{ToolCallID: "tc2", ToolName: "dangerous_tool", Input: `{"action":"rm"}`},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "tool-calls"}},
			},
		},
	}

	exec := &approvalMockExecutor{
		approvalTools: map[string]json.RawMessage{
			"tc2": json.RawMessage(`[{"question":"Delete?"}]`),
		},
		asyncToolIDs: map[string]string{"tc1": "task-bg"},
		asyncResults: map[string]message.ToolResultPart{
			"tc1": {ToolCallID: "tc1", ToolName: "launch_task", Output: message.TextOutput{Value: "async result"}},
		},
	}

	chunks := collectChunks(t, RunTurn(
		context.Background(), prov, exec, store,
		threadID, "", TurnConfig{
			Model:     "test-model",
			UserParts: []message.Part{message.TextPart{Text: "async then approve"}},
		},
	))

	// Should yield async tool output (tc1 waited before approval pause) and approval chunk.
	var hasAsyncOutput, hasApproval bool
	for _, c := range chunks {
		switch v := c.(type) {
		case message.ToolOutputAvailableChunk:
			if v.ToolCallID == "tc1" {
				hasAsyncOutput = true
			}
		case message.ToolApprovalRequestChunk:
			if v.ToolCallID == "tc2" {
				hasApproval = true
			}
		}
	}
	if !hasAsyncOutput {
		t.Error("expected async tool output for tc1 (waited before approval pause)")
	}
	if !hasApproval {
		t.Error("missing approval chunk for tc2")
	}

	// Turn state should be waiting for answer.
	state, err := store.LoadTurnState(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if state == nil {
		t.Fatal("expected turn state to persist")
	}
	if state.Phase != PhaseWaitingForAnswer {
		t.Errorf("expected phase=%s, got %s", PhaseWaitingForAnswer, state.Phase)
	}

	// Tool results should contain tc1's async result.
	toolResults, err := store.LoadToolResults(threadID, state.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(toolResults.Results) != 1 {
		t.Fatalf("expected 1 tool result (tc1 async), got %d", len(toolResults.Results))
	}
	if toolResults.Results[0].ToolCallID != "tc1" {
		t.Errorf("expected tc1 in tool results, got %s", toolResults.Results[0].ToolCallID)
	}
	textOut, ok := toolResults.Results[0].Output.(message.TextOutput)
	if !ok {
		t.Fatalf("expected TextOutput for async result, got %T", toolResults.Results[0].Output)
	}
	if textOut.Value != "async result" {
		t.Errorf("expected 'async result', got %q", textOut.Value)
	}
}

// --- P1: PhaseTools Async Re-Attach Variant Tests ---

// TestResumeTurn_CrashedDuringToolPhase_AsyncResumeError tests that when
// ResumeAsync returns an error during the tools phase, the error result
// is used and the turn continues.
func TestResumeTurn_CrashedDuringToolPhase_AsyncResumeError(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"
	turnID := "turn1"

	if err := store.SaveMessage(threadID, StoredMessage{
		ID:      "msg-user",
		Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "run"}}},
	}); err != nil {
		t.Fatal(err)
	}

	sf, _ := store.CreateStepFile(threadID, turnID, 0)
	sf.Close()

	// Step result: one async tool call.
	if err := store.SaveStepResult(threadID, turnID, 0, StepResult{
		AssistantMessage: message.Message{
			Role: "assistant",
			Parts: []message.Part{
				message.ToolCallPart{ToolCallID: "tc1", ToolName: "launch_task", Input: `{}`},
			},
		},
		ToolCalls: []ToolCallInfo{
			{ToolCallID: "tc1", ToolName: "launch_task", Input: "{}"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// tc1 was an async task that was launched before crash.
	if err := store.SaveAsyncContinuations(threadID, turnID, 0, StepAsyncContinuations{
		Continuations: []AsyncContinuationInfo{
			{ToolCallID: "tc1", ToolName: "launch_task", Continuation: testContinuation("task-gone"), Input: "{}"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Phase is tools (crash before entering async wait).
	turnState := &TurnState{
		ID: turnID, ThreadID: threadID,
		CurrentStep: 0, Phase: PhaseTools,
		LeafMsgID: "msg-user",
		Config:    TurnConfig{Model: "test-model"},
	}
	if err := store.SaveTurnState(threadID, *turnState); err != nil {
		t.Fatal(err)
	}

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "handled error"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	// ResumeAsync fails.
	exec := &asyncMockExecutor{
		resumeErrors: map[string]error{"task-gone": fmt.Errorf("container gone")},
	}

	chunks := collectChunks(t, ResumeTurn(context.Background(), prov, exec, store, turnState))

	// Should still proceed with an error result.
	var hasFinalText bool
	for _, c := range chunks {
		if v, ok := c.(message.TextDeltaChunk); ok && v.Delta == "handled error" {
			hasFinalText = true
		}
	}
	if !hasFinalText {
		t.Error("expected final text after async resume error in tools phase")
	}

	// Verify the LLM received an error tool result.
	if len(prov.requests) < 1 {
		t.Fatal("expected at least 1 LLM call")
	}
	msgs := prov.requests[0].Messages
	toolMsg := msgs[len(msgs)-1]
	if toolMsg.Role != "tool" {
		t.Fatalf("expected tool message, got %s", toolMsg.Role)
	}
	tr := toolMsg.Parts[0].(message.ToolResultPart)
	errOut, ok := tr.Output.(message.ErrorTextOutput)
	if !ok {
		t.Fatalf("expected ErrorTextOutput, got %T", tr.Output)
	}
	if errOut.Value != "async continuation lost: container gone" {
		t.Errorf("expected 'async continuation lost: container gone', got %q", errOut.Value)
	}

	state, _ := store.LoadTurnState(threadID)
	if state != nil {
		t.Error("expected turn state to be deleted")
	}
}

// TestResumeTurn_CrashedDuringToolPhase_AsyncImmediateResult tests that when
// ResumeAsync returns an immediate result (no Wait), the result is used directly.
func TestResumeTurn_CrashedDuringToolPhase_AsyncImmediateResult(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"
	turnID := "turn1"

	if err := store.SaveMessage(threadID, StoredMessage{
		ID:      "msg-user",
		Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "run"}}},
	}); err != nil {
		t.Fatal(err)
	}

	sf, _ := store.CreateStepFile(threadID, turnID, 0)
	sf.Close()

	if err := store.SaveStepResult(threadID, turnID, 0, StepResult{
		AssistantMessage: message.Message{
			Role: "assistant",
			Parts: []message.Part{
				message.ToolCallPart{ToolCallID: "tc1", ToolName: "launch_task", Input: `{}`},
			},
		},
		ToolCalls: []ToolCallInfo{
			{ToolCallID: "tc1", ToolName: "launch_task", Input: "{}"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.SaveAsyncContinuations(threadID, turnID, 0, StepAsyncContinuations{
		Continuations: []AsyncContinuationInfo{
			{ToolCallID: "tc1", ToolName: "launch_task", Continuation: testContinuation("task-done"), Input: "{}"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	turnState := &TurnState{
		ID: turnID, ThreadID: threadID,
		CurrentStep: 0, Phase: PhaseTools,
		LeafMsgID: "msg-user",
		Config:    TurnConfig{Model: "test-model"},
	}
	if err := store.SaveTurnState(threadID, *turnState); err != nil {
		t.Fatal(err)
	}

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "got it"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	// ResumeAsync returns an immediate result (task already completed).
	exec := &asyncMockExecutor{
		resumeResults: map[string]ToolExecuteResult{
			"task-done": {Result: message.ToolResultPart{
				ToolCallID: "tc1", ToolName: "launch_task",
				Output: message.TextOutput{Value: "completed while offline"},
			}},
		},
	}

	chunks := collectChunks(t, ResumeTurn(context.Background(), prov, exec, store, turnState))

	var hasFinalText bool
	for _, c := range chunks {
		if v, ok := c.(message.TextDeltaChunk); ok && v.Delta == "got it" {
			hasFinalText = true
		}
	}
	if !hasFinalText {
		t.Error("expected final text after async immediate result")
	}

	// Verify the LLM received the completed result.
	msgs := prov.requests[0].Messages
	toolMsg := msgs[len(msgs)-1]
	tr := toolMsg.Parts[0].(message.ToolResultPart)
	textOut, ok := tr.Output.(message.TextOutput)
	if !ok {
		t.Fatalf("expected TextOutput, got %T", tr.Output)
	}
	if textOut.Value != "completed while offline" {
		t.Errorf("expected 'completed while offline', got %q", textOut.Value)
	}

	state, _ := store.LoadTurnState(threadID)
	if state != nil {
		t.Error("expected turn state to be deleted")
	}
}

// --- P1: Provider-Supplied Message ID Test ---

// TestRunTurn_MessageIDFromStartChunk verifies that the assistant message is saved
// with the pre-generated ID emitted in StartChunk, not the provider-supplied ID.
// The provider's message ID is per-step (per LLM call) and not suitable as the
// turn-level message ID the UI tracks.
func TestRunTurn_MessageIDFromStartChunk(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.ResponseMetadataChunk{ID: "provider-msg-123"},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "Hello"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	chunks := collectChunks(t, RunTurn(
		context.Background(), prov, &mockExecutor{}, store,
		threadID, "", TurnConfig{
			Model:     "test-model",
			UserParts: []message.Part{message.TextPart{Text: "hi"}},
		},
	))

	// First chunk must be StartChunk with the pre-generated message ID.
	if len(chunks) == 0 {
		t.Fatal("expected chunks, got none")
	}
	if _, ok := chunks[0].(message.UserMessageChunk); !ok {
		t.Fatalf("expected UserMessageChunk at index 0, got %T", chunks[0])
	}
	startChunk, ok := chunks[1].(message.StartChunk)
	if !ok {
		t.Fatalf("expected StartChunk at index 1, got %T", chunks[1])
	}
	if startChunk.MessageID == "" {
		t.Error("StartChunk.MessageID must not be empty")
	}
	if startChunk.MessageID == "provider-msg-123" {
		t.Error("StartChunk.MessageID must be our pre-generated ID, not the provider's per-step ID")
	}

	// The leaf (assistant message) must be saved with the same ID as StartChunk.
	leaf, err := store.FindLeaf(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if leaf != startChunk.MessageID {
		t.Errorf("leaf message ID %q does not match StartChunk.MessageID %q", leaf, startChunk.MessageID)
	}

	// Verify the message exists and has correct content.
	msg, err := store.LoadMessage(threadID, startChunk.MessageID)
	if err != nil {
		t.Fatalf("failed to load assistant message: %v", err)
	}
	if msg.Message.Role != "assistant" {
		t.Errorf("expected role=assistant, got %s", msg.Message.Role)
	}
	if msg.Message.ID != startChunk.MessageID {
		t.Errorf("expected persisted assistant message ID %q, got %q", startChunk.MessageID, msg.Message.ID)
	}
	if msg.Message.ProviderResponseID != "provider-msg-123" {
		t.Errorf("expected provider response ID %q, got %q", "provider-msg-123", msg.Message.ProviderResponseID)
	}
}

func TestRunTurn_UserMessageChunkProjectsImagePartsToUIFiles(t *testing.T) {
	store := NewStore(t.TempDir())

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "ok"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	chunks := collectChunks(t, RunTurn(
		context.Background(), prov, &mockExecutor{}, store,
		"thread-image", "", TurnConfig{
			Model: "test-model",
			UserParts: []message.Part{
				message.TextPart{Text: "Does this rendering look correct?"},
				message.ImagePart{
					Image:     "data:image/png;base64,abc123",
					MediaType: "image/png",
				},
			},
		},
	))

	userChunk, ok := chunks[0].(message.UserMessageChunk)
	if !ok {
		t.Fatalf("expected UserMessageChunk at index 0, got %T", chunks[0])
	}
	if len(userChunk.Data.Message.Parts) != 2 {
		t.Fatalf("expected 2 projected user parts, got %d", len(userChunk.Data.Message.Parts))
	}

	textPart, ok := userChunk.Data.Message.Parts[0].(message.UITextPart)
	if !ok {
		t.Fatalf("expected first user part to be UITextPart, got %T", userChunk.Data.Message.Parts[0])
	}
	if textPart.Text != "Does this rendering look correct?" {
		t.Fatalf("unexpected text part: %+v", textPart)
	}

	filePart, ok := userChunk.Data.Message.Parts[1].(message.UIFilePart)
	if !ok {
		t.Fatalf("expected second user part to be UIFilePart, got %T", userChunk.Data.Message.Parts[1])
	}
	if filePart.MediaType != "image/png" {
		t.Fatalf("expected projected image media type image/png, got %q", filePart.MediaType)
	}
	if filePart.URL != "data:image/png;base64,abc123" {
		t.Fatalf("expected projected image URL to be preserved, got %q", filePart.URL)
	}
}

// --- P2: Async Interrupted Fallback + FindLeaf Store Tests ---

// TestResumeTurn_AsyncPhase_MissingToolResult verifies that when a tool call
// has no result in the async phase (not in completedTools, not in asyncTasks),
// it gets an "interrupted" fallback.
func TestResumeTurn_AsyncPhase_MissingToolResult(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"
	turnID := "turn1"

	if err := store.SaveMessage(threadID, StoredMessage{
		ID:      "msg-user",
		Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "run"}}},
	}); err != nil {
		t.Fatal(err)
	}

	sf, _ := store.CreateStepFile(threadID, turnID, 0)
	sf.Close()

	// Step result: two tools, but only one has an async task.
	if err := store.SaveStepResult(threadID, turnID, 0, StepResult{
		AssistantMessage: message.Message{
			Role: "assistant",
			Parts: []message.Part{
				message.ToolCallPart{ToolCallID: "tc1", ToolName: "launch_task", Input: `{}`},
				message.ToolCallPart{ToolCallID: "tc2", ToolName: "lost_tool", Input: `{}`},
			},
		},
		ToolCalls: []ToolCallInfo{
			{ToolCallID: "tc1", ToolName: "launch_task", Input: "{}"},
			{ToolCallID: "tc2", ToolName: "lost_tool", Input: "{}"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Only tc1 has an async task. tc2 has no result or async task (was never started).
	if err := store.SaveAsyncContinuations(threadID, turnID, 0, StepAsyncContinuations{
		Continuations: []AsyncContinuationInfo{
			{ToolCallID: "tc1", ToolName: "launch_task", Continuation: testContinuation("task-1"), Input: "{}"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	turnState := &TurnState{
		ID: turnID, ThreadID: threadID,
		CurrentStep: 0, Phase: PhaseWaitingForAsync,
		LeafMsgID: "msg-user",
		Config:    TurnConfig{Model: "test-model"},
	}
	if err := store.SaveTurnState(threadID, *turnState); err != nil {
		t.Fatal(err)
	}

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{
			{
				message.StreamStartChunk{},
				message.TextStartChunk{ID: "t1"},
				message.TextDeltaChunk{ID: "t1", Delta: "ok"},
				message.TextEndChunk{ID: "t1"},
				message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
			},
		},
	}

	exec := &asyncMockExecutor{
		resumeResults: map[string]ToolExecuteResult{
			"task-1": {Result: message.ToolResultPart{
				ToolCallID: "tc1", ToolName: "launch_task",
				Output: message.TextOutput{Value: "done"},
			}},
		},
	}

	collectChunks(t, ResumeTurn(context.Background(), prov, exec, store, turnState))

	// Verify the LLM received tc1 with result and tc2 with interrupted error.
	if len(prov.requests) < 1 {
		t.Fatal("expected at least 1 LLM call")
	}
	msgs := prov.requests[0].Messages
	results := toolResultParts(msgs)
	if len(results) != 2 {
		t.Fatalf("expected 2 tool results, got %d", len(results))
	}

	// tc1: successful result.
	tr1 := results[0]
	if _, isErr := tr1.Output.(message.ErrorTextOutput); isErr {
		t.Error("tc1: expected successful result, got error")
	}

	// tc2: interrupted fallback.
	tr2 := results[1]
	errOut, ok := tr2.Output.(message.ErrorTextOutput)
	if !ok {
		t.Fatalf("tc2: expected ErrorTextOutput, got %T", tr2.Output)
	}
	if errOut.Value != "interrupted by transient system failure" {
		t.Errorf("tc2: expected interrupted message, got %q", errOut.Value)
	}
}

// TestFindLeaf verifies the Store.FindLeaf method.
func TestFindLeaf(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread1"

	// Empty thread.
	leaf, err := store.FindLeaf(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if leaf != "" {
		t.Errorf("expected empty leaf for nonexistent thread, got %q", leaf)
	}

	// Single message (root is also the leaf).
	if err := store.SaveMessage(threadID, StoredMessage{
		ID:      "msg1",
		Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "hello"}}},
	}); err != nil {
		t.Fatal(err)
	}
	leaf, err = store.FindLeaf(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if leaf != "msg1" {
		t.Errorf("expected leaf=msg1, got %q", leaf)
	}

	// Chain: msg1 -> msg2 -> msg3. Leaf should be msg3.
	if err := store.SaveMessage(threadID, StoredMessage{
		ID:       "msg2",
		ParentID: "msg1",
		Message:  message.Message{Role: "assistant", Parts: []message.Part{message.TextPart{Text: "hi"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveMessage(threadID, StoredMessage{
		ID:       "msg3",
		ParentID: "msg2",
		Message:  message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "bye"}}},
	}); err != nil {
		t.Fatal(err)
	}

	leaf, err = store.FindLeaf(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if leaf != "msg3" {
		t.Errorf("expected leaf=msg3, got %q", leaf)
	}
}

func TestFormatRetryMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		event transport.RetryEvent
		want  string
	}{
		{
			name:  "rate limited",
			event: transport.RetryEvent{StatusCode: 429, Delay: 1250 * time.Millisecond, Attempt: 2, MaxRetries: 5},
			want:  "provider rate limited (HTTP 429); retrying in 1.3s (attempt 2/5)",
		},
		{
			name:  "http error",
			event: transport.RetryEvent{StatusCode: 503, Delay: 2 * time.Second, Attempt: 1, MaxRetries: 4},
			want:  "provider request failed (HTTP 503); retrying in 2s (attempt 1/4)",
		},
		{
			name:  "network error with negative delay",
			event: transport.RetryEvent{Err: fmt.Errorf("dial tcp timeout"), Delay: -1 * time.Second, Attempt: 3, MaxRetries: 6},
			want:  "provider request failed: dial tcp timeout; retrying in 0s (attempt 3/6)",
		},
		{
			name:  "generic failure",
			event: transport.RetryEvent{Delay: 450 * time.Millisecond, Attempt: 1, MaxRetries: 2},
			want:  "provider request failed; retrying in 500ms (attempt 1/2)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatRetryMessage(tt.event); got != tt.want {
				t.Fatalf("formatRetryMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEffectiveReasoning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  TurnConfig
		want providers.Reasoning
	}{
		{
			name: "disabled becomes none",
			cfg:  TurnConfig{Reasoning: providers.ReasoningDisabled},
			want: providers.ReasoningNone,
		},
		{
			name: "explicit level wins",
			cfg:  TurnConfig{Reasoning: providers.ReasoningHigh},
			want: providers.ReasoningHigh,
		},
		{
			name: "default unknown model falls back to none",
			cfg:  TurnConfig{ProviderID: "missing", Model: "missing", Reasoning: providers.ReasoningDefault},
			want: providers.ReasoningNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectiveReasoning(tt.cfg); got != tt.want {
				t.Fatalf("effectiveReasoning() = %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("metadata-backed default uses model capabilities", func(t *testing.T) {
		got := effectiveReasoning(TurnConfig{
			ProviderID: "openai",
			Model:      "o3",
			Reasoning:  providers.ReasoningDefault,
		})
		if got == providers.ReasoningNone {
			t.Fatalf("expected metadata-backed reasoning level for openai/o3, got %q", got)
		}
	})
}

func TestFilterContentParts(t *testing.T) {
	t.Parallel()

	providerExecuted := true
	parts := []message.Part{
		message.TextPart{Text: "visible"},
		message.ToolCallPart{ToolCallID: "tc1", ToolName: "dangerous", Input: `{}`},
		message.ReasoningPart{Text: "thinking"},
		message.ToolResultPart{ToolCallID: "tc1", ToolName: "dangerous", Output: message.TextOutput{Value: "done"}},
		message.ToolCallPart{ToolCallID: "tc2", ToolName: "provider", Input: `{}`, ProviderExecuted: &providerExecuted},
	}

	filtered := filterContentParts(parts)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 content parts, got %d", len(filtered))
	}
	if text, ok := filtered[0].(message.TextPart); !ok || text.Text != "visible" {
		t.Fatalf("expected first part to be text, got %#v", filtered[0])
	}
	if reasoning, ok := filtered[1].(message.ReasoningPart); !ok || reasoning.Text != "thinking" {
		t.Fatalf("expected second part to be reasoning, got %#v", filtered[1])
	}
}

func TestRecoverStreamingStep_IncompleteDropsToolCalls(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread-incomplete"
	turnID := "turn-incomplete"

	stepFile, err := store.CreateStepFile(threadID, turnID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, chunk := range []message.ProviderMessageChunk{
		message.StreamStartChunk{},
		message.TextStartChunk{ID: "text-1"},
		message.TextDeltaChunk{ID: "text-1", Delta: "partial"},
		message.TextEndChunk{ID: "text-1"},
		message.ToolCallChunk{ToolCallID: "tc1", ToolName: "dangerous_tool", Input: `{"path":"secret.txt"}`},
	} {
		if err := store.AppendChunk(stepFile, chunk); err != nil {
			t.Fatal(err)
		}
	}
	if err := stepFile.Close(); err != nil {
		t.Fatal(err)
	}

	turnState := &TurnState{ID: turnID, ThreadID: threadID, CurrentStep: 0, AssistantMsgID: "asst-1"}
	streamComplete, recovered := recoverStreamingStep(store, threadID, turnID, turnState)
	if streamComplete {
		t.Fatal("expected incomplete stream to report streamComplete=false")
	}
	if !recovered {
		t.Fatal("expected incomplete stream to be recovered")
	}

	stepResult, err := store.LoadStepResult(threadID, turnID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if stepResult == nil {
		t.Fatal("expected recovered step result")
	}
	if stepResult.AssistantMessage.ID != "asst-1" {
		t.Fatalf("expected recovered assistant message ID to use AssistantMsgID, got %q", stepResult.AssistantMessage.ID)
	}
	if len(stepResult.AssistantMessage.Parts) != 1 {
		t.Fatalf("expected incomplete recovery to keep only content parts, got %d parts", len(stepResult.AssistantMessage.Parts))
	}
	if text, ok := stepResult.AssistantMessage.Parts[0].(message.TextPart); !ok || text.Text != "partial" {
		t.Fatalf("expected recovered partial text, got %#v", stepResult.AssistantMessage.Parts[0])
	}
}

func TestRecoverStreamingStep_CompleteKeepsToolCalls(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread-complete"
	turnID := "turn-complete"

	stepFile, err := store.CreateStepFile(threadID, turnID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, chunk := range []message.ProviderMessageChunk{
		message.StreamStartChunk{},
		message.ToolCallChunk{ToolCallID: "tc1", ToolName: "dangerous_tool", Input: `{"path":"secret.txt"}`},
		message.FinishChunk{FinishReason: message.FinishReason{Unified: "tool-calls"}},
	} {
		if err := store.AppendChunk(stepFile, chunk); err != nil {
			t.Fatal(err)
		}
	}
	if err := stepFile.Close(); err != nil {
		t.Fatal(err)
	}

	turnState := &TurnState{ID: turnID, ThreadID: threadID, CurrentStep: 0, AssistantMsgID: "asst-2"}
	streamComplete, recovered := recoverStreamingStep(store, threadID, turnID, turnState)
	if !streamComplete {
		t.Fatal("expected completed stream to report streamComplete=true")
	}
	if !recovered {
		t.Fatal("expected completed stream to be recovered")
	}

	stepResult, err := store.LoadStepResult(threadID, turnID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if stepResult == nil {
		t.Fatal("expected recovered step result")
	}
	if len(stepResult.AssistantMessage.Parts) != 1 {
		t.Fatalf("expected recovered completed message to keep tool call, got %d parts", len(stepResult.AssistantMessage.Parts))
	}
	if toolCall, ok := stepResult.AssistantMessage.Parts[0].(message.ToolCallPart); !ok || toolCall.ToolCallID != "tc1" {
		t.Fatalf("expected recovered tool call, got %#v", stepResult.AssistantMessage.Parts[0])
	}
}

func TestResumeTurn_CrashedDuringAsyncWait_PausesForApproval(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread-async-approval-recovery"
	turnID := "turn-async-approval-recovery"

	if err := store.SaveMessage(threadID, StoredMessage{
		ID:      "msg-user",
		Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "run"}}},
	}); err != nil {
		t.Fatal(err)
	}

	stepFile, err := store.CreateStepFile(threadID, turnID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := stepFile.Close(); err != nil {
		t.Fatal(err)
	}

	if err := store.SaveStepResult(threadID, turnID, 0, StepResult{
		AssistantMessage: message.Message{
			Role: "assistant",
			Parts: []message.Part{
				message.ToolCallPart{ToolCallID: "tc1", ToolName: "launch_task", Input: `{}`},
			},
		},
		ToolCalls: []ToolCallInfo{
			{ToolCallID: "tc1", ToolName: "launch_task", Input: "{}"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.SaveAsyncContinuations(threadID, turnID, 0, StepAsyncContinuations{
		Continuations: []AsyncContinuationInfo{{ToolCallID: "tc1", ToolName: "launch_task", Continuation: testContinuation("task-approval"), Input: "{}"}},
	}); err != nil {
		t.Fatal(err)
	}

	turnState := &TurnState{
		ID:          turnID,
		ThreadID:    threadID,
		CurrentStep: 0,
		Phase:       PhaseWaitingForAsync,
		LeafMsgID:   "msg-user",
		Config:      TurnConfig{Model: "test-model"},
	}
	if err := store.SaveTurnState(threadID, *turnState); err != nil {
		t.Fatal(err)
	}

	exec := &asyncMockExecutor{
		resumeResults: map[string]ToolExecuteResult{
			"task-approval": {
				Approval: &ApprovalRequest{Questions: json.RawMessage(`[{"question":"continue?"}]`)},
			},
		},
	}

	chunks := collectChunks(t, ResumeTurn(context.Background(), &mockProvider{}, exec, store, turnState))

	var hasApprovalChunk bool
	for _, c := range chunks {
		if approval, ok := c.(message.ToolApprovalRequestChunk); ok && approval.ToolCallID == "tc1" {
			hasApprovalChunk = true
		}
	}
	if !hasApprovalChunk {
		t.Fatal("expected approval chunk after async recovery")
	}

	state, err := store.LoadTurnState(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if state == nil || state.Phase != PhaseWaitingForAnswer {
		t.Fatalf("expected paused waiting_for_answer state, got %#v", state)
	}
	question, err := store.LoadQuestion(threadID, turnID, state.PendingApprovalID)
	if err != nil {
		t.Fatal(err)
	}
	if question == nil {
		t.Fatalf("expected pending question for task-approval, got %#v", question)
	}
	if got := testContinuationID(t, question.Continuation); got != "task-approval" {
		t.Fatalf("expected pending question continuation taskID=task-approval, got %q", got)
	}
	if question.ResumePhase != PhaseWaitingForAsync {
		t.Fatalf("expected pending question resumePhase=%s, got %s", PhaseWaitingForAsync, question.ResumePhase)
	}
}

func TestResumeTurn_ApprovalAnswered_ResolveAnswerAsyncThenContinues(t *testing.T) {
	store := NewStore(t.TempDir())
	threadID := "thread-answer-async"
	turnID := "turn-answer-async"

	if err := store.SaveMessage(threadID, StoredMessage{
		ID:      "msg-user",
		Message: message.Message{Role: "user", Parts: []message.Part{message.TextPart{Text: "approve it"}}},
	}); err != nil {
		t.Fatal(err)
	}

	assistantMsgID := "msg-asst"
	if err := store.SaveMessage(threadID, StoredMessage{
		ID:       assistantMsgID,
		ParentID: "msg-user",
		Message: message.Message{
			Role: "assistant",
			Parts: []message.Part{
				message.ToolCallPart{ToolCallID: "tc1", ToolName: "dangerous_tool", Input: `{"action":"delete"}`},
				message.ToolApprovalRequest{ApprovalID: "approval-1", ToolCallID: "tc1"},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	stepFile, err := store.CreateStepFile(threadID, turnID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := stepFile.Close(); err != nil {
		t.Fatal(err)
	}

	if err := store.SaveStepResult(threadID, turnID, 0, StepResult{
		AssistantMessage: message.Message{
			Role: "assistant",
			Parts: []message.Part{
				message.ToolCallPart{ToolCallID: "tc1", ToolName: "dangerous_tool", Input: `{"action":"delete"}`},
			},
		},
		ToolCalls: []ToolCallInfo{
			{ToolCallID: "tc1", ToolName: "dangerous_tool", Input: `{"action":"delete"}`},
		},
	}); err != nil {
		t.Fatal(err)
	}

	if err := store.SaveQuestion(threadID, turnID, PendingQuestionState{
		ApprovalID:  "approval-1",
		ToolCallID:  "tc1",
		StepIndex:   0,
		ResumePhase: PhaseTools,
		Questions:   json.RawMessage(`[{"question":"Are you sure?"}]`),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAnswer(threadID, turnID, QuestionAnswer{
		ApprovalID: "approval-1",
		Answers:    map[string]string{"q1": "yes"},
	}); err != nil {
		t.Fatal(err)
	}

	turnState := &TurnState{
		ID:                turnID,
		ThreadID:          threadID,
		CurrentStep:       0,
		Phase:             PhaseWaitingForAnswer,
		LeafMsgID:         assistantMsgID,
		Config:            TurnConfig{Model: "test-model"},
		PendingApprovalID: "approval-1",
	}
	if err := store.SaveTurnState(threadID, *turnState); err != nil {
		t.Fatal(err)
	}

	prov := &mockProvider{
		responses: [][]message.ProviderMessageChunk{{
			message.StreamStartChunk{},
			message.TextStartChunk{ID: "t1"},
			message.TextDeltaChunk{ID: "t1", Delta: "Async approval resolved"},
			message.TextEndChunk{ID: "t1"},
			message.FinishChunk{FinishReason: message.FinishReason{Unified: "stop"}},
		}},
	}
	exec := &approvalMockExecutor{
		resolveAsyncSubThreadID: "task-after-approval",
		resolveAsyncResult: message.ToolResultPart{
			ToolCallID: "tc1",
			ToolName:   "dangerous_tool",
			Output:     message.TextOutput{Value: "deleted later"},
		},
	}

	chunks := collectChunks(t, ResumeTurn(context.Background(), prov, exec, store, turnState))

	var hasToolOutput bool
	var hasFinalText bool
	for _, c := range chunks {
		switch chunk := c.(type) {
		case message.ToolOutputAvailableChunk:
			if chunk.ToolCallID == "tc1" {
				hasToolOutput = true
			}
		case message.TextDeltaChunk:
			if chunk.Delta == "Async approval resolved" {
				hasFinalText = true
			}
		}
	}
	if !hasToolOutput {
		t.Fatal("expected tool output after async approval continuation")
	}
	if !hasFinalText {
		t.Fatal("expected final assistant text after async approval continuation")
	}

	state, err := store.LoadTurnState(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if state != nil {
		t.Fatalf("expected turn state to be cleaned up, got %#v", state)
	}
}
