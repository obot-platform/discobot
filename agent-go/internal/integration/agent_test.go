package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/obot-platform/discobot/agent-go/internal/api"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
	"github.com/obot-platform/discobot/agent-go/thread"
)

// --- Shared test infrastructure ---

type toolHandler func(input json.RawMessage) (message.ToolResultPart, error)

// testToolExecutor implements thread.ToolExecutor with a map of tool handlers.
type testToolExecutor struct {
	handlers map[string]toolHandler
}

func (e *testToolExecutor) Execute(_ context.Context, _ *thread.ToolContext, call message.ToolCallPart) (thread.ToolExecuteResult, error) {
	handler, ok := e.handlers[call.ToolName]
	if !ok {
		return thread.ToolExecuteResult{
			Result: message.ToolResultPart{
				ToolCallID: call.ToolCallID,
				ToolName:   call.ToolName,
				Output:     message.ErrorTextOutput{Value: fmt.Sprintf("unknown tool: %s", call.ToolName)},
			},
		}, nil
	}
	result, err := handler(json.RawMessage(call.Input))
	if err != nil {
		return thread.ToolExecuteResult{}, err
	}
	result.ToolCallID = call.ToolCallID
	result.ToolName = call.ToolName
	return thread.ToolExecuteResult{Result: result}, nil
}

func (e *testToolExecutor) ResolveAnswer(_ *thread.ToolContext, _ message.ToolCallPart, _ api.AnswerQuestionRequest) (thread.ToolExecuteResult, error) {
	return thread.ToolExecuteResult{}, fmt.Errorf("approval not supported in test executor")
}

func (e *testToolExecutor) ResumeAsync(_ context.Context, _ *thread.ToolContext, _ message.ToolCallPart, _ string, _ *api.AnswerQuestionRequest) (thread.ToolExecuteResult, error) {
	return thread.ToolExecuteResult{}, fmt.Errorf("async not supported in test executor")
}

func (e *testToolExecutor) SetPlanMode(_ bool)   {}
func (e *testToolExecutor) SetThreadID(_ string) {}

// seedSystemMessage saves a system prompt into the store and returns its message ID.
func seedSystemMessage(t *testing.T, store *thread.Store, threadID, prompt string) string {
	t.Helper()
	msgID := "system-msg"
	err := store.SaveMessage(threadID, thread.StoredMessage{
		ID:       msgID,
		ParentID: "",
		Message:  message.Message{Role: "system", Parts: []message.Part{message.TextPart{Text: prompt}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return msgID
}

// runTurnCollect iterates the turn sequence, collecting all chunks. Fatals on errors.
func runTurnCollect(t *testing.T, seq iter.Seq2[message.MessageChunk, error]) []message.MessageChunk {
	t.Helper()
	var chunks []message.MessageChunk
	for chunk, err := range seq {
		if err != nil {
			t.Fatalf("unexpected turn error: %v", err)
		}
		if chunk != nil {
			chunks = append(chunks, chunk)
		}
	}
	return chunks
}

// extractText accumulates all TextDeltaChunk.Delta values into a single string.
func extractText(chunks []message.MessageChunk) string {
	var b strings.Builder
	for _, c := range chunks {
		if td, ok := c.(message.TextDeltaChunk); ok {
			b.WriteString(td.Delta)
		}
	}
	return b.String()
}

func intPtr(n int) *int { return &n }

func agentTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// --- Tests ---

func TestAgent_SimpleTextResponse(t *testing.T) {
	t.Parallel()
	ctx := agentTestContext(t)
	p := openaiProvider(t)
	store := thread.NewStore(t.TempDir())
	threadID := "simple-text"

	leafID := seedSystemMessage(t, store, threadID,
		"You are a math assistant. When asked a math question, respond with ONLY the numeric answer. Nothing else. No explanation, no words, just the number.")

	chunks := runTurnCollect(t, thread.RunTurn(ctx, p, &testToolExecutor{}, store, threadID, leafID, thread.TurnConfig{
		Model:     testModel,
		UserParts: []message.Part{message.TextPart{Text: "What is 2 + 2?"}},
		MaxTokens: intPtr(64),
	}))

	// Verify chunks: should have text, no tool calls.
	text := extractText(chunks)
	if !strings.Contains(text, "4") {
		t.Errorf("expected text to contain '4', got %q", text)
	}

	hasToolInput := false
	for _, c := range chunks {
		if _, ok := c.(message.ToolInputStartChunk); ok {
			hasToolInput = true
		}
	}
	if hasToolInput {
		t.Error("expected no tool calls in simple text response")
	}

	// Verify persistence.
	turnState, err := store.LoadTurnState(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if turnState != nil {
		t.Error("expected turn state to be cleaned up after completion")
	}

	leafMsgID, err := store.FindLeaf(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if leafMsgID == "" {
		t.Fatal("expected non-empty leaf message ID")
	}

	history, err := store.BuildHistory(threadID, leafMsgID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 {
		t.Fatalf("expected 3 messages (system, user, assistant), got %d", len(history))
	}
	if history[0].Role != "system" {
		t.Errorf("history[0] role: expected 'system', got %q", history[0].Role)
	}
	if history[1].Role != "user" {
		t.Errorf("history[1] role: expected 'user', got %q", history[1].Role)
	}
	if history[2].Role != "assistant" {
		t.Errorf("history[2] role: expected 'assistant', got %q", history[2].Role)
	}
}

func TestAgent_SingleToolCall(t *testing.T) {
	t.Parallel()
	ctx := agentTestContext(t)
	p := openaiProvider(t)
	store := thread.NewStore(t.TempDir())
	threadID := "single-tool"

	leafID := seedSystemMessage(t, store, threadID,
		"You have access to a get_weather tool. You MUST call the get_weather tool to answer any weather question. After receiving the tool result, state the weather in exactly one sentence.")

	executor := &testToolExecutor{
		handlers: map[string]toolHandler{
			"get_weather": func(_ json.RawMessage) (message.ToolResultPart, error) {
				return message.ToolResultPart{
					Output: message.TextOutput{Value: "Sunny, 25 degrees Celsius"},
				}, nil
			},
		},
	}

	chunks := runTurnCollect(t, thread.RunTurn(ctx, p, executor, store, threadID, leafID, thread.TurnConfig{
		Model:     testModel,
		UserParts: []message.Part{message.TextPart{Text: "What is the weather in Tokyo?"}},
		MaxTokens: intPtr(256),
		Tools: []providers.ToolDefinition{{
			Name:        "get_weather",
			Description: "Get the current weather for a city",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string","description":"City name"}},"required":["city"],"additionalProperties":false}`),
		}},
	}))

	// Verify the model called get_weather.
	gotToolCall := false
	for _, c := range chunks {
		if ts, ok := c.(message.ToolInputStartChunk); ok && ts.ToolName == "get_weather" {
			gotToolCall = true
		}
	}
	if !gotToolCall {
		t.Error("expected a get_weather tool call")
	}

	// Verify the final text references the tool result.
	text := strings.ToLower(extractText(chunks))
	if !strings.Contains(text, "25") && !strings.Contains(text, "sunny") {
		t.Errorf("expected text to reference tool result (25 or sunny), got %q", text)
	}

	// Verify persistence: 5 messages (system, user, assistant+tool, tool result, assistant text).
	turnState, _ := store.LoadTurnState(threadID)
	if turnState != nil {
		t.Error("expected turn state to be cleaned up")
	}

	leafMsgID, err := store.FindLeaf(threadID)
	if err != nil || leafMsgID == "" {
		t.Fatal("expected non-empty leaf")
	}

	history, err := store.BuildHistory(threadID, leafMsgID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(history))
	}
	if history[2].Role != "assistant" {
		t.Errorf("history[2] role: expected 'assistant', got %q", history[2].Role)
	}
	if history[3].Role != "tool" {
		t.Errorf("history[3] role: expected 'tool', got %q", history[3].Role)
	}
	if history[4].Role != "assistant" {
		t.Errorf("history[4] role: expected 'assistant', got %q", history[4].Role)
	}

	// Verify the tool call message has a ToolCallPart.
	hasToolCallPart := false
	for _, part := range history[2].Parts {
		if tc, ok := part.(message.ToolCallPart); ok && tc.ToolName == "get_weather" {
			hasToolCallPart = true
		}
	}
	if !hasToolCallPart {
		t.Error("expected assistant message to have ToolCallPart for get_weather")
	}
}

func TestAgent_MultiStepToolCalls(t *testing.T) {
	t.Parallel()
	ctx := agentTestContext(t)
	p := openaiProvider(t)
	store := thread.NewStore(t.TempDir())
	threadID := "multi-step"

	leafID := seedSystemMessage(t, store, threadID,
		"You have two tools: get_population and get_area. To answer questions about population density, you MUST first call get_population, then call get_area, then compute the density yourself. Always call the tools one at a time. After computing, state the result.")

	executor := &testToolExecutor{
		handlers: map[string]toolHandler{
			"get_population": func(_ json.RawMessage) (message.ToolResultPart, error) {
				return message.ToolResultPart{
					Output: message.TextOutput{Value: "67000000"},
				}, nil
			},
			"get_area": func(_ json.RawMessage) (message.ToolResultPart, error) {
				return message.ToolResultPart{
					Output: message.TextOutput{Value: "551695"},
				}, nil
			},
		},
	}

	chunks := runTurnCollect(t, thread.RunTurn(ctx, p, executor, store, threadID, leafID, thread.TurnConfig{
		Model:     testModel,
		UserParts: []message.Part{message.TextPart{Text: "What is the population density of France?"}},
		MaxTokens: intPtr(512),
		Tools: []providers.ToolDefinition{
			{
				Name:        "get_population",
				Description: "Get the population of a country. Returns a number.",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"country":{"type":"string"}},"required":["country"],"additionalProperties":false}`),
			},
			{
				Name:        "get_area",
				Description: "Get the area of a country in square kilometers. Returns a number.",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"country":{"type":"string"}},"required":["country"],"additionalProperties":false}`),
			},
		},
	}))

	// Verify both tools were called.
	toolNames := make(map[string]bool)
	for _, c := range chunks {
		if ts, ok := c.(message.ToolInputStartChunk); ok {
			toolNames[ts.ToolName] = true
		}
	}
	if !toolNames["get_population"] {
		t.Error("expected get_population tool call")
	}
	if !toolNames["get_area"] {
		t.Error("expected get_area tool call")
	}

	// Verify final text exists.
	text := extractText(chunks)
	if text == "" {
		t.Error("expected non-empty final text response")
	}

	// Verify persistence.
	turnState, _ := store.LoadTurnState(threadID)
	if turnState != nil {
		t.Error("expected turn state to be cleaned up")
	}

	leafMsgID, err := store.FindLeaf(threadID)
	if err != nil || leafMsgID == "" {
		t.Fatal("expected non-empty leaf")
	}

	history, err := store.BuildHistory(threadID, leafMsgID)
	if err != nil {
		t.Fatal(err)
	}

	// Minimum 5 messages: system, user, assistant(tools), tool(results), assistant(final).
	// Could be 7+ if model calls tools sequentially across steps.
	if len(history) < 5 {
		t.Fatalf("expected at least 5 messages, got %d", len(history))
	}

	// Final message should be an assistant text message.
	lastMsg := history[len(history)-1]
	if lastMsg.Role != "assistant" {
		t.Errorf("last message role: expected 'assistant', got %q", lastMsg.Role)
	}
	hasText := false
	for _, part := range lastMsg.Parts {
		if _, ok := part.(message.TextPart); ok {
			hasText = true
		}
	}
	if !hasText {
		t.Error("expected final assistant message to have a TextPart")
	}

	// Verify both tool names appear in the history.
	historyToolNames := make(map[string]bool)
	for _, msg := range history {
		for _, part := range msg.Parts {
			if tc, ok := part.(message.ToolCallPart); ok {
				historyToolNames[tc.ToolName] = true
			}
		}
	}
	if !historyToolNames["get_population"] {
		t.Error("expected get_population in persisted history")
	}
	if !historyToolNames["get_area"] {
		t.Error("expected get_area in persisted history")
	}
}

func TestAgent_MultiTurnConversation(t *testing.T) {
	t.Parallel()
	ctx := agentTestContext(t)
	p := openaiProvider(t)
	store := thread.NewStore(t.TempDir())
	threadID := "multi-turn"

	leafID := seedSystemMessage(t, store, threadID,
		"You are a helpful assistant. Keep all responses under 20 words.")

	// Turn 1: Establish context.
	runTurnCollect(t, thread.RunTurn(ctx, p, &testToolExecutor{}, store, threadID, leafID, thread.TurnConfig{
		Model:     testModel,
		UserParts: []message.Part{message.TextPart{Text: "My favorite color is blue. Remember that."}},
		MaxTokens: intPtr(128),
	}))

	// Get leaf after turn 1.
	turn1Leaf, err := store.FindLeaf(threadID)
	if err != nil || turn1Leaf == "" {
		t.Fatal("expected non-empty leaf after turn 1")
	}

	// Turn 2: Ask about the context from turn 1.
	chunks2 := runTurnCollect(t, thread.RunTurn(ctx, p, &testToolExecutor{}, store, threadID, turn1Leaf, thread.TurnConfig{
		Model:     testModel,
		UserParts: []message.Part{message.TextPart{Text: "What is my favorite color?"}},
		MaxTokens: intPtr(128),
	}))

	// Verify the model remembers "blue".
	text2 := strings.ToLower(extractText(chunks2))
	if !strings.Contains(text2, "blue") {
		t.Errorf("expected turn 2 response to contain 'blue', got %q", text2)
	}

	// Verify persistence: full conversation chain.
	turn2Leaf, err := store.FindLeaf(threadID)
	if err != nil || turn2Leaf == "" {
		t.Fatal("expected non-empty leaf after turn 2")
	}

	history, err := store.BuildHistory(threadID, turn2Leaf)
	if err != nil {
		t.Fatal(err)
	}

	// At least: system, user1, assistant1, user2, assistant2.
	if len(history) < 5 {
		t.Fatalf("expected at least 5 messages in history, got %d", len(history))
	}

	// First message is system, last is assistant.
	if history[0].Role != "system" {
		t.Errorf("history[0] role: expected 'system', got %q", history[0].Role)
	}
	if history[len(history)-1].Role != "assistant" {
		t.Errorf("last message role: expected 'assistant', got %q", history[len(history)-1].Role)
	}

	// Turn state should be cleaned up for both turns.
	turnState, _ := store.LoadTurnState(threadID)
	if turnState != nil {
		t.Error("expected turn state to be cleaned up")
	}
}

func TestAgent_ToolExecutionError(t *testing.T) {
	t.Parallel()
	ctx := agentTestContext(t)
	p := openaiProvider(t)
	store := thread.NewStore(t.TempDir())
	threadID := "tool-error"

	leafID := seedSystemMessage(t, store, threadID,
		"You have a get_data tool. You MUST call the get_data tool for any data request. If the tool returns an error, explain the error to the user in one short sentence.")

	executor := &testToolExecutor{
		handlers: map[string]toolHandler{
			"get_data": func(_ json.RawMessage) (message.ToolResultPart, error) {
				return message.ToolResultPart{}, fmt.Errorf("connection timeout: database unavailable")
			},
		},
	}

	chunks := runTurnCollect(t, thread.RunTurn(ctx, p, executor, store, threadID, leafID, thread.TurnConfig{
		Model:     testModel,
		UserParts: []message.Part{message.TextPart{Text: "Get the data for item XYZ."}},
		MaxTokens: intPtr(256),
		Tools: []providers.ToolDefinition{{
			Name:        "get_data",
			Description: "Retrieve data for an item by ID",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"item_id":{"type":"string"}},"required":["item_id"],"additionalProperties":false}`),
		}},
	}))

	// Verify tool was called.
	gotToolCall := false
	for _, c := range chunks {
		if ts, ok := c.(message.ToolInputStartChunk); ok && ts.ToolName == "get_data" {
			gotToolCall = true
		}
	}
	if !gotToolCall {
		t.Error("expected a get_data tool call")
	}

	// Verify there's a tool error chunk.
	gotToolError := false
	for _, c := range chunks {
		if _, ok := c.(message.ToolOutputErrorChunk); ok {
			gotToolError = true
		}
	}
	if !gotToolError {
		t.Error("expected a ToolOutputErrorChunk for the failed tool")
	}

	// Verify the model produced a text response referencing the error.
	text := strings.ToLower(extractText(chunks))
	referencesError := strings.Contains(text, "error") ||
		strings.Contains(text, "timeout") ||
		strings.Contains(text, "unavailable") ||
		strings.Contains(text, "database") ||
		strings.Contains(text, "connection")
	if !referencesError {
		t.Errorf("expected text to reference the error, got %q", text)
	}

	// Verify persistence: 5 messages.
	leafMsgID, err := store.FindLeaf(threadID)
	if err != nil || leafMsgID == "" {
		t.Fatal("expected non-empty leaf")
	}
	history, err := store.BuildHistory(threadID, leafMsgID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(history))
	}

	// Verify the tool result message has an ErrorTextOutput.
	toolMsg := history[3]
	if toolMsg.Role != "tool" {
		t.Fatalf("history[3] role: expected 'tool', got %q", toolMsg.Role)
	}
	hasErrorOutput := false
	for _, part := range toolMsg.Parts {
		if tr, ok := part.(message.ToolResultPart); ok {
			if _, isErr := tr.Output.(message.ErrorTextOutput); isErr {
				hasErrorOutput = true
			}
		}
	}
	if !hasErrorOutput {
		t.Error("expected tool result to have ErrorTextOutput")
	}
}

func TestAgent_MessagePersistence(t *testing.T) {
	t.Parallel()
	ctx := agentTestContext(t)
	p := openaiProvider(t)
	store := thread.NewStore(t.TempDir())
	threadID := "persistence"

	leafID := seedSystemMessage(t, store, threadID,
		"You have a lookup tool. You MUST use the lookup tool for any question. After the result, respond with the answer in one sentence.")

	executor := &testToolExecutor{
		handlers: map[string]toolHandler{
			"lookup": func(_ json.RawMessage) (message.ToolResultPart, error) {
				return message.ToolResultPart{
					Output: message.TextOutput{Value: "Item 42: The Answer to Everything"},
				}, nil
			},
		},
	}

	runTurnCollect(t, thread.RunTurn(ctx, p, executor, store, threadID, leafID, thread.TurnConfig{
		Model:     testModel,
		UserParts: []message.Part{message.TextPart{Text: "Look up item 42."}},
		MaxTokens: intPtr(256),
		Tools: []providers.ToolDefinition{{
			Name:        "lookup",
			Description: "Look up an item by number",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"number":{"type":"integer"}},"required":["number"],"additionalProperties":false}`),
		}},
	}))

	// 1. Turn state cleanup.
	turnState, err := store.LoadTurnState(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if turnState != nil {
		t.Error("expected turn state to be nil after completion")
	}

	// 2. FindLeaf returns a valid ID.
	leafMsgID, err := store.FindLeaf(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if leafMsgID == "" {
		t.Fatal("expected non-empty leaf message ID")
	}

	// 3. BuildHistory returns correct message sequence.
	history, err := store.BuildHistory(threadID, leafMsgID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 5 {
		t.Fatalf("expected 5 messages [system, user, assistant, tool, assistant], got %d", len(history))
	}

	// Verify roles.
	expectedRoles := []string{"system", "user", "assistant", "tool", "assistant"}
	for i, role := range expectedRoles {
		if history[i].Role != role {
			t.Errorf("history[%d] role: expected %q, got %q", i, role, history[i].Role)
		}
	}

	// Verify system message content.
	sysText := ""
	for _, part := range history[0].Parts {
		if tp, ok := part.(message.TextPart); ok {
			sysText = tp.Text
		}
	}
	if !strings.Contains(sysText, "lookup tool") {
		t.Errorf("system message text: expected to contain 'lookup tool', got %q", sysText)
	}

	// Verify user message content.
	userText := ""
	for _, part := range history[1].Parts {
		if tp, ok := part.(message.TextPart); ok {
			userText = tp.Text
		}
	}
	if userText != "Look up item 42." {
		t.Errorf("user message: expected 'Look up item 42.', got %q", userText)
	}

	// Verify assistant message has ToolCallPart for "lookup".
	hasLookupCall := false
	for _, part := range history[2].Parts {
		if tc, ok := part.(message.ToolCallPart); ok && tc.ToolName == "lookup" {
			hasLookupCall = true
		}
	}
	if !hasLookupCall {
		t.Error("expected assistant message to have ToolCallPart for 'lookup'")
	}

	// Verify tool result message.
	hasLookupResult := false
	for _, part := range history[3].Parts {
		if tr, ok := part.(message.ToolResultPart); ok && tr.ToolName == "lookup" {
			if to, ok := tr.Output.(message.TextOutput); ok && strings.Contains(to.Value, "42") {
				hasLookupResult = true
			}
		}
	}
	if !hasLookupResult {
		t.Error("expected tool message to have ToolResultPart with '42' in output")
	}

	// Verify final assistant message has a TextPart.
	finalHasText := false
	for _, part := range history[4].Parts {
		if tp, ok := part.(message.TextPart); ok && tp.Text != "" {
			finalHasText = true
		}
	}
	if !finalHasText {
		t.Error("expected final assistant message to have non-empty TextPart")
	}

	// 4. Verify parent chain by walking from leaf to root.
	currentID := leafMsgID
	msgCount := 0
	for currentID != "" {
		msg, err := store.LoadMessage(threadID, currentID)
		if err != nil {
			t.Fatalf("failed to load message %s: %v", currentID, err)
		}
		msgCount++
		currentID = msg.ParentID
	}
	if msgCount != 5 {
		t.Errorf("parent chain walk: expected 5 messages, got %d", msgCount)
	}

	// 5. ListThreads returns exactly 1 thread.
	threads, err := store.ListThreads()
	if err != nil {
		t.Fatal(err)
	}
	if len(threads) != 1 {
		t.Errorf("expected 1 thread, got %d", len(threads))
	}
	if len(threads) == 1 && threads[0] != threadID {
		t.Errorf("expected thread ID %q, got %q", threadID, threads[0])
	}
}
