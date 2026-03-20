package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/coder/websocket"

	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
	_ "github.com/obot-platform/discobot/agent-go/providers/openai"
)

const testModel = "gpt-4.1-nano"

func openaiProvider(t *testing.T) providers.Provider {
	t.Helper()
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	p, err := providers.New("openai", providers.Config{"api_key": apiKey})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func openaiWebSocketProvider(t *testing.T) providers.Provider {
	t.Helper()
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	p, err := providers.New("openai", providers.Config{
		"api_key":       apiKey,
		"use_websocket": "true",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestOpenAI_WebSocketSimpleTextCompletion(t *testing.T) {
	t.Parallel()
	p := openaiWebSocketProvider(t)

	req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: testModel},
		Messages: []message.Message{
			{Role: "system", Parts: []message.Part{
				message.TextPart{Text: "Reply with only the word 'pong'. Nothing else."},
			}},
			{Role: "user", Parts: []message.Part{
				message.TextPart{Text: "ping"},
			}},
		},
	}

	var gotText strings.Builder
	var gotStreamStart, gotFinish bool
	var responseID string
	var usage message.Usage

	for chunk, err := range p.Complete(context.Background(), req) {
		if err != nil {
			t.Fatal(err)
		}
		switch c := chunk.(type) {
		case message.StreamStartChunk:
			gotStreamStart = true
		case message.ResponseMetadataChunk:
			responseID = c.ID
		case message.TextDeltaChunk:
			gotText.WriteString(c.Delta)
		case message.FinishChunk:
			gotFinish = true
			usage = c.Usage
		}
	}

	if !gotStreamStart {
		t.Error("expected StreamStartChunk")
	}
	if responseID == "" {
		t.Error("expected non-empty response ID in ResponseMetadataChunk")
	}
	if !gotFinish {
		t.Error("expected FinishChunk")
	}
	text := strings.TrimSpace(strings.ToLower(gotText.String()))
	if !strings.Contains(text, "pong") {
		t.Errorf("expected response to contain 'pong', got %q", gotText.String())
	}
	if usage.InputTokens.Total == 0 {
		t.Error("expected non-zero input tokens")
	}
	if usage.OutputTokens.Total == 0 {
		t.Error("expected non-zero output tokens")
	}
}

func TestOpenAI_WebSocketToolCallRoundTrip(t *testing.T) {
	t.Parallel()
	p := openaiWebSocketProvider(t)

	toolDef := providers.ToolDefinition{
		Name:        "get_temperature",
		Description: "Get current temperature for a city",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"],"additionalProperties":false}`),
	}

	systemMsg := message.Message{Role: "system", Parts: []message.Part{
		message.TextPart{Text: "You must use the get_temperature tool. After receiving the result, state the temperature in one short sentence."},
	}}
	userMsg := message.Message{Role: "user", Parts: []message.Part{
		message.TextPart{Text: "What is the temperature in London?"},
	}}

	turn1Req := providers.CompleteRequest{
		Model:    providers.ModelRef{ProviderID: "openai", ModelID: testModel},
		Messages: []message.Message{systemMsg, userMsg},
		Tools:    []providers.ToolDefinition{toolDef},
	}

	acc := message.NewChunkAccumulator()
	var finishReason1 string
	for chunk, err := range p.Complete(context.Background(), turn1Req) {
		if err != nil {
			t.Fatal(err)
		}
		if chunk != nil {
			acc.Push(chunk)
		}
		if c, ok := chunk.(message.FinishChunk); ok {
			finishReason1 = c.FinishReason.Unified
		}
	}
	acc.Close()
	assistantMsg := acc.Message()

	if finishReason1 != "tool-calls" {
		t.Fatalf("expected first turn finish reason 'tool-calls', got %q", finishReason1)
	}
	if assistantMsg.ID == "" {
		t.Fatal("expected first turn assistant message to carry OpenAI response ID")
	}

	var toolCall message.ToolCallPart
	for _, part := range assistantMsg.Parts {
		if tc, ok := part.(message.ToolCallPart); ok {
			toolCall = tc
			break
		}
	}
	if toolCall.ToolCallID == "" {
		t.Fatal("expected first turn assistant message to contain a tool call")
	}

	turn2Req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: testModel},
		Messages: []message.Message{
			systemMsg,
			userMsg,
			assistantMsg,
			{Role: "tool", Parts: []message.Part{
				message.ToolResultPart{
					ToolCallID: toolCall.ToolCallID,
					ToolName:   toolCall.ToolName,
					Output:     message.TextOutput{Value: "18 degrees Celsius"},
				},
			}},
		},
		Tools: []providers.ToolDefinition{toolDef},
	}

	var gotText strings.Builder
	var finishReason2 string
	for chunk, err := range p.Complete(context.Background(), turn2Req) {
		if err != nil {
			t.Fatal(err)
		}
		switch c := chunk.(type) {
		case message.TextDeltaChunk:
			gotText.WriteString(c.Delta)
		case message.FinishChunk:
			finishReason2 = c.FinishReason.Unified
		}
	}

	if finishReason2 != "stop" {
		t.Errorf("expected second turn finish reason 'stop', got %q", finishReason2)
	}
	response := strings.ToLower(gotText.String())
	if !strings.Contains(response, "18") {
		t.Errorf("expected second turn response to mention '18', got %q", gotText.String())
	}
}

func TestOpenAI_WebSocketReconnectAfterStalePooledConnDropsPreviousResponseID(t *testing.T) {
	t.Parallel()

	sendEvents := func(ctx context.Context, t *testing.T, conn *websocket.Conn, events []map[string]any) {
		t.Helper()
		for _, ev := range events {
			data, err := json.Marshal(ev)
			if err != nil {
				t.Fatalf("marshal event: %v", err)
			}
			if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
				t.Fatalf("write event: %v", err)
			}
		}
	}

	textCompletion := func(id, text string) []map[string]any {
		return []map[string]any{
			{"type": "response.created", "response": map[string]any{"id": id, "model": testModel}},
			{"type": "response.output_item.added", "item": map[string]any{"id": "msg_" + id, "type": "message"}},
			{"type": "response.content_part.added", "part": map[string]any{"type": "output_text"}, "item_id": "msg_" + id},
			{"type": "response.output_text.delta", "item_id": "msg_" + id, "delta": text},
			{"type": "response.output_text.done", "item_id": "msg_" + id},
			{"type": "response.output_item.done", "item": map[string]any{"id": "msg_" + id, "type": "message"}},
			{"type": "response.completed", "response": map[string]any{
				"status": "completed",
				"output": []any{map[string]any{"type": "message"}},
				"usage": map[string]any{
					"input_tokens":          1,
					"input_tokens_details":  map[string]any{"cached_tokens": 0},
					"output_tokens":         1,
					"output_tokens_details": map[string]any{"reasoning_tokens": 0},
				},
			}},
		}
	}

	var (
		mu              sync.Mutex
		connCount       int
		retryReqPrevID  string
		fallbackReqPrev string
	)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("websocket accept: %v", err)
			return
		}
		defer conn.CloseNow()

		mu.Lock()
		connCount++
		currentConn := connCount
		mu.Unlock()

		switch currentConn {
		case 1:
			if _, _, err := conn.Read(r.Context()); err != nil {
				t.Errorf("conn 1 turn 1 read: %v", err)
				return
			}
			sendEvents(r.Context(), t, conn, textCompletion("resp_1", "first"))

			if _, _, err := conn.Read(r.Context()); err != nil {
				t.Errorf("conn 1 turn 2 read: %v", err)
				return
			}
			if err := conn.Close(websocket.StatusInternalError, "keepalive ping timeout"); err != nil {
				t.Errorf("conn 1 close: %v", err)
			}
		case 2:
			_, data, err := conn.Read(r.Context())
			if err != nil {
				t.Errorf("conn 2 read: %v", err)
				return
			}
			var req map[string]any
			if err := json.Unmarshal(data, &req); err != nil {
				t.Errorf("conn 2 decode: %v", err)
				return
			}
			retryReqPrevID, _ = req["previous_response_id"].(string)
			if err := conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"error","error":{"message":"Previous response with id 'resp_1' not found.","code":"previous_response_not_found"}}`)); err != nil {
				t.Errorf("conn 2 write: %v", err)
			}
		case 3:
			_, data, err := conn.Read(r.Context())
			if err != nil {
				t.Errorf("conn 3 read: %v", err)
				return
			}
			var req map[string]any
			if err := json.Unmarshal(data, &req); err != nil {
				t.Errorf("conn 3 decode: %v", err)
				return
			}
			fallbackReqPrev, _ = req["previous_response_id"].(string)
			sendEvents(r.Context(), t, conn, textCompletion("resp_2", "recovered"))
		default:
			t.Errorf("unexpected connection %d", currentConn)
		}
	}))
	defer ts.Close()

	p, err := providers.New("openai", providers.Config{
		"api_key":       "test-key",
		"use_websocket": "true",
		"base_url":      ts.URL,
	})
	if err != nil {
		t.Fatal(err)
	}

	firstReq := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: testModel},
		Messages: []message.Message{
			{Role: "system", Parts: []message.Part{message.TextPart{Text: "Reply with one word."}}},
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Say first"}}},
		},
	}

	acc := message.NewChunkAccumulator()
	for chunk, err := range p.Complete(context.Background(), firstReq) {
		if err != nil {
			t.Fatalf("first turn: %v", err)
		}
		if chunk != nil {
			acc.Push(chunk)
		}
	}
	acc.Close()
	assistantMsg := acc.Message()
	if assistantMsg.ID == "" {
		t.Fatal("expected first turn assistant message ID")
	}

	secondReq := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: testModel},
		Messages: []message.Message{
			firstReq.Messages[0],
			firstReq.Messages[1],
			assistantMsg,
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Now say recovered"}}},
		},
	}

	var gotText strings.Builder
	for chunk, err := range p.Complete(context.Background(), secondReq) {
		if err != nil {
			t.Fatalf("second turn should recover, got %v", err)
		}
		if delta, ok := chunk.(message.TextDeltaChunk); ok {
			gotText.WriteString(delta.Delta)
		}
	}

	if retryReqPrevID != assistantMsg.ID {
		t.Fatalf("expected retry to preserve previous_response_id=%q, got %q", assistantMsg.ID, retryReqPrevID)
	}
	if fallbackReqPrev != "" {
		t.Fatalf("expected fallback retry to drop previous_response_id, got %q", fallbackReqPrev)
	}
	if strings.TrimSpace(gotText.String()) != "recovered" {
		t.Fatalf("expected recovered final text, got %q", gotText.String())
	}
}

func TestOpenAI_SimpleTextCompletion(t *testing.T) {
	t.Parallel()
	p := openaiProvider(t)

	req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: testModel},
		Messages: []message.Message{
			{Role: "system", Parts: []message.Part{
				message.TextPart{Text: "Reply with only the word 'pong'. Nothing else."},
			}},
			{Role: "user", Parts: []message.Part{
				message.TextPart{Text: "ping"},
			}},
		},
	}

	var gotText strings.Builder
	var gotStreamStart, gotFinish bool
	var usage message.Usage

	for chunk, err := range p.Complete(context.Background(), req) {
		if err != nil {
			t.Fatal(err)
		}
		switch c := chunk.(type) {
		case message.StreamStartChunk:
			gotStreamStart = true
		case message.TextDeltaChunk:
			gotText.WriteString(c.Delta)
		case message.FinishChunk:
			gotFinish = true
			usage = c.Usage
		}
	}

	if !gotStreamStart {
		t.Error("expected StreamStartChunk")
	}
	if !gotFinish {
		t.Error("expected FinishChunk")
	}
	text := strings.TrimSpace(strings.ToLower(gotText.String()))
	if !strings.Contains(text, "pong") {
		t.Errorf("expected response to contain 'pong', got %q", gotText.String())
	}
	if usage.InputTokens.Total == 0 {
		t.Error("expected non-zero input tokens")
	}
	if usage.OutputTokens.Total == 0 {
		t.Error("expected non-zero output tokens")
	}
}

func TestOpenAI_ToolCall(t *testing.T) {
	t.Parallel()
	p := openaiProvider(t)

	req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: testModel},
		Messages: []message.Message{
			{Role: "system", Parts: []message.Part{
				message.TextPart{Text: "You must use the get_weather tool to answer weather questions. Do not answer without calling the tool."},
			}},
			{Role: "user", Parts: []message.Part{
				message.TextPart{Text: "What is the weather in Paris?"},
			}},
		},
		Tools: []providers.ToolDefinition{
			{
				Name:        "get_weather",
				Description: "Get the current weather for a location",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"location":{"type":"string","description":"City name"}},"required":["location"],"additionalProperties":false}`),
			},
		},
	}

	var toolCalls []toolCallCapture
	var gotFinish bool
	var finishReason string
	var currentCallID string
	var currentName string
	var currentArgs strings.Builder

	for chunk, err := range p.Complete(context.Background(), req) {
		if err != nil {
			t.Fatal(err)
		}
		switch c := chunk.(type) {
		case message.ToolInputStartChunk:
			currentCallID = c.ToolCallID
			currentName = c.ToolName
			currentArgs.Reset()
		case message.ToolInputDeltaChunk:
			currentArgs.WriteString(c.InputTextDelta)
		case message.ToolInputEndChunk:
			toolCalls = append(toolCalls, toolCallCapture{
				callID:    currentCallID,
				toolName:  currentName,
				arguments: currentArgs.String(),
			})
		case message.FinishChunk:
			gotFinish = true
			finishReason = c.FinishReason.Unified
		}
	}

	if !gotFinish {
		t.Fatal("expected FinishChunk")
	}
	if finishReason != "tool-calls" {
		t.Errorf("expected finish reason 'tool-calls', got %q", finishReason)
	}
	if len(toolCalls) == 0 {
		t.Fatal("expected at least one tool call")
	}

	tc := toolCalls[0]
	if tc.toolName != "get_weather" {
		t.Errorf("expected tool name 'get_weather', got %q", tc.toolName)
	}
	if tc.callID == "" {
		t.Error("expected non-empty call ID")
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(tc.arguments), &args); err != nil {
		t.Fatalf("expected valid JSON arguments, got %q: %v", tc.arguments, err)
	}
	loc, _ := args["location"].(string)
	if !strings.Contains(strings.ToLower(loc), "paris") {
		t.Errorf("expected location to contain 'paris', got %q", loc)
	}
}

func TestOpenAI_ToolCallRoundTrip(t *testing.T) {
	t.Parallel()
	p := openaiProvider(t)

	// Turn 1: model calls the tool.
	turn1Req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: testModel},
		Messages: []message.Message{
			{Role: "system", Parts: []message.Part{
				message.TextPart{Text: "You must use the get_temperature tool. After receiving the result, state the temperature."},
			}},
			{Role: "user", Parts: []message.Part{
				message.TextPart{Text: "What is the temperature in London?"},
			}},
		},
		Tools: []providers.ToolDefinition{
			{
				Name:        "get_temperature",
				Description: "Get current temperature for a city",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"],"additionalProperties":false}`),
			},
		},
	}

	var callID string
	for chunk, err := range p.Complete(context.Background(), turn1Req) {
		if err != nil {
			t.Fatal(err)
		}
		if c, ok := chunk.(message.ToolInputStartChunk); ok {
			callID = c.ToolCallID
		}
	}
	if callID == "" {
		t.Fatal("expected a tool call in turn 1")
	}

	// Turn 2: provide tool result, expect text response.
	turn2Req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: testModel},
		Messages: []message.Message{
			{Role: "system", Parts: []message.Part{
				message.TextPart{Text: "You must use the get_temperature tool. After receiving the result, state the temperature."},
			}},
			{Role: "user", Parts: []message.Part{
				message.TextPart{Text: "What is the temperature in London?"},
			}},
			{Role: "assistant", Parts: []message.Part{
				message.ToolCallPart{
					ToolCallID: callID,
					ToolName:   "get_temperature",
					Input:      `{"city":"London"}`,
				},
			}},
			{Role: "tool", Parts: []message.Part{
				message.ToolResultPart{
					ToolCallID: callID,
					ToolName:   "get_temperature",
					Output:     message.TextOutput{Value: "18 degrees Celsius"},
				},
			}},
		},
		Tools: []providers.ToolDefinition{
			{
				Name:        "get_temperature",
				Description: "Get current temperature for a city",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"],"additionalProperties":false}`),
			},
		},
	}

	var text strings.Builder
	var finishReason string
	for chunk, err := range p.Complete(context.Background(), turn2Req) {
		if err != nil {
			t.Fatal(err)
		}
		switch c := chunk.(type) {
		case message.TextDeltaChunk:
			text.WriteString(c.Delta)
		case message.FinishChunk:
			finishReason = c.FinishReason.Unified
		}
	}

	if finishReason != "stop" {
		t.Errorf("expected finish reason 'stop', got %q", finishReason)
	}
	response := strings.ToLower(text.String())
	if !strings.Contains(response, "18") {
		t.Errorf("expected response to mention '18', got %q", text.String())
	}
}

func TestOpenAI_MultiTurnConversation(t *testing.T) {
	t.Parallel()
	p := openaiProvider(t)

	req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: testModel},
		Messages: []message.Message{
			{Role: "system", Parts: []message.Part{
				message.TextPart{Text: "You are a helpful assistant. Keep responses very brief."},
			}},
			{Role: "user", Parts: []message.Part{
				message.TextPart{Text: "My name is Alice."},
			}},
			{Role: "assistant", Parts: []message.Part{
				message.TextPart{Text: "Hello Alice! How can I help you?"},
			}},
			{Role: "user", Parts: []message.Part{
				message.TextPart{Text: "What is my name?"},
			}},
		},
	}

	var text strings.Builder
	for chunk, err := range p.Complete(context.Background(), req) {
		if err != nil {
			t.Fatal(err)
		}
		if c, ok := chunk.(message.TextDeltaChunk); ok {
			text.WriteString(c.Delta)
		}
	}

	if !strings.Contains(strings.ToLower(text.String()), "alice") {
		t.Errorf("expected response to contain 'alice', got %q", text.String())
	}
}

func TestOpenAI_StreamLifecycle(t *testing.T) {
	t.Parallel()
	p := openaiProvider(t)

	req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: testModel},
		Messages: []message.Message{
			{Role: "user", Parts: []message.Part{
				message.TextPart{Text: "Say 'hello' and nothing else."},
			}},
		},
	}

	var events []string
	for chunk, err := range p.Complete(context.Background(), req) {
		if err != nil {
			t.Fatal(err)
		}
		switch chunk.(type) {
		case message.StreamStartChunk:
			events = append(events, "stream-start")
		case message.ResponseMetadataChunk:
			events = append(events, "response-metadata")
		case message.TextStartChunk:
			events = append(events, "text-start")
		case message.TextDeltaChunk:
			if len(events) == 0 || events[len(events)-1] != "text-delta" {
				events = append(events, "text-delta")
			}
		case message.TextEndChunk:
			events = append(events, "text-end")
		case message.FinishChunk:
			events = append(events, "finish")
		}
	}

	expected := []string{
		"stream-start",
		"response-metadata",
		"text-start",
		"text-delta",
		"text-end",
		"finish",
	}
	if len(events) != len(expected) {
		t.Fatalf("expected event sequence %v, got %v", expected, events)
	}
	for i, e := range expected {
		if events[i] != e {
			t.Errorf("event[%d]: expected %q, got %q", i, e, events[i])
		}
	}
}

func TestOpenAI_ContextCancellation(t *testing.T) {
	t.Parallel()
	p := openaiProvider(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: testModel},
		Messages: []message.Message{
			{Role: "user", Parts: []message.Part{
				message.TextPart{Text: "Write a very long essay about the history of computing."},
			}},
		},
	}

	chunkCount := 0
	for chunk, err := range p.Complete(ctx, req) {
		if err != nil {
			// Context cancellation may surface as an error — that's expected.
			break
		}
		_ = chunk
		chunkCount++
		if chunkCount >= 3 {
			cancel()
		}
	}
	if chunkCount < 3 {
		t.Errorf("expected to receive at least 3 chunks before cancellation, got %d", chunkCount)
	}
}

const reasoningModel = "o4-mini"

func TestOpenAI_ReasoningCompletion(t *testing.T) {
	t.Parallel()
	p := openaiProvider(t)

	req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: reasoningModel},
		Messages: []message.Message{
			{Role: "user", Parts: []message.Part{
				message.TextPart{Text: "What is 2+2? Reply with just the number."},
			}},
		},
		Reasoning: "enabled",
	}

	var gotReasoningStart, gotReasoningEnd bool
	var reasoningText strings.Builder
	var answerText strings.Builder
	var usage message.Usage

	for chunk, err := range p.Complete(context.Background(), req) {
		if err != nil {
			t.Fatal(err)
		}
		switch c := chunk.(type) {
		case message.ReasoningStartChunk:
			gotReasoningStart = true
		case message.ReasoningDeltaChunk:
			reasoningText.WriteString(c.Delta)
		case message.ReasoningEndChunk:
			gotReasoningEnd = true
			// Verify encrypted_content is captured in ProviderMetadata.
			if len(c.ProviderMetadata) == 0 {
				t.Error("expected ProviderMetadata with encrypted_content on ReasoningEndChunk")
			}
		case message.TextDeltaChunk:
			answerText.WriteString(c.Delta)
		case message.FinishChunk:
			usage = c.Usage
		}
	}

	if !gotReasoningStart {
		t.Error("expected ReasoningStartChunk")
	}
	if !gotReasoningEnd {
		t.Error("expected ReasoningEndChunk")
	}
	if !strings.Contains(answerText.String(), "4") {
		t.Errorf("expected answer to contain '4', got %q", answerText.String())
	}
	// Note: usage.OutputTokens.Reasoning may be 0 for trivial prompts
	// even with reasoning enabled — this is an API behavior, not a bug.
	if usage.OutputTokens.Total == 0 {
		t.Error("expected non-zero output tokens in usage")
	}
}

func TestOpenAI_ReasoningMultiTurn(t *testing.T) {
	t.Parallel()
	p := openaiProvider(t)

	// Turn 1: ask a question with reasoning enabled.
	turn1Req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: reasoningModel},
		Messages: []message.Message{
			{Role: "user", Parts: []message.Part{
				message.TextPart{Text: "Remember the number 73. What is 73 * 2? Reply with just the number."},
			}},
		},
		Reasoning: "enabled",
	}

	// Accumulate turn 1 using ChunkAccumulator.
	acc := message.NewChunkAccumulator()
	for chunk, err := range p.Complete(context.Background(), turn1Req) {
		if err != nil {
			t.Fatal(err)
		}
		acc.Push(chunk)
	}
	acc.Close()
	turn1Msg := acc.Message()

	// Verify turn 1 has a reasoning part with ProviderMetadata.
	var hasReasoning bool
	var hasProviderMetadata bool
	for _, part := range turn1Msg.Parts {
		if rp, ok := part.(message.ReasoningPart); ok {
			hasReasoning = true
			hasProviderMetadata = len(rp.ProviderMetadata) > 0
		}
	}
	if !hasReasoning {
		t.Fatal("expected reasoning part in turn 1 response")
	}
	if !hasProviderMetadata {
		t.Error("expected ProviderMetadata on reasoning part (encrypted_content)")
	}

	// Turn 2: send reasoning + answer from turn 1 back, ask a follow-up.
	// The reasoning context should help the model maintain continuity.
	turn2Req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: reasoningModel},
		Messages: []message.Message{
			{Role: "user", Parts: []message.Part{
				message.TextPart{Text: "Remember the number 73. What is 73 * 2? Reply with just the number."},
			}},
			turn1Msg, // assistant message with reasoning + text
			{Role: "user", Parts: []message.Part{
				message.TextPart{Text: "Now add 1 to that result. Reply with just the number."},
			}},
		},
		Reasoning: "enabled",
	}

	var turn2Text strings.Builder
	for chunk, err := range p.Complete(context.Background(), turn2Req) {
		if err != nil {
			t.Fatal(err)
		}
		if c, ok := chunk.(message.TextDeltaChunk); ok {
			turn2Text.WriteString(c.Delta)
		}
	}

	// 73 * 2 = 146, 146 + 1 = 147
	response := strings.TrimSpace(turn2Text.String())
	if !strings.Contains(response, "147") {
		t.Errorf("expected response to contain '147', got %q", response)
	}
}

func TestOpenAI_ReasoningStreamLifecycle(t *testing.T) {
	t.Parallel()
	p := openaiProvider(t)

	req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "openai", ModelID: reasoningModel},
		Messages: []message.Message{
			{Role: "user", Parts: []message.Part{
				message.TextPart{Text: "Say 'yes'. One word."},
			}},
		},
		Reasoning: "enabled",
	}

	var events []string
	for chunk, err := range p.Complete(context.Background(), req) {
		if err != nil {
			t.Fatal(err)
		}
		switch chunk.(type) {
		case message.StreamStartChunk:
			events = append(events, "stream-start")
		case message.ResponseMetadataChunk:
			events = append(events, "response-metadata")
		case message.ReasoningStartChunk:
			events = append(events, "reasoning-start")
		case message.ReasoningDeltaChunk:
			if len(events) == 0 || events[len(events)-1] != "reasoning-delta" {
				events = append(events, "reasoning-delta")
			}
		case message.ReasoningEndChunk:
			events = append(events, "reasoning-end")
		case message.TextStartChunk:
			events = append(events, "text-start")
		case message.TextDeltaChunk:
			if len(events) == 0 || events[len(events)-1] != "text-delta" {
				events = append(events, "text-delta")
			}
		case message.TextEndChunk:
			events = append(events, "text-end")
		case message.FinishChunk:
			events = append(events, "finish")
		}
	}

	// Verify required events are present in correct order.
	// reasoning-delta is optional (summary can be empty for trivial prompts).
	required := []string{
		"stream-start",
		"response-metadata",
		"reasoning-start",
		"reasoning-end",
		"text-start",
		"text-delta",
		"text-end",
		"finish",
	}
	ri := 0
	for _, e := range events {
		if ri < len(required) && e == required[ri] {
			ri++
		}
	}
	if ri != len(required) {
		t.Errorf("missing required events; want %v in order, got %v", required, events)
	}
}

type toolCallCapture struct {
	callID    string
	toolName  string
	arguments string
}
