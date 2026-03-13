package integration

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
	_ "github.com/obot-platform/discobot/agent-go/providers/anthropic"
)

const anthropicTestModel = "claude-haiku-4-5-20251001"
const anthropicReasoningModel = "claude-haiku-4-5-20251001" // supports extended thinking (legacy)
const anthropicAdaptiveModel = "claude-sonnet-4-6"          // supports adaptive thinking (4.6+)

func anthropicProvider(t *testing.T) providers.Provider {
	t.Helper()
	cfg := providers.Config{}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		cfg["api_key"] = key
	} else if token := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN"); token != "" {
		cfg["auth_token"] = token
	} else {
		t.Skip("ANTHROPIC_API_KEY or CLAUDE_CODE_OAUTH_TOKEN not set")
	}
	p, err := providers.New("anthropic", cfg)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// isOAuthOnly returns true when the test is running with a CLAUDE_CODE_OAUTH_TOKEN
// but no ANTHROPIC_API_KEY. OAuth tokens have limited API surface: they do not
// support extended thinking.
func isOAuthOnly() bool {
	return os.Getenv("ANTHROPIC_API_KEY") == "" && os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") != ""
}

func isAnthropicBillingError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "credit balance is too low") ||
		strings.Contains(s, "plans & billing") ||
		strings.Contains(s, "purchase credits")
}

func TestAnthropic_SimpleTextCompletion(t *testing.T) {
	t.Parallel()
	p := anthropicProvider(t)

	req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "anthropic", ModelID: anthropicTestModel},
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

func TestAnthropic_ToolCall(t *testing.T) {
	t.Parallel()
	p := anthropicProvider(t)

	req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "anthropic", ModelID: anthropicTestModel},
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

	var toolCalls []anthropicToolCallCapture
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
			toolCalls = append(toolCalls, anthropicToolCallCapture{
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

func TestAnthropic_ToolCallRoundTrip(t *testing.T) {
	t.Parallel()
	p := anthropicProvider(t)

	// Turn 1: model calls the tool.
	turn1Req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "anthropic", ModelID: anthropicTestModel},
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
	var toolName string
	var toolArgs strings.Builder
	for chunk, err := range p.Complete(context.Background(), turn1Req) {
		if err != nil {
			t.Fatal(err)
		}
		switch c := chunk.(type) {
		case message.ToolInputStartChunk:
			callID = c.ToolCallID
			toolName = c.ToolName
		case message.ToolInputDeltaChunk:
			toolArgs.WriteString(c.InputTextDelta)
		}
	}
	if callID == "" {
		t.Fatal("expected a tool call in turn 1")
	}

	// Turn 2: provide tool result, expect text response.
	turn2Req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "anthropic", ModelID: anthropicTestModel},
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
					ToolName:   toolName,
					Input:      toolArgs.String(),
				},
			}},
			{Role: "tool", Parts: []message.Part{
				message.ToolResultPart{
					ToolCallID: callID,
					ToolName:   toolName,
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

func TestAnthropic_MultiTurnConversation(t *testing.T) {
	t.Parallel()
	p := anthropicProvider(t)

	req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "anthropic", ModelID: anthropicTestModel},
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

func TestAnthropic_StreamLifecycle(t *testing.T) {
	t.Parallel()
	p := anthropicProvider(t)

	req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "anthropic", ModelID: anthropicTestModel},
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

func TestAnthropic_ContextCancellation(t *testing.T) {
	t.Parallel()
	p := anthropicProvider(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "anthropic", ModelID: anthropicTestModel},
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

func TestAnthropic_ReasoningCompletion(t *testing.T) {
	t.Parallel()
	if isOAuthOnly() {
		t.Skip("extended thinking not supported with OAuth tokens")
	}
	p := anthropicProvider(t)

	req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "anthropic", ModelID: anthropicReasoningModel},
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
			// Verify signature is captured in ProviderMetadata.
			if len(c.ProviderMetadata) == 0 {
				t.Error("expected ProviderMetadata with signature on ReasoningEndChunk")
			}
			var meta map[string]any
			if err := json.Unmarshal(c.ProviderMetadata, &meta); err != nil {
				t.Fatalf("expected valid JSON ProviderMetadata: %v", err)
			}
			if sig, _ := meta["signature"].(string); sig == "" {
				t.Error("expected non-empty signature in ProviderMetadata")
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
	if usage.OutputTokens.Total == 0 {
		t.Error("expected non-zero output tokens in usage")
	}
}

func TestAnthropic_ReasoningMultiTurn(t *testing.T) {
	t.Parallel()
	if isOAuthOnly() {
		t.Skip("extended thinking not supported with OAuth tokens")
	}
	p := anthropicProvider(t)

	// Turn 1: ask a question with reasoning enabled.
	turn1Req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "anthropic", ModelID: anthropicReasoningModel},
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

	// Verify turn 1 has a reasoning part with ProviderMetadata (signature).
	var hasReasoning bool
	var hasProviderMetadata bool
	for _, part := range turn1Msg.Parts {
		if rp, ok := part.(message.ReasoningPart); ok {
			hasReasoning = true
			if len(rp.ProviderMetadata) > 0 {
				var meta map[string]any
				if json.Unmarshal(rp.ProviderMetadata, &meta) == nil {
					if sig, _ := meta["signature"].(string); sig != "" {
						hasProviderMetadata = true
					}
				}
			}
		}
	}
	if !hasReasoning {
		t.Fatal("expected reasoning part in turn 1 response")
	}
	if !hasProviderMetadata {
		t.Error("expected ProviderMetadata with signature on reasoning part")
	}

	// Turn 2: send reasoning + answer from turn 1 back, ask a follow-up.
	// The thinking block with signature should be preserved across turns.
	turn2Req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "anthropic", ModelID: anthropicReasoningModel},
		Messages: []message.Message{
			{Role: "user", Parts: []message.Part{
				message.TextPart{Text: "Remember the number 73. What is 73 * 2? Reply with just the number."},
			}},
			turn1Msg, // assistant message with thinking + text
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

func TestAnthropic_ReasoningStreamLifecycle(t *testing.T) {
	t.Parallel()
	if isOAuthOnly() {
		t.Skip("extended thinking not supported with OAuth tokens")
	}
	p := anthropicProvider(t)

	req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "anthropic", ModelID: anthropicReasoningModel},
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

// adaptiveThinkingOptions is a ProviderOptions blob that sets effort=max so that
// adaptive thinking reliably produces reasoning blocks even for moderately simple
// questions. Without it the model may skip thinking entirely on easy prompts.
var adaptiveThinkingOptions = json.RawMessage(`{"output_config":{"effort":"max"}}`)

// TestAnthropic_AdaptiveReasoningCompletion verifies that claude-sonnet-4-6 and later
// models correctly use adaptive thinking (type:"adaptive", no budget_tokens, no beta header)
// and still stream reasoning blocks with a valid signature in ProviderMetadata.
// effort=max is used via ProviderOptions to guarantee the model actually thinks.
func TestAnthropic_AdaptiveReasoningCompletion(t *testing.T) {
	t.Parallel()
	if isOAuthOnly() {
		t.Skip("extended thinking not supported with OAuth tokens")
	}
	p := anthropicProvider(t)

	req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "anthropic", ModelID: anthropicAdaptiveModel},
		Messages: []message.Message{
			{Role: "user", Parts: []message.Part{
				// Multi-step problem: ensures the model finds it worthwhile to think.
				message.TextPart{Text: "A train travels 120 km in 1.5 hours, then 90 km in 45 minutes. What is its average speed over the whole journey? Reply with only the number in km/h, rounded to two decimal places."},
			}},
		},
		Reasoning:       "enabled",
		ProviderOptions: adaptiveThinkingOptions,
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
			if len(c.ProviderMetadata) == 0 {
				t.Error("expected ProviderMetadata with signature on ReasoningEndChunk")
			}
			var meta map[string]any
			if err := json.Unmarshal(c.ProviderMetadata, &meta); err != nil {
				t.Fatalf("expected valid JSON ProviderMetadata: %v", err)
			}
			if sig, _ := meta["signature"].(string); sig == "" {
				t.Error("expected non-empty signature in ProviderMetadata")
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
	// 120km/1.5h + 90km/0.75h = 210km/2.25h ≈ 93.33 km/h
	if !strings.Contains(answerText.String(), "93") {
		t.Errorf("expected answer to contain '93', got %q", answerText.String())
	}
	if usage.OutputTokens.Total == 0 {
		t.Error("expected non-zero output tokens in usage")
	}
}

// TestAnthropic_AdaptiveReasoningMultiTurn verifies that thinking block signatures
// from adaptive reasoning are correctly preserved and sent back across turns.
func TestAnthropic_AdaptiveReasoningMultiTurn(t *testing.T) {
	t.Parallel()
	if isOAuthOnly() {
		t.Skip("extended thinking not supported with OAuth tokens")
	}
	p := anthropicProvider(t)

	// Turn 1: multi-step problem to reliably trigger adaptive thinking.
	turn1Req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "anthropic", ModelID: anthropicAdaptiveModel},
		Messages: []message.Message{
			{Role: "user", Parts: []message.Part{
				message.TextPart{Text: "A rectangle has a perimeter of 56 cm and its length is 3 times its width. What is the area? Reply with only the number in cm²."},
			}},
		},
		Reasoning:       "enabled",
		ProviderOptions: adaptiveThinkingOptions,
	}

	acc := message.NewChunkAccumulator()
	for chunk, err := range p.Complete(context.Background(), turn1Req) {
		if err != nil {
			t.Fatal(err)
		}
		acc.Push(chunk)
	}
	acc.Close()
	turn1Msg := acc.Message()

	// Verify turn 1 has a reasoning part with a signature in ProviderMetadata.
	var hasReasoning, hasSignature bool
	for _, part := range turn1Msg.Parts {
		if rp, ok := part.(message.ReasoningPart); ok {
			hasReasoning = true
			var meta map[string]any
			if json.Unmarshal(rp.ProviderMetadata, &meta) == nil {
				if sig, _ := meta["signature"].(string); sig != "" {
					hasSignature = true
				}
			}
		}
	}
	if !hasReasoning {
		t.Fatal("expected reasoning part in turn 1 response")
	}
	if !hasSignature {
		t.Error("expected ProviderMetadata with signature on reasoning part")
	}

	// Turn 2: send the thinking block back and ask a follow-up that depends on turn 1.
	// The thinking block with signature must be preserved for the API to accept it.
	turn2Req := providers.CompleteRequest{
		Model: providers.ModelRef{ProviderID: "anthropic", ModelID: anthropicAdaptiveModel},
		Messages: []message.Message{
			{Role: "user", Parts: []message.Part{
				message.TextPart{Text: "A rectangle has a perimeter of 56 cm and its length is 3 times its width. What is the area? Reply with only the number in cm²."},
			}},
			turn1Msg,
			{Role: "user", Parts: []message.Part{
				message.TextPart{Text: "What is double that area? Reply with only the number in cm²."},
			}},
		},
		Reasoning:       "enabled",
		ProviderOptions: adaptiveThinkingOptions,
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

	// perimeter=56 → 2(l+w)=56 → l+w=28; l=3w → 4w=28 → w=7, l=21; area=147 cm²; double=294
	if !strings.Contains(turn2Text.String(), "294") {
		t.Errorf("expected response to contain '294', got %q", turn2Text.String())
	}
}

type anthropicToolCallCapture struct {
	callID    string
	toolName  string
	arguments string
}
