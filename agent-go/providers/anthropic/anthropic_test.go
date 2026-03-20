package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
)

func TestNew(t *testing.T) {
	t.Run("requires api key or auth token", func(t *testing.T) {
		t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
		_, err := New(providers.Config{})
		if err == nil {
			t.Fatal("expected error when neither api_key nor auth_token is set")
		}
	})

	t.Run("accepts api key", func(t *testing.T) {
		t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
		p, err := New(providers.Config{"api_key": "test-key"})
		if err != nil {
			t.Fatal(err)
		}
		ap := p.(*Provider)
		if ap.apiKey != "test-key" {
			t.Errorf("expected apiKey 'test-key', got %q", ap.apiKey)
		}
		if ap.authToken != "" {
			t.Errorf("expected empty authToken, got %q", ap.authToken)
		}
	})

	t.Run("accepts auth_token config key", func(t *testing.T) {
		p, err := New(providers.Config{"auth_token": "oauth-token"})
		if err != nil {
			t.Fatal(err)
		}
		ap := p.(*Provider)
		if ap.authToken != "oauth-token" {
			t.Errorf("expected authToken 'oauth-token', got %q", ap.authToken)
		}
	})

	t.Run("falls back to CLAUDE_CODE_OAUTH_TOKEN env var", func(t *testing.T) {
		t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "env-oauth-token")
		p, err := New(providers.Config{})
		if err != nil {
			t.Fatal(err)
		}
		ap := p.(*Provider)
		if ap.authToken != "env-oauth-token" {
			t.Errorf("expected authToken 'env-oauth-token', got %q", ap.authToken)
		}
	})

	t.Run("auth_token config takes precedence over env var", func(t *testing.T) {
		t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "env-token")
		p, err := New(providers.Config{"auth_token": "config-token"})
		if err != nil {
			t.Fatal(err)
		}
		ap := p.(*Provider)
		if ap.authToken != "config-token" {
			t.Errorf("expected authToken 'config-token', got %q", ap.authToken)
		}
	})

	t.Run("uses default base url", func(t *testing.T) {
		t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
		p, err := New(providers.Config{"api_key": "test-key"})
		if err != nil {
			t.Fatal(err)
		}
		ap := p.(*Provider)
		if ap.baseURL != defaultBaseURL {
			t.Errorf("expected base URL %q, got %q", defaultBaseURL, ap.baseURL)
		}
	})

	t.Run("uses custom base url and strips trailing slash", func(t *testing.T) {
		t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
		p, err := New(providers.Config{"api_key": "test-key", "base_url": "https://custom.api.com/v1/"})
		if err != nil {
			t.Fatal(err)
		}
		ap := p.(*Provider)
		if ap.baseURL != "https://custom.api.com/v1" {
			t.Errorf("expected base URL %q, got %q", "https://custom.api.com/v1", ap.baseURL)
		}
	})
}

func TestProviderID(t *testing.T) {
	p, _ := New(providers.Config{"api_key": "test"})
	if p.ID() != "anthropic" {
		t.Errorf("expected ID %q, got %q", "anthropic", p.ID())
	}
}

func TestSupportsAdaptiveThinking(t *testing.T) {
	cases := []struct {
		modelID string
		want    bool
	}{
		{"claude-sonnet-4-6", true},
		{"claude-opus-4-6", true},
		{"claude-sonnet-4-7", true},          // future minor
		{"claude-opus-5-0", true},            // future major
		{"claude-haiku-4-5-20251001", false}, // old-style with date suffix
		{"claude-3-7-sonnet-20250219", false},
		{"claude-haiku-4-5", false}, // 4.5 < 4.6
		{"claude-sonnet-4-5", false},
		{"", false},
		{"somemodel", false},
	}
	for _, tc := range cases {
		got := supportsAdaptiveThinking(tc.modelID)
		if got != tc.want {
			t.Errorf("supportsAdaptiveThinking(%q) = %v, want %v", tc.modelID, got, tc.want)
		}
	}
}

func TestConvertMessages(t *testing.T) {
	t.Run("system message extracted to system string", func(t *testing.T) {
		msgs := []message.Message{
			{Role: "system", Parts: []message.Part{message.TextPart{Text: "You are helpful"}}},
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hello"}}},
		}
		system, converted, err := convertMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		if system != "You are helpful" {
			t.Errorf("expected system %q, got %q", "You are helpful", system)
		}
		if len(converted) != 1 {
			t.Fatalf("expected 1 message (user), got %d", len(converted))
		}
		if converted[0]["role"] != "user" {
			t.Errorf("expected role 'user', got %q", converted[0]["role"])
		}
	})

	t.Run("multiple system messages are joined", func(t *testing.T) {
		msgs := []message.Message{
			{Role: "system", Parts: []message.Part{message.TextPart{Text: "You are helpful"}}},
			{Role: "system", Parts: []message.Part{message.TextPart{Text: "Be concise"}}},
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hi"}}},
		}
		system, _, err := convertMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(system, "You are helpful") || !strings.Contains(system, "Be concise") {
			t.Errorf("expected joined system messages, got %q", system)
		}
	})

	t.Run("user message text content", func(t *testing.T) {
		msgs := []message.Message{
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hello"}}},
		}
		_, converted, err := convertMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		if len(converted) != 1 {
			t.Fatalf("expected 1 message, got %d", len(converted))
		}
		msg := converted[0]
		if msg["role"] != "user" {
			t.Errorf("expected role 'user', got %q", msg["role"])
		}
		content := msg["content"].([]any)
		if len(content) != 1 {
			t.Fatalf("expected 1 content block, got %d", len(content))
		}
		block := content[0].(map[string]any)
		if block["type"] != "text" {
			t.Errorf("expected type 'text', got %q", block["type"])
		}
		if block["text"] != "Hello" {
			t.Errorf("expected text 'Hello', got %q", block["text"])
		}
	})

	t.Run("user message with http image", func(t *testing.T) {
		msgs := []message.Message{
			{Role: "user", Parts: []message.Part{
				message.ImagePart{Image: "https://example.com/img.jpg"},
			}},
		}
		_, converted, err := convertMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		content := converted[0]["content"].([]any)
		block := content[0].(map[string]any)
		if block["type"] != "image" {
			t.Errorf("expected type 'image', got %q", block["type"])
		}
		source := block["source"].(map[string]any)
		if source["type"] != "url" {
			t.Errorf("expected source type 'url', got %q", source["type"])
		}
		if source["url"] != "https://example.com/img.jpg" {
			t.Errorf("expected image URL, got %q", source["url"])
		}
	})

	t.Run("user message with base64 image", func(t *testing.T) {
		msgs := []message.Message{
			{Role: "user", Parts: []message.Part{
				message.ImagePart{Image: "abc123base64", MediaType: "image/png"},
			}},
		}
		_, converted, err := convertMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		content := converted[0]["content"].([]any)
		block := content[0].(map[string]any)
		source := block["source"].(map[string]any)
		if source["type"] != "base64" {
			t.Errorf("expected source type 'base64', got %q", source["type"])
		}
		if source["media_type"] != "image/png" {
			t.Errorf("expected media_type 'image/png', got %q", source["media_type"])
		}
		if source["data"] != "abc123base64" {
			t.Errorf("expected data 'abc123base64', got %q", source["data"])
		}
	})

	t.Run("assistant message with text and tool call", func(t *testing.T) {
		msgs := []message.Message{
			{Role: "assistant", Parts: []message.Part{
				message.TextPart{Text: "Let me check"},
				message.ToolCallPart{
					ToolCallID: "toolu_01abc",
					ToolName:   "get_weather",
					Input:      `{"location":"Paris"}`,
				},
			}},
		}
		_, converted, err := convertMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		if len(converted) != 1 {
			t.Fatalf("expected 1 message, got %d", len(converted))
		}
		msg := converted[0]
		if msg["role"] != "assistant" {
			t.Errorf("expected role 'assistant', got %q", msg["role"])
		}
		content := msg["content"].([]any)
		if len(content) != 2 {
			t.Fatalf("expected 2 content blocks (text + tool_use), got %d", len(content))
		}

		textBlock := content[0].(map[string]any)
		if textBlock["type"] != "text" {
			t.Errorf("expected first block type 'text', got %q", textBlock["type"])
		}
		if textBlock["text"] != "Let me check" {
			t.Errorf("expected text 'Let me check', got %q", textBlock["text"])
		}

		toolBlock := content[1].(map[string]any)
		if toolBlock["type"] != "tool_use" {
			t.Errorf("expected second block type 'tool_use', got %q", toolBlock["type"])
		}
		if toolBlock["id"] != "toolu_01abc" {
			t.Errorf("expected id 'toolu_01abc', got %q", toolBlock["id"])
		}
		if toolBlock["name"] != "get_weather" {
			t.Errorf("expected name 'get_weather', got %q", toolBlock["name"])
		}
		// Input should be a JSON object, not a string.
		input, ok := toolBlock["input"].(map[string]any)
		if !ok {
			t.Fatalf("expected input to be map, got %T", toolBlock["input"])
		}
		if input["location"] != "Paris" {
			t.Errorf("expected location 'Paris', got %v", input["location"])
		}
	})

	t.Run("assistant message with reasoning and provider metadata", func(t *testing.T) {
		providerMeta := json.RawMessage(`{"anthropic":{"type":"thinking","thinking":"Let me think...","signature":"ErUk_sig"}}`)
		msgs := []message.Message{
			{Role: "assistant", Parts: []message.Part{
				message.ReasoningPart{
					ID:               "th_1",
					Text:             "Let me think...",
					ProviderMetadata: providerMeta,
				},
				message.TextPart{Text: "The answer is 42."},
			}},
		}
		_, converted, err := convertMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		msg := converted[0]
		content := msg["content"].([]any)
		if len(content) != 2 {
			t.Fatalf("expected 2 content blocks (thinking + text), got %d", len(content))
		}

		// First: thinking block with signature.
		thinkBlock := content[0].(map[string]any)
		if thinkBlock["type"] != "thinking" {
			t.Errorf("expected type 'thinking', got %q", thinkBlock["type"])
		}
		if thinkBlock["signature"] != "ErUk_sig" {
			t.Errorf("expected signature 'ErUk_sig', got %v", thinkBlock["signature"])
		}
	})

	t.Run("reasoning part without provider metadata is skipped", func(t *testing.T) {
		msgs := []message.Message{
			{Role: "assistant", Parts: []message.Part{
				message.ReasoningPart{ID: "th_1", Text: "No signature here"},
				message.TextPart{Text: "Answer."},
			}},
		}
		_, converted, err := convertMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		msg := converted[0]
		content := msg["content"].([]any)
		// Only the text block should be present.
		if len(content) != 1 {
			t.Fatalf("expected 1 content block (text only), got %d", len(content))
		}
		if content[0].(map[string]any)["type"] != "text" {
			t.Errorf("expected 'text' block, got %q", content[0].(map[string]any)["type"])
		}
	})

	t.Run("tool role becomes user message with tool_result blocks", func(t *testing.T) {
		msgs := []message.Message{
			{Role: "tool", Parts: []message.Part{
				message.ToolResultPart{
					ToolCallID: "toolu_01abc",
					ToolName:   "get_weather",
					Output:     message.TextOutput{Value: "25C sunny"},
				},
			}},
		}
		_, converted, err := convertMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		if len(converted) != 1 {
			t.Fatalf("expected 1 message, got %d", len(converted))
		}
		msg := converted[0]
		if msg["role"] != "user" {
			t.Errorf("expected role 'user' for tool result, got %q", msg["role"])
		}
		content := msg["content"].([]any)
		block := content[0].(map[string]any)
		if block["type"] != "tool_result" {
			t.Errorf("expected type 'tool_result', got %q", block["type"])
		}
		if block["tool_use_id"] != "toolu_01abc" {
			t.Errorf("expected tool_use_id 'toolu_01abc', got %q", block["tool_use_id"])
		}
		if block["content"] != "25C sunny" {
			t.Errorf("expected content '25C sunny', got %q", block["content"])
		}
	})

	t.Run("multiple tool results in one message", func(t *testing.T) {
		msgs := []message.Message{
			{Role: "tool", Parts: []message.Part{
				message.ToolResultPart{ToolCallID: "call_1", Output: message.TextOutput{Value: "a"}},
				message.ToolResultPart{ToolCallID: "call_2", Output: message.TextOutput{Value: "b"}},
			}},
		}
		_, converted, err := convertMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		content := converted[0]["content"].([]any)
		if len(content) != 2 {
			t.Fatalf("expected 2 tool_result blocks, got %d", len(content))
		}
	})
}

func TestConvertTools(t *testing.T) {
	t.Run("maps to Anthropic tool format", func(t *testing.T) {
		tools := []providers.ToolDefinition{
			{
				Name:        "get_weather",
				Description: "Get current weather",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}}}`),
			},
		}
		result := convertTools(tools)
		if len(result) != 1 {
			t.Fatalf("expected 1 tool, got %d", len(result))
		}
		// Anthropic uses "name" and "input_schema" at top level (not nested under "function").
		if result[0]["name"] != "get_weather" {
			t.Errorf("expected name 'get_weather', got %q", result[0]["name"])
		}
		if result[0]["description"] != "Get current weather" {
			t.Errorf("expected description, got %q", result[0]["description"])
		}
		if _, ok := result[0]["input_schema"]; !ok {
			t.Error("expected input_schema field")
		}
	})

	t.Run("omits empty description", func(t *testing.T) {
		tools := []providers.ToolDefinition{
			{Name: "fn", InputSchema: json.RawMessage(`{}`)},
		}
		result := convertTools(tools)
		if _, ok := result[0]["description"]; ok {
			t.Error("expected description to be omitted when empty")
		}
	})

	t.Run("nil tools returns nil", func(t *testing.T) {
		result := convertTools(nil)
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})
}

func TestToolResultToString(t *testing.T) {
	tests := []struct {
		name     string
		output   message.ToolResultOutput
		expected string
	}{
		{"text output", message.TextOutput{Value: "hello"}, "hello"},
		{"json output", message.JSONOutput{Value: json.RawMessage(`{"key":"val"}`)}, `{"key":"val"}`},
		{"error text", message.ErrorTextOutput{Value: "oops"}, "oops"},
		{"error json", message.ErrorJSONOutput{Value: json.RawMessage(`{"err":true}`)}, `{"err":true}`},
		{"execution denied with reason", message.ExecutionDeniedOutput{Reason: "not allowed"}, "Execution denied: not allowed"},
		{"execution denied no reason", message.ExecutionDeniedOutput{}, "Execution denied"},
		{"content output with text", message.ContentOutput{
			Value: []message.ToolResultContentItem{
				message.ContentTextItem{Text: "line1"},
				message.ContentTextItem{Text: "line2"},
			},
		}, "line1\nline2"},
		{"nil output", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toolResultToString(tt.output)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestToolResultToAnthropicContent(t *testing.T) {
	t.Run("plain text output returns string", func(t *testing.T) {
		result := toolResultToAnthropicContent(message.TextOutput{Value: "hello"})
		text, ok := result.(string)
		if !ok {
			t.Fatalf("expected string, got %T", result)
		}
		if text != "hello" {
			t.Errorf("expected hello, got %q", text)
		}
	})

	t.Run("content output with image and pdf returns blocks", func(t *testing.T) {
		result := toolResultToAnthropicContent(message.ContentOutput{
			Value: []message.ToolResultContentItem{
				message.ContentTextItem{Text: "summary"},
				message.ContentImageDataItem{Data: "aGVsbG8=", MediaType: "image/png"},
				message.ContentFileDataItem{Data: "cGRm", MediaType: "application/pdf", Filename: "sample.pdf"},
			},
		})

		blocks, ok := result.([]any)
		if !ok {
			t.Fatalf("expected []any, got %T", result)
		}
		if len(blocks) != 3 {
			t.Fatalf("expected 3 blocks, got %d", len(blocks))
		}

		textBlock := blocks[0].(map[string]any)
		if textBlock["type"] != "text" {
			t.Errorf("expected first block type text, got %v", textBlock["type"])
		}

		imageBlock := blocks[1].(map[string]any)
		if imageBlock["type"] != "image" {
			t.Errorf("expected second block type image, got %v", imageBlock["type"])
		}
		source := imageBlock["source"].(map[string]any)
		if source["media_type"] != "image/png" {
			t.Errorf("expected image media_type image/png, got %v", source["media_type"])
		}

		docBlock := blocks[2].(map[string]any)
		if docBlock["type"] != "document" {
			t.Errorf("expected third block type document, got %v", docBlock["type"])
		}
		docSource := docBlock["source"].(map[string]any)
		if docSource["media_type"] != "application/pdf" {
			t.Errorf("expected document media_type application/pdf, got %v", docSource["media_type"])
		}
	})
}

func TestParseSSEStream(t *testing.T) {
	t.Run("text response", func(t *testing.T) {
		sse := buildSSE(
			"message_start", `{"type":"message_start","message":{"id":"msg_01","model":"claude-3-5-sonnet-20241022","usage":{"input_tokens":10,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`,
			"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello "}}`,
			"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"world!"}}`,
			"content_block_stop", `{"type":"content_block_stop","index":0}`,
			"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
			"message_stop", `{"type":"message_stop"}`,
		)

		chunks := collectChunks(t, sse)
		assertChunkTypes(t, chunks,
			"stream-start",
			"response-metadata",
			"text-start",
			"text-delta",
			"text-delta",
			"text-end",
			"finish",
		)

		if d := chunks[3].(message.TextDeltaChunk); d.Delta != "Hello " {
			t.Errorf("expected delta 'Hello ', got %q", d.Delta)
		}
		if d := chunks[4].(message.TextDeltaChunk); d.Delta != "world!" {
			t.Errorf("expected delta 'world!', got %q", d.Delta)
		}

		f := chunks[6].(message.FinishChunk)
		if f.FinishReason.Unified != "stop" {
			t.Errorf("expected finish reason 'stop', got %q", f.FinishReason.Unified)
		}
		if f.FinishReason.Raw != "end_turn" {
			t.Errorf("expected raw finish reason 'end_turn', got %q", f.FinishReason.Raw)
		}
		if f.Usage.InputTokens.Total != 10 {
			t.Errorf("expected 10 input tokens, got %d", f.Usage.InputTokens.Total)
		}
		if f.Usage.OutputTokens.Total != 5 {
			t.Errorf("expected 5 output tokens, got %d", f.Usage.OutputTokens.Total)
		}
	})

	t.Run("tool call response", func(t *testing.T) {
		sse := buildSSE(
			"message_start", `{"type":"message_start","message":{"id":"msg_02","model":"claude-3-5-sonnet-20241022","usage":{"input_tokens":20,"cache_creation_input_tokens":0,"cache_read_input_tokens":5}}}`,
			"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_01abc","name":"get_weather","input":{}}}`,
			"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"loc"}}`,
			"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"ation\":\"Paris\"}"}}`,
			"content_block_stop", `{"type":"content_block_stop","index":0}`,
			"message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":15}}`,
			"message_stop", `{"type":"message_stop"}`,
		)

		chunks := collectChunks(t, sse)
		assertChunkTypes(t, chunks,
			"stream-start",
			"response-metadata",
			"tool-input-start",
			"tool-input-delta",
			"tool-input-delta",
			"tool-input-end",
			"finish",
		)

		ts := chunks[2].(message.ToolInputStartChunk)
		if ts.ToolCallID != "toolu_01abc" {
			t.Errorf("expected ToolCallID 'toolu_01abc', got %q", ts.ToolCallID)
		}
		if ts.ToolName != "get_weather" {
			t.Errorf("expected ToolName 'get_weather', got %q", ts.ToolName)
		}

		f := chunks[6].(message.FinishChunk)
		if f.FinishReason.Unified != "tool-calls" {
			t.Errorf("expected finish reason 'tool-calls', got %q", f.FinishReason.Unified)
		}
		if f.Usage.InputTokens.CacheRead != 5 {
			t.Errorf("expected 5 cache read tokens, got %d", f.Usage.InputTokens.CacheRead)
		}
		if f.Usage.InputTokens.NoCache != 15 {
			t.Errorf("expected 15 no-cache input tokens, got %d", f.Usage.InputTokens.NoCache)
		}
	})

	t.Run("thinking response", func(t *testing.T) {
		sse := buildSSE(
			"message_start", `{"type":"message_start","message":{"id":"msg_03","model":"claude-3-7-sonnet-20250219","usage":{"input_tokens":50,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`,
			"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
			"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me think..."}}`,
			"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"ErUkAbc123"}}`,
			"content_block_stop", `{"type":"content_block_stop","index":0}`,
			"content_block_start", `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
			"content_block_delta", `{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"The answer is 42."}}`,
			"content_block_stop", `{"type":"content_block_stop","index":1}`,
			"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":100}}`,
			"message_stop", `{"type":"message_stop"}`,
		)

		chunks := collectChunks(t, sse)
		assertChunkTypes(t, chunks,
			"stream-start",
			"response-metadata",
			"reasoning-start",
			"reasoning-delta",
			"reasoning-end",
			"text-start",
			"text-delta",
			"text-end",
			"finish",
		)

		rd := chunks[3].(message.ReasoningDeltaChunk)
		if rd.Delta != "Let me think..." {
			t.Errorf("expected reasoning delta 'Let me think...', got %q", rd.Delta)
		}

		// Verify signature is captured in ProviderMetadata in the nested format.
		re := chunks[4].(message.ReasoningEndChunk)
		if len(re.ProviderMetadata) == 0 {
			t.Fatal("expected ProviderMetadata on ReasoningEndChunk")
		}
		// Outer map must be {"anthropic": {...}} (nested Vercel AI SDK v6 format).
		var outerMap map[string]map[string]any
		if err := json.Unmarshal(re.ProviderMetadata, &outerMap); err != nil {
			t.Fatalf("expected nested JSON ProviderMetadata: %v", err)
		}
		thinkBlock, ok := outerMap["anthropic"]
		if !ok {
			t.Fatalf("expected 'anthropic' key in ProviderMetadata, got keys: %v", func() []string {
				keys := make([]string, 0, len(outerMap))
				for k := range outerMap {
					keys = append(keys, k)
				}
				return keys
			}())
		}
		if thinkBlock["type"] != "thinking" {
			t.Errorf("expected ProviderMetadata type 'thinking', got %v", thinkBlock["type"])
		}
		if thinkBlock["signature"] != "ErUkAbc123" {
			t.Errorf("expected signature 'ErUkAbc123' in ProviderMetadata, got %v", thinkBlock["signature"])
		}
		if thinkBlock["thinking"] != "Let me think..." {
			t.Errorf("expected thinking text in ProviderMetadata, got %v", thinkBlock["thinking"])
		}
	})

	t.Run("max_tokens stop reason maps to length", func(t *testing.T) {
		sse := buildSSE(
			"message_start", `{"type":"message_start","message":{"id":"msg_04","model":"claude-3-5-sonnet-20241022","usage":{"input_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`,
			"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial..."}}`,
			"content_block_stop", `{"type":"content_block_stop","index":0}`,
			"message_delta", `{"type":"message_delta","delta":{"stop_reason":"max_tokens"},"usage":{"output_tokens":4096}}`,
			"message_stop", `{"type":"message_stop"}`,
		)

		chunks := collectChunks(t, sse)
		var finish *message.FinishChunk
		for _, c := range chunks {
			if f, ok := c.(message.FinishChunk); ok {
				finish = &f
			}
		}
		if finish == nil {
			t.Fatal("expected FinishChunk")
		}
		if finish.FinishReason.Unified != "length" {
			t.Errorf("expected finish reason 'length', got %q", finish.FinishReason.Unified)
		}
	})

	t.Run("cache write tokens", func(t *testing.T) {
		sse := buildSSE(
			"message_start", `{"type":"message_start","message":{"id":"msg_05","model":"claude-3-5-sonnet-20241022","usage":{"input_tokens":100,"cache_creation_input_tokens":80,"cache_read_input_tokens":0}}}`,
			"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			"content_block_stop", `{"type":"content_block_stop","index":0}`,
			"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":10}}`,
			"message_stop", `{"type":"message_stop"}`,
		)

		chunks := collectChunks(t, sse)
		var finish *message.FinishChunk
		for _, c := range chunks {
			if f, ok := c.(message.FinishChunk); ok {
				finish = &f
			}
		}
		if finish == nil {
			t.Fatal("expected FinishChunk")
		}
		if finish.Usage.InputTokens.CacheWrite != 80 {
			t.Errorf("expected CacheWrite 80, got %d", finish.Usage.InputTokens.CacheWrite)
		}
		if finish.Usage.InputTokens.NoCache != 20 {
			t.Errorf("expected NoCache 20, got %d", finish.Usage.InputTokens.NoCache)
		}
	})

	t.Run("error event", func(t *testing.T) {
		sse := buildSSE(
			"error", `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`,
		)

		var gotErr error
		parseSSEStream(strings.NewReader(sse), func(_ message.ProviderMessageChunk, err error) bool {
			if err != nil {
				gotErr = err
				return false
			}
			return true
		})
		if gotErr == nil {
			t.Fatal("expected error from stream")
		}
		if !strings.Contains(gotErr.Error(), "Overloaded") {
			t.Errorf("error should contain 'Overloaded', got: %v", gotErr)
		}
	})

	t.Run("ping and message_stop are ignored", func(t *testing.T) {
		sse := buildSSE(
			"message_start", `{"type":"message_start","message":{"id":"msg_06","model":"claude-3-5-sonnet-20241022","usage":{"input_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`,
			"ping", `{"type":"ping"}`,
			"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			"content_block_stop", `{"type":"content_block_stop","index":0}`,
			"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
			"message_stop", `{"type":"message_stop"}`,
		)

		chunks := collectChunks(t, sse)
		assertChunkTypes(t, chunks,
			"stream-start",
			"response-metadata",
			"text-start",
			"text-end",
			"finish",
		)
	})
}

func TestComplete(t *testing.T) {
	t.Run("streams text response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.URL.Path != "/messages" {
				t.Errorf("expected /messages, got %s", r.URL.Path)
			}
			if r.Header.Get("x-api-key") != "test-key" {
				t.Errorf("expected x-api-key 'test-key', got %s", r.Header.Get("x-api-key"))
			}
			if r.Header.Get("anthropic-version") != apiVersion {
				t.Errorf("expected anthropic-version %q, got %s", apiVersion, r.Header.Get("anthropic-version"))
			}

			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["model"] != "claude-3-5-sonnet-20241022" {
				t.Errorf("expected model, got %v", body["model"])
			}
			if body["stream"] != true {
				t.Errorf("expected stream true, got %v", body["stream"])
			}

			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			fmt.Fprint(w, buildSSE(
				"message_start", `{"type":"message_start","message":{"id":"msg_01","model":"claude-3-5-sonnet-20241022","usage":{"input_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`,
				"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi!"}}`,
				"content_block_stop", `{"type":"content_block_stop","index":0}`,
				"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
				"message_stop", `{"type":"message_stop"}`,
			))
		}))
		defer server.Close()

		p := &Provider{apiKey: "test-key", baseURL: server.URL, client: server.Client()}
		req := providers.CompleteRequest{
			Model: providers.ModelRef{ProviderID: "anthropic", ModelID: "claude-3-5-sonnet-20241022"},
			Messages: []message.Message{
				{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hi"}}},
			},
		}

		var chunks []message.ProviderMessageChunk
		for chunk, err := range p.Complete(context.Background(), req) {
			if err != nil {
				t.Fatal(err)
			}
			chunks = append(chunks, chunk)
		}
		if len(chunks) == 0 {
			t.Fatal("expected chunks from Complete")
		}

		var finish *message.FinishChunk
		for _, c := range chunks {
			if f, ok := c.(message.FinishChunk); ok {
				finish = &f
			}
		}
		if finish == nil {
			t.Fatal("expected FinishChunk")
		}
		if finish.FinishReason.Unified != "stop" {
			t.Errorf("expected stop, got %q", finish.FinishReason.Unified)
		}
	})

	t.Run("sends system prompt and optional parameters", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)

			if body["system"] != "Be helpful" {
				t.Errorf("expected system 'Be helpful', got %v", body["system"])
			}
			if body["max_tokens"] != float64(100) {
				t.Errorf("expected max_tokens 100, got %v", body["max_tokens"])
			}
			if body["temperature"] != 0.5 {
				t.Errorf("expected temperature 0.5, got %v", body["temperature"])
			}
			if _, ok := body["tools"]; !ok {
				t.Error("expected tools in request body")
			}

			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, buildSSE(
				"message_start", `{"type":"message_start","message":{"id":"m","model":"claude-3-5-sonnet-20241022","usage":{"input_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`,
				"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
				"message_stop", `{"type":"message_stop"}`,
			))
		}))
		defer server.Close()

		p := &Provider{apiKey: "k", baseURL: server.URL, client: server.Client()}
		maxTokens := 100
		temp := 0.5
		req := providers.CompleteRequest{
			Model: providers.ModelRef{ProviderID: "anthropic", ModelID: "claude-3-5-sonnet-20241022"},
			Messages: []message.Message{
				{Role: "system", Parts: []message.Part{message.TextPart{Text: "Be helpful"}}},
				{Role: "user", Parts: []message.Part{message.TextPart{Text: "x"}}},
			},
			Tools: []providers.ToolDefinition{
				{Name: "fn", InputSchema: json.RawMessage(`{}`)},
			},
			MaxTokens:   &maxTokens,
			Temperature: &temp,
		}

		for _, err := range p.Complete(context.Background(), req) {
			if err != nil {
				t.Fatal(err)
			}
		}
	})

	t.Run("sends thinking config when reasoning enabled", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("anthropic-beta") != thinkingBetaHeader {
				t.Errorf("expected anthropic-beta %q, got %s", thinkingBetaHeader, r.Header.Get("anthropic-beta"))
			}

			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)

			thinking, ok := body["thinking"].(map[string]any)
			if !ok {
				t.Fatal("expected thinking in request body")
			}
			if thinking["type"] != "enabled" {
				t.Errorf("expected thinking type 'enabled', got %v", thinking["type"])
			}
			if thinking["budget_tokens"] != float64(reasoningBudgetHigh) {
				t.Errorf("expected budget_tokens %d, got %v", reasoningBudgetHigh, thinking["budget_tokens"])
			}
			// max_tokens should be bumped to reasoningMaxTokens.
			if body["max_tokens"] != float64(reasoningMaxTokens) {
				t.Errorf("expected max_tokens %d, got %v", reasoningMaxTokens, body["max_tokens"])
			}

			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, buildSSE(
				"message_start", `{"type":"message_start","message":{"id":"m","model":"claude-3-7-sonnet-20250219","usage":{"input_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`,
				"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
				"message_stop", `{"type":"message_stop"}`,
			))
		}))
		defer server.Close()

		p := &Provider{apiKey: "k", baseURL: server.URL, client: server.Client()}
		req := providers.CompleteRequest{
			Model:     providers.ModelRef{ProviderID: "anthropic", ModelID: "claude-3-7-sonnet-20250219"},
			Messages:  []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "x"}}}},
			Reasoning: "enabled",
		}

		for _, err := range p.Complete(context.Background(), req) {
			if err != nil {
				t.Fatal(err)
			}
		}
	})

	t.Run("sends adaptive thinking config for 4.6+ models", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Adaptive thinking must NOT set the legacy interleaved-thinking beta header.
			if strings.Contains(r.Header.Get("anthropic-beta"), thinkingBetaHeader) {
				t.Errorf("adaptive thinking should not set beta header %q", thinkingBetaHeader)
			}

			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)

			thinking, ok := body["thinking"].(map[string]any)
			if !ok {
				t.Fatal("expected thinking in request body")
			}
			if thinking["type"] != "adaptive" {
				t.Errorf("expected thinking type 'adaptive', got %v", thinking["type"])
			}
			if _, hasBudget := thinking["budget_tokens"]; hasBudget {
				t.Error("adaptive thinking must not include budget_tokens")
			}
			// max_tokens should still be bumped to reasoningMaxTokens.
			if body["max_tokens"] != float64(reasoningMaxTokens) {
				t.Errorf("expected max_tokens %d, got %v", reasoningMaxTokens, body["max_tokens"])
			}

			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, buildSSE(
				"message_start", `{"type":"message_start","message":{"id":"m","model":"claude-sonnet-4-6","usage":{"input_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`,
				"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
				"message_stop", `{"type":"message_stop"}`,
			))
		}))
		defer server.Close()

		p := &Provider{apiKey: "k", baseURL: server.URL, client: server.Client()}
		req := providers.CompleteRequest{
			Model:     providers.ModelRef{ProviderID: "anthropic", ModelID: "claude-sonnet-4-6"},
			Messages:  []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "x"}}}},
			Reasoning: "enabled",
		}

		for _, err := range p.Complete(context.Background(), req) {
			if err != nil {
				t.Fatal(err)
			}
		}
	})

	t.Run("uses Authorization: Bearer when auth token is set", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer oauth-token-xyz" {
				t.Errorf("expected Authorization 'Bearer oauth-token-xyz', got %q", r.Header.Get("Authorization"))
			}
			if r.Header.Get("x-api-key") != "" {
				t.Errorf("expected no x-api-key header when using OAuth, got %q", r.Header.Get("x-api-key"))
			}
			if !strings.Contains(r.Header.Get("anthropic-beta"), oauthBetaHeader) {
				t.Errorf("expected anthropic-beta to contain %q, got %q", oauthBetaHeader, r.Header.Get("anthropic-beta"))
			}

			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, buildSSE(
				"message_start", `{"type":"message_start","message":{"id":"m","model":"claude-3-5-sonnet-20241022","usage":{"input_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`,
				"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
				"message_stop", `{"type":"message_stop"}`,
			))
		}))
		defer server.Close()

		p := &Provider{authToken: "oauth-token-xyz", baseURL: server.URL, client: server.Client()}
		req := providers.CompleteRequest{
			Model:    providers.ModelRef{ProviderID: "anthropic", ModelID: "claude-3-5-sonnet-20241022"},
			Messages: []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hi"}}}},
		}
		for _, err := range p.Complete(context.Background(), req) {
			if err != nil {
				t.Fatal(err)
			}
		}
	})

	t.Run("handles API error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(429)
			fmt.Fprint(w, `{"type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`)
		}))
		defer server.Close()

		p := &Provider{apiKey: "k", baseURL: server.URL, client: server.Client()}
		req := providers.CompleteRequest{
			Model:    providers.ModelRef{ProviderID: "anthropic", ModelID: "claude-3-5-sonnet-20241022"},
			Messages: []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hi"}}}},
		}

		var gotErr error
		for _, err := range p.Complete(context.Background(), req) {
			if err != nil {
				gotErr = err
			}
		}
		if gotErr == nil {
			t.Fatal("expected error for 429 response")
		}
		if !strings.Contains(gotErr.Error(), "429") {
			t.Errorf("error should contain status code, got: %v", gotErr)
		}
	})
}

func TestComplete_AutoReasoning(t *testing.T) {
	minimalSSE := buildSSE(
		"message_start", `{"type":"message_start","message":{"id":"m","model":"x","usage":{"input_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}`,
		"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
		"message_stop", `{"type":"message_stop"}`,
	)

	t.Run("auto-enables thinking for reasoning-capable model", func(t *testing.T) {
		var gotBeta string
		var gotBody map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotBeta = r.Header.Get("anthropic-beta")
			json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, minimalSSE)
		}))
		defer server.Close()

		p := &Provider{apiKey: "k", baseURL: server.URL, client: server.Client()}
		req := providers.CompleteRequest{
			// claude-sonnet-4-5 has Reasoning=true in modelsdev and still uses
			// the legacy enabled+budget thinking API.
			Model:    providers.ModelRef{ProviderID: "anthropic", ModelID: "claude-sonnet-4-5"},
			Messages: []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "x"}}}},
			// Reasoning intentionally unset — should be auto-detected
		}
		for _, err := range p.Complete(context.Background(), req) {
			if err != nil {
				t.Fatal(err)
			}
		}

		if gotBeta != thinkingBetaHeader {
			t.Errorf("expected anthropic-beta %q, got %q", thinkingBetaHeader, gotBeta)
		}
		thinking, ok := gotBody["thinking"].(map[string]any)
		if !ok {
			t.Fatal("expected thinking block in request body")
		}
		if thinking["type"] != "enabled" {
			t.Errorf("expected thinking type 'enabled', got %v", thinking["type"])
		}
	})

	t.Run("does not auto-enable thinking for non-reasoning model", func(t *testing.T) {
		var gotBeta string
		var gotBody map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotBeta = r.Header.Get("anthropic-beta")
			json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, minimalSSE)
		}))
		defer server.Close()

		p := &Provider{apiKey: "k", baseURL: server.URL, client: server.Client()}
		req := providers.CompleteRequest{
			// claude-3-5-haiku-20241022 has Reasoning=false in modelsdev
			Model:    providers.ModelRef{ProviderID: "anthropic", ModelID: "claude-3-5-haiku-20241022"},
			Messages: []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "x"}}}},
		}
		for _, err := range p.Complete(context.Background(), req) {
			if err != nil {
				t.Fatal(err)
			}
		}

		if gotBeta == thinkingBetaHeader {
			t.Errorf("expected no thinking beta header for non-reasoning model")
		}
		if _, ok := gotBody["thinking"]; ok {
			t.Error("expected no thinking block in request body for non-reasoning model")
		}
	})

	t.Run("explicit disabled overrides auto-detection", func(t *testing.T) {
		var gotBody map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, minimalSSE)
		}))
		defer server.Close()

		p := &Provider{apiKey: "k", baseURL: server.URL, client: server.Client()}
		req := providers.CompleteRequest{
			Model:     providers.ModelRef{ProviderID: "anthropic", ModelID: "claude-sonnet-4-6"},
			Messages:  []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "x"}}}},
			Reasoning: "disabled",
		}
		for _, err := range p.Complete(context.Background(), req) {
			if err != nil {
				t.Fatal(err)
			}
		}

		if _, ok := gotBody["thinking"]; ok {
			t.Error("expected no thinking block when reasoning explicitly disabled")
		}
	})

	t.Run("OAuth token + reasoning includes both beta headers", func(t *testing.T) {
		var gotBetaValues []string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotBetaValues = r.Header.Values("anthropic-beta")
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, minimalSSE)
		}))
		defer server.Close()

		// OAuth provider + reasoning-capable model: must send both oauth and
		// thinking beta headers (not clobber oauth with Set).
		// claude-sonnet-4-5 has Reasoning=true and still uses the legacy
		// type:"enabled"+beta-header path.
		p := &Provider{authToken: "oauth-tok", baseURL: server.URL, client: server.Client()}
		req := providers.CompleteRequest{
			Model:    providers.ModelRef{ProviderID: "anthropic", ModelID: "claude-sonnet-4-5"},
			Messages: []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "x"}}}},
		}
		for _, err := range p.Complete(context.Background(), req) {
			if err != nil {
				t.Fatal(err)
			}
		}

		hasOAuth := false
		hasThinking := false
		for _, v := range gotBetaValues {
			if strings.Contains(v, oauthBetaHeader) {
				hasOAuth = true
			}
			if strings.Contains(v, thinkingBetaHeader) {
				hasThinking = true
			}
		}
		if !hasOAuth {
			t.Errorf("expected %q in anthropic-beta headers, got %v", oauthBetaHeader, gotBetaValues)
		}
		if !hasThinking {
			t.Errorf("expected %q in anthropic-beta headers, got %v", thinkingBetaHeader, gotBetaValues)
		}
	})
}

func TestListModels(t *testing.T) {
	t.Run("fetches from API and enriches with modelsdev", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/models" {
				t.Errorf("expected /models, got %s", r.URL.Path)
			}
			if r.Header.Get("x-api-key") != "test-key" {
				t.Errorf("expected x-api-key 'test-key', got %s", r.Header.Get("x-api-key"))
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":[{"id":"claude-3-5-sonnet-20241022","type":"model"},{"id":"claude-3-7-sonnet-20250219","type":"model"},{"id":"claude-custom-unknown","type":"model"}]}`)
		}))
		defer server.Close()

		p := &Provider{apiKey: "test-key", baseURL: server.URL, client: server.Client()}
		models, err := p.ListModels(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(models) != 3 {
			t.Fatalf("expected 3 models, got %d", len(models))
		}

		var sonnet *providers.ModelInfo
		var sonnet37 *providers.ModelInfo
		var unknown *providers.ModelInfo
		for i := range models {
			switch models[i].ID {
			case "claude-3-5-sonnet-20241022":
				sonnet = &models[i]
			case "claude-3-7-sonnet-20250219":
				sonnet37 = &models[i]
			case "claude-custom-unknown":
				unknown = &models[i]
			}
		}

		if sonnet == nil {
			t.Fatal("expected claude-3-5-sonnet-20241022 in results")
		}
		if sonnet.DisplayName == "claude-3-5-sonnet-20241022" {
			t.Error("expected modelsdev to provide a display name")
		}

		if sonnet37 == nil {
			t.Fatal("expected claude-3-7-sonnet-20250219 in results")
		}
		if !sonnet37.Reasoning {
			t.Error("expected claude-3-7-sonnet to have Reasoning=true")
		}

		// Unknown model: falls back to ID as display name.
		if unknown == nil {
			t.Fatal("expected claude-custom-unknown in results")
		}
		if unknown.DisplayName != "claude-custom-unknown" {
			t.Errorf("expected unknown model to use ID as display name, got %q", unknown.DisplayName)
		}
	})

	t.Run("handles API error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(401)
			fmt.Fprint(w, `{"type":"error","error":{"type":"authentication_error","message":"unauthorized"}}`)
		}))
		defer server.Close()

		p := &Provider{apiKey: "bad", baseURL: server.URL, client: server.Client()}
		_, err := p.ListModels(context.Background())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestFactoryRegistration(t *testing.T) {
	if !providers.Has("anthropic") {
		t.Fatal("expected anthropic provider to be registered via init()")
	}
	p, err := providers.New("anthropic", providers.Config{"api_key": "test"})
	if err != nil {
		t.Fatal(err)
	}
	if p.ID() != "anthropic" {
		t.Errorf("expected ID 'anthropic', got %q", p.ID())
	}
}

func TestStripDataURL(t *testing.T) {
	tests := []struct {
		data      string
		mediaType string
		wantData  string
		wantMT    string
	}{
		{"abc123", "image/png", "abc123", "image/png"},
		{"abc123", "", "abc123", "image/jpeg"},
		{"data:image/png;base64,abc123", "image/jpeg", "abc123", "image/png"},
		{"data:image/webp;base64,xyz", "", "xyz", "image/webp"},
	}
	for _, tt := range tests {
		t.Run(tt.data, func(t *testing.T) {
			gotData, gotMT := stripDataURL(tt.data, tt.mediaType)
			if gotData != tt.wantData {
				t.Errorf("expected data %q, got %q", tt.wantData, gotData)
			}
			if gotMT != tt.wantMT {
				t.Errorf("expected media type %q, got %q", tt.wantMT, gotMT)
			}
		})
	}
}

// --- Test helpers ---

func buildSSE(eventDataPairs ...string) string {
	var sb strings.Builder
	for i := 0; i < len(eventDataPairs); i += 2 {
		sb.WriteString("event: " + eventDataPairs[i] + "\n")
		sb.WriteString("data: " + eventDataPairs[i+1] + "\n\n")
	}
	return sb.String()
}

func collectChunks(t *testing.T, sse string) []message.ProviderMessageChunk {
	t.Helper()
	var chunks []message.ProviderMessageChunk
	parseSSEStream(strings.NewReader(sse), func(chunk message.ProviderMessageChunk, err error) bool {
		if err != nil {
			t.Fatal(err)
		}
		if chunk != nil {
			chunks = append(chunks, chunk)
		}
		return true
	})
	return chunks
}

func assertChunkTypes(t *testing.T, chunks []message.ProviderMessageChunk, expectedTypes ...string) {
	t.Helper()
	if len(chunks) != len(expectedTypes) {
		types := make([]string, len(chunks))
		for i, c := range chunks {
			types[i] = fmt.Sprintf("%T", c)
		}
		t.Fatalf("expected %d chunks %v, got %d: %v", len(expectedTypes), expectedTypes, len(chunks), types)
	}

	typeMap := map[string]string{
		"stream-start":      "message.StreamStartChunk",
		"response-metadata": "message.ResponseMetadataChunk",
		"text-start":        "message.TextStartChunk",
		"text-delta":        "message.TextDeltaChunk",
		"text-end":          "message.TextEndChunk",
		"tool-input-start":  "message.ToolInputStartChunk",
		"tool-input-delta":  "message.ToolInputDeltaChunk",
		"tool-input-end":    "message.ToolInputEndChunk",
		"reasoning-start":   "message.ReasoningStartChunk",
		"reasoning-delta":   "message.ReasoningDeltaChunk",
		"reasoning-end":     "message.ReasoningEndChunk",
		"finish":            "message.FinishChunk",
	}

	for i, expected := range expectedTypes {
		expectedType := typeMap[expected]
		actual := fmt.Sprintf("%T", chunks[i])
		if actual != expectedType {
			t.Errorf("chunk[%d]: expected %s (%s), got %s", i, expected, expectedType, actual)
		}
	}
}
