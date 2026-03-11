package openaicompatible

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

// --- Helpers ---

// testProv returns a Provider pointing at the given base URL for use in tests.
func testProv(baseURL string) *Provider {
	return &Provider{
		id:      "test",
		apiKey:  "test-key",
		baseURL: baseURL,
		client:  http.DefaultClient,
	}
}

// buildChatSSE builds a Chat Completions SSE body from alternating data lines
// and an optional final [DONE] sentinel. Each argument becomes one data: line.
// Pass "[DONE]" as the last argument to terminate the stream explicitly.
func buildChatSSE(dataLines ...string) string {
	var sb strings.Builder
	for _, d := range dataLines {
		sb.WriteString("data: " + d + "\n")
	}
	return sb.String()
}

// collectChunks drains the SSE stream via parseSSEStream and returns all chunks.
// The test fails immediately if any error is yielded.
func collectChunks(t *testing.T, p *Provider, sse string) []message.ProviderMessageChunk {
	t.Helper()
	var chunks []message.ProviderMessageChunk
	p.parseSSEStream(strings.NewReader(sse), func(chunk message.ProviderMessageChunk, err error) bool {
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

// assertChunkTypes asserts that the collected chunks have exactly the given
// type-name sequence (using the same short names as the OpenAI provider tests).
func assertChunkTypes(t *testing.T, chunks []message.ProviderMessageChunk, expectedTypes ...string) {
	t.Helper()
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
		"error":             "message.ErrorChunk",
		"finish":            "message.FinishChunk",
	}
	if len(chunks) != len(expectedTypes) {
		types := make([]string, len(chunks))
		for i, c := range chunks {
			types[i] = fmt.Sprintf("%T", c)
		}
		t.Fatalf("expected %d chunks %v, got %d: %v", len(expectedTypes), expectedTypes, len(chunks), types)
	}
	for i, expected := range expectedTypes {
		expectedType := typeMap[expected]
		actual := fmt.Sprintf("%T", chunks[i])
		if actual != expectedType {
			t.Errorf("chunk[%d]: expected %s (%s), got %s", i, expected, expectedType, actual)
		}
	}
}

// --- TestNew ---

func TestNew(t *testing.T) {
	t.Run("requires api key", func(t *testing.T) {
		_, err := newProvider("test", "https://api.example.com", providers.Config{})
		if err == nil {
			t.Fatal("expected error for missing api key")
		}
	})

	t.Run("uses default base url when not provided", func(t *testing.T) {
		p, err := newProvider("test", "https://api.example.com", providers.Config{"api_key": "k"})
		if err != nil {
			t.Fatal(err)
		}
		if p.baseURL != "https://api.example.com" {
			t.Errorf("expected default base URL %q, got %q", "https://api.example.com", p.baseURL)
		}
	})

	t.Run("uses custom base url and strips trailing slash", func(t *testing.T) {
		p, err := newProvider("test", "https://api.example.com", providers.Config{
			"api_key":  "k",
			"base_url": "https://custom.api.com/v1/",
		})
		if err != nil {
			t.Fatal(err)
		}
		if p.baseURL != "https://custom.api.com/v1" {
			t.Errorf("expected trimmed URL, got %q", p.baseURL)
		}
	})
}

func TestProviderID(t *testing.T) {
	p, _ := newProvider("deepseek", "https://api.deepseek.com", providers.Config{"api_key": "k"})
	if p.ID() != "deepseek" {
		t.Errorf("expected ID 'deepseek', got %q", p.ID())
	}
}

// --- TestConvertMessages ---

func TestConvertMessages(t *testing.T) {
	t.Run("system message keeps system role", func(t *testing.T) {
		// Chat Completions uses "system" — NOT "developer" like the Responses API.
		msgs := []message.Message{
			{Role: "system", Parts: []message.Part{message.TextPart{Text: "You are helpful"}}},
		}
		items, err := convertMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 {
			t.Fatalf("expected 1 message, got %d", len(items))
		}
		if items[0]["role"] != "system" {
			t.Errorf("expected role 'system', got %q", items[0]["role"])
		}
		if items[0]["content"] != "You are helpful" {
			t.Errorf("expected content 'You are helpful', got %q", items[0]["content"])
		}
	})

	t.Run("user message single text uses string shorthand", func(t *testing.T) {
		msgs := []message.Message{
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hello"}}},
		}
		items, err := convertMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		if items[0]["role"] != "user" {
			t.Errorf("expected role 'user', got %q", items[0]["role"])
		}
		// Single text uses string shorthand, not an array.
		if items[0]["content"] != "Hello" {
			t.Errorf("expected content string 'Hello', got %v", items[0]["content"])
		}
	})

	t.Run("user message with image uses array format", func(t *testing.T) {
		msgs := []message.Message{
			{Role: "user", Parts: []message.Part{
				message.TextPart{Text: "Describe this"},
				message.ImagePart{Image: "https://example.com/img.jpg"},
			}},
		}
		items, err := convertMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		content, ok := items[0]["content"].([]any)
		if !ok {
			t.Fatalf("expected content array, got %T", items[0]["content"])
		}
		if len(content) != 2 {
			t.Fatalf("expected 2 content parts, got %d", len(content))
		}
		imgPart := content[1].(map[string]any)
		if imgPart["type"] != "image_url" {
			t.Errorf("expected type 'image_url' (Chat Completions format), got %q", imgPart["type"])
		}
		imgURL := imgPart["image_url"].(map[string]any)
		if imgURL["url"] != "https://example.com/img.jpg" {
			t.Errorf("expected image URL, got %q", imgURL["url"])
		}
	})

	t.Run("user message with base64 image builds data URL", func(t *testing.T) {
		msgs := []message.Message{
			{Role: "user", Parts: []message.Part{
				message.ImagePart{Image: "abc123base64", MediaType: "image/png"},
			}},
		}
		items, err := convertMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		content := items[0]["content"].([]any)
		imgPart := content[0].(map[string]any)
		imgURL := imgPart["image_url"].(map[string]any)
		expected := "data:image/png;base64,abc123base64"
		if imgURL["url"] != expected {
			t.Errorf("expected %q, got %q", expected, imgURL["url"])
		}
	})

	t.Run("assistant message with text and tool calls", func(t *testing.T) {
		// Chat Completions: text in content, tool calls in tool_calls array,
		// each call nested under a "function" key.
		msgs := []message.Message{
			{Role: "assistant", Parts: []message.Part{
				message.TextPart{Text: "Let me check"},
				message.ToolCallPart{
					ToolCallID: "call_123",
					ToolName:   "get_weather",
					Input:      `{"location":"Paris"}`,
				},
			}},
		}
		items, err := convertMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		// Chat Completions: single assistant message (not split into separate items).
		if len(items) != 1 {
			t.Fatalf("expected 1 message, got %d", len(items))
		}
		m := items[0]
		if m["role"] != "assistant" {
			t.Errorf("expected role 'assistant', got %q", m["role"])
		}
		if m["content"] != "Let me check" {
			t.Errorf("expected content 'Let me check', got %q", m["content"])
		}
		toolCalls, ok := m["tool_calls"].([]map[string]any)
		if !ok || len(toolCalls) != 1 {
			t.Fatalf("expected 1 tool_call, got %v", m["tool_calls"])
		}
		tc := toolCalls[0]
		if tc["id"] != "call_123" {
			t.Errorf("expected id 'call_123', got %q", tc["id"])
		}
		if tc["type"] != "function" {
			t.Errorf("expected type 'function', got %q", tc["type"])
		}
		// Tool name is nested under "function" key (not at top level).
		fn, ok := tc["function"].(map[string]any)
		if !ok {
			t.Fatalf("expected 'function' key in tool_call, got %v", tc["function"])
		}
		if fn["name"] != "get_weather" {
			t.Errorf("expected function name 'get_weather', got %q", fn["name"])
		}
		if fn["arguments"] != `{"location":"Paris"}` {
			t.Errorf("unexpected arguments: %q", fn["arguments"])
		}
	})

	t.Run("assistant message with only tool calls has no text", func(t *testing.T) {
		msgs := []message.Message{
			{Role: "assistant", Parts: []message.Part{
				message.ToolCallPart{
					ToolCallID: "call_1",
					ToolName:   "fn",
					Input:      `{}`,
				},
			}},
		}
		items, err := convertMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 {
			t.Fatalf("expected 1 message, got %d", len(items))
		}
		m := items[0]
		// Content is empty string when there is no text.
		if m["content"] != "" {
			t.Errorf("expected empty content, got %q", m["content"])
		}
		toolCalls, ok := m["tool_calls"].([]map[string]any)
		if !ok || len(toolCalls) != 1 {
			t.Fatalf("expected 1 tool_call in message")
		}
	})

	t.Run("assistant message with reasoning part", func(t *testing.T) {
		// Chat Completions: reasoning is sent as reasoning_content field
		// (not as a separate opaque item like in the Responses API).
		msgs := []message.Message{
			{Role: "assistant", Parts: []message.Part{
				message.ReasoningPart{
					ID:   "rs_1",
					Text: "Let me think...",
				},
				message.TextPart{Text: "The answer is 42."},
			}},
		}
		items, err := convertMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 {
			t.Fatalf("expected 1 message, got %d", len(items))
		}
		m := items[0]
		if m["reasoning_content"] != "Let me think..." {
			t.Errorf("expected reasoning_content 'Let me think...', got %q", m["reasoning_content"])
		}
		if m["content"] != "The answer is 42." {
			t.Errorf("expected content 'The answer is 42.', got %q", m["content"])
		}
	})

	t.Run("assistant message with reasoning part and provider metadata uses text not metadata", func(t *testing.T) {
		// Unlike the OpenAI Responses API (which round-trips the encrypted opaque
		// token) or Anthropic (which uses a thinking signature), Chat Completions
		// has no opaque token concept. ProviderMetadata is ignored and the plain
		// text is always sent as reasoning_content.
		providerMeta := json.RawMessage(`{"openai":{"encrypted_content":"gAAAA_enc"}}`)
		msgs := []message.Message{
			{Role: "assistant", Parts: []message.Part{
				message.ReasoningPart{
					ID:               "rs_1",
					Text:             "Thinking about it...",
					ProviderMetadata: providerMeta,
				},
				message.TextPart{Text: "Done."},
			}},
		}
		items, err := convertMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		// Always 1 message (no separate opaque reasoning item).
		if len(items) != 1 {
			t.Fatalf("expected 1 message, got %d", len(items))
		}
		m := items[0]
		if m["reasoning_content"] != "Thinking about it..." {
			t.Errorf("expected reasoning_content text, got %q", m["reasoning_content"])
		}
		if m["content"] != "Done." {
			t.Errorf("expected content 'Done.', got %q", m["content"])
		}
		// ProviderMetadata must NOT be forwarded as a separate field.
		if _, hasEnc := m["encrypted_content"]; hasEnc {
			t.Error("expected no encrypted_content in Chat Completions message")
		}
	})

	t.Run("assistant message with reasoning and tool calls", func(t *testing.T) {
		msgs := []message.Message{
			{Role: "assistant", Parts: []message.Part{
				message.ReasoningPart{ID: "rs_1", Text: "Thinking..."},
				message.ToolCallPart{ToolCallID: "call_1", ToolName: "fn", Input: `{}`},
			}},
		}
		items, err := convertMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 {
			t.Fatalf("expected 1 message, got %d", len(items))
		}
		m := items[0]
		if m["reasoning_content"] != "Thinking..." {
			t.Errorf("expected reasoning_content, got %q", m["reasoning_content"])
		}
		if _, hasTC := m["tool_calls"]; !hasTC {
			t.Error("expected tool_calls in assistant message")
		}
	})

	t.Run("tool result message produces role=tool messages", func(t *testing.T) {
		// Chat Completions: tool results are role "tool" messages (not "function_call_output").
		msgs := []message.Message{
			{Role: "tool", Parts: []message.Part{
				message.ToolResultPart{
					ToolCallID: "call_123",
					ToolName:   "get_weather",
					Output:     message.TextOutput{Value: "25C sunny"},
				},
			}},
		}
		items, err := convertMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 {
			t.Fatalf("expected 1 message, got %d", len(items))
		}
		m := items[0]
		if m["role"] != "tool" {
			t.Errorf("expected role 'tool', got %q", m["role"])
		}
		if m["tool_call_id"] != "call_123" {
			t.Errorf("expected tool_call_id 'call_123', got %q", m["tool_call_id"])
		}
		if m["content"] != "25C sunny" {
			t.Errorf("expected content '25C sunny', got %q", m["content"])
		}
	})

	t.Run("multiple tool results produce separate messages", func(t *testing.T) {
		msgs := []message.Message{
			{Role: "tool", Parts: []message.Part{
				message.ToolResultPart{ToolCallID: "call_1", Output: message.TextOutput{Value: "a"}},
				message.ToolResultPart{ToolCallID: "call_2", Output: message.TextOutput{Value: "b"}},
			}},
		}
		items, err := convertMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		// One "tool" message per result (Chat Completions requirement).
		if len(items) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(items))
		}
		if items[0]["tool_call_id"] != "call_1" {
			t.Errorf("expected call_1, got %q", items[0]["tool_call_id"])
		}
		if items[1]["tool_call_id"] != "call_2" {
			t.Errorf("expected call_2, got %q", items[1]["tool_call_id"])
		}
	})
}

// --- TestConvertTools ---

func TestConvertTools(t *testing.T) {
	t.Run("maps to Chat Completions function format", func(t *testing.T) {
		// In Chat Completions, name/description/parameters are nested under "function".
		// This is different from the Responses API where they are at the top level.
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
		if result[0]["type"] != "function" {
			t.Errorf("expected type 'function', got %q", result[0]["type"])
		}
		// Chat Completions: name is nested under "function", NOT at top level.
		if _, hasTopName := result[0]["name"]; hasTopName {
			t.Error("name should NOT be at top level in Chat Completions format")
		}
		fn, ok := result[0]["function"].(map[string]any)
		if !ok {
			t.Fatalf("expected 'function' key, got %v", result[0]["function"])
		}
		if fn["name"] != "get_weather" {
			t.Errorf("expected function name 'get_weather', got %q", fn["name"])
		}
		if fn["description"] != "Get current weather" {
			t.Errorf("expected description, got %q", fn["description"])
		}
	})

	t.Run("omits empty description", func(t *testing.T) {
		tools := []providers.ToolDefinition{
			{Name: "fn", InputSchema: json.RawMessage(`{}`)},
		}
		result := convertTools(tools)
		fn := result[0]["function"].(map[string]any)
		if _, ok := fn["description"]; ok {
			t.Error("expected description to be omitted when empty")
		}
	})

	t.Run("nil tools returns nil", func(t *testing.T) {
		if result := convertTools(nil); result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})
}

// --- TestToolResultToString ---

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
		{"content output with media placeholders", message.ContentOutput{
			Value: []message.ToolResultContentItem{
				message.ContentTextItem{Text: "summary"},
				message.ContentImageDataItem{Data: "aGVsbG8=", MediaType: "image/png"},
				message.ContentFileDataItem{Data: "cGRm", MediaType: "application/pdf", Filename: "sample.pdf"},
			},
		}, "summary\n[image data omitted (image/png)]\n[file data omitted (application/pdf, filename=sample.pdf)]"},
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

// --- TestParseSSEStream ---

func TestParseSSEStream(t *testing.T) {
	p := testProv("http://localhost")

	t.Run("text response", func(t *testing.T) {
		sse := buildChatSSE(
			// First chunk: role announcement, empty content.
			`{"id":"chatcmpl-1","model":"gpt-4","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
			// Content deltas.
			`{"id":"chatcmpl-1","model":"gpt-4","choices":[{"index":0,"delta":{"content":"Hello "},"finish_reason":null}]}`,
			`{"id":"chatcmpl-1","model":"gpt-4","choices":[{"index":0,"delta":{"content":"world!"},"finish_reason":null}]}`,
			// Finish with usage.
			`{"id":"chatcmpl-1","model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}}`,
			"[DONE]",
		)

		chunks := collectChunks(t, p, sse)
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
		if f.FinishReason.Raw != "stop" {
			t.Errorf("expected raw finish reason 'stop', got %q", f.FinishReason.Raw)
		}
		if f.Usage.InputTokens.Total != 10 {
			t.Errorf("expected 10 input tokens, got %d", f.Usage.InputTokens.Total)
		}
		if f.Usage.OutputTokens.Total != 5 {
			t.Errorf("expected 5 output tokens, got %d", f.Usage.OutputTokens.Total)
		}
	})

	t.Run("tool call response", func(t *testing.T) {
		sse := buildChatSSE(
			// First tool call chunk with id + name.
			`{"id":"chatcmpl-2","model":"gpt-4","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}`,
			// Arguments deltas.
			`{"id":"chatcmpl-2","model":"gpt-4","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"loc"}}]},"finish_reason":null}]}`,
			`{"id":"chatcmpl-2","model":"gpt-4","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ation\":\"Paris\"}"}}]},"finish_reason":null}]}`,
			// Finish with cache usage.
			`{"id":"chatcmpl-2","model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":20,"completion_tokens":15,"prompt_tokens_details":{"cached_tokens":5},"completion_tokens_details":{"reasoning_tokens":0}}}`,
			"[DONE]",
		)

		chunks := collectChunks(t, p, sse)
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
		if ts.ToolCallID != "call_1" {
			t.Errorf("expected ToolCallID 'call_1', got %q", ts.ToolCallID)
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
			t.Errorf("expected 15 no-cache tokens, got %d", f.Usage.InputTokens.NoCache)
		}
	})

	t.Run("reasoning response via reasoning_content", func(t *testing.T) {
		// Many reasoning-capable providers (DeepSeek, etc.) send reasoning
		// in delta.reasoning_content before the main content.
		sse := buildChatSSE(
			`{"id":"chatcmpl-3","model":"deepseek-reasoner","choices":[{"index":0,"delta":{"reasoning_content":"Thinking..."},"finish_reason":null}]}`,
			// Content starts — reasoning should close.
			`{"id":"chatcmpl-3","model":"deepseek-reasoner","choices":[{"index":0,"delta":{"content":"The answer is 42."},"finish_reason":null}]}`,
			`{"id":"chatcmpl-3","model":"deepseek-reasoner","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":50,"completion_tokens":100,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":80}}}`,
			"[DONE]",
		)

		chunks := collectChunks(t, p, sse)
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
		if rd.Delta != "Thinking..." {
			t.Errorf("expected reasoning delta 'Thinking...', got %q", rd.Delta)
		}

		f := chunks[8].(message.FinishChunk)
		if f.Usage.OutputTokens.Reasoning != 80 {
			t.Errorf("expected 80 reasoning tokens, got %d", f.Usage.OutputTokens.Reasoning)
		}
		if f.Usage.OutputTokens.Text != 20 {
			t.Errorf("expected 20 text tokens, got %d", f.Usage.OutputTokens.Text)
		}
	})

	t.Run("reasoning response via reasoning field", func(t *testing.T) {
		// Some providers use delta.reasoning instead of delta.reasoning_content.
		sse := buildChatSSE(
			`{"id":"chatcmpl-4","model":"some-model","choices":[{"index":0,"delta":{"reasoning":"Step 1..."},"finish_reason":null}]}`,
			`{"id":"chatcmpl-4","model":"some-model","choices":[{"index":0,"delta":{"content":"Done."},"finish_reason":null}]}`,
			`{"id":"chatcmpl-4","model":"some-model","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}}`,
			"[DONE]",
		)

		chunks := collectChunks(t, p, sse)
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
		if rd.Delta != "Step 1..." {
			t.Errorf("expected reasoning delta 'Step 1...', got %q", rd.Delta)
		}
	})

	t.Run("length finish reason", func(t *testing.T) {
		sse := buildChatSSE(
			`{"id":"chatcmpl-5","model":"gpt-4","choices":[{"index":0,"delta":{"content":"partial"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-5","model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"length"}],"usage":{"prompt_tokens":100,"completion_tokens":4096,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}}`,
			"[DONE]",
		)

		chunks := collectChunks(t, p, sse)
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

	t.Run("stream ends without DONE emits FinishChunk", func(t *testing.T) {
		// Some providers omit [DONE]; the implementation should still emit FinishChunk.
		sse := buildChatSSE(
			`{"id":"chatcmpl-6","model":"gpt-4","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-6","model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}}`,
			// No [DONE] line.
		)

		chunks := collectChunks(t, p, sse)
		var gotFinish bool
		for _, c := range chunks {
			if _, ok := c.(message.FinishChunk); ok {
				gotFinish = true
			}
		}
		if !gotFinish {
			t.Error("expected FinishChunk even without [DONE] sentinel")
		}
	})

	t.Run("usage in separate chunk before DONE", func(t *testing.T) {
		// Some providers send usage in a separate chunk after finish_reason.
		sse := buildChatSSE(
			`{"id":"chatcmpl-7","model":"gpt-4","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-7","model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
			// Usage in a separate chunk with empty choices.
			`{"id":"chatcmpl-7","model":"gpt-4","choices":[],"usage":{"prompt_tokens":8,"completion_tokens":3,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}}`,
			"[DONE]",
		)

		chunks := collectChunks(t, p, sse)
		var finish *message.FinishChunk
		for _, c := range chunks {
			if f, ok := c.(message.FinishChunk); ok {
				finish = &f
			}
		}
		if finish == nil {
			t.Fatal("expected FinishChunk")
		}
		if finish.Usage.InputTokens.Total != 8 {
			t.Errorf("expected 8 input tokens, got %d", finish.Usage.InputTokens.Total)
		}
		if finish.Usage.OutputTokens.Total != 3 {
			t.Errorf("expected 3 output tokens, got %d", finish.Usage.OutputTokens.Total)
		}
	})

	t.Run("malformed JSON lines are skipped", func(t *testing.T) {
		sse := buildChatSSE(
			`{"id":"chatcmpl-8","model":"gpt-4","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":null}]}`,
			`not-valid-json`,
			`{"id":"chatcmpl-8","model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}}`,
			"[DONE]",
		)

		// Malformed JSON should be silently skipped without error.
		chunks := collectChunks(t, p, sse)
		var gotFinish bool
		for _, c := range chunks {
			if _, ok := c.(message.FinishChunk); ok {
				gotFinish = true
			}
		}
		if !gotFinish {
			t.Error("expected stream to complete despite malformed chunk")
		}
	})

	t.Run("non-data lines are ignored", func(t *testing.T) {
		// Lines without "data: " prefix (comments, blank lines, event: lines) are
		// skipped. This verifies robustness against non-standard server output.
		sse := "# comment\n" +
			"data: " + `{"id":"chatcmpl-9","model":"gpt-4","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}` + "\n" +
			"\n" + // blank line
			"data: " + `{"id":"chatcmpl-9","model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}}` + "\n" +
			"data: [DONE]\n"

		chunks := collectChunks(t, p, sse)
		var gotFinish bool
		for _, c := range chunks {
			if _, ok := c.(message.FinishChunk); ok {
				gotFinish = true
			}
		}
		if !gotFinish {
			t.Error("expected stream to complete with non-data lines present")
		}
	})

	t.Run("multiple concurrent tool calls", func(t *testing.T) {
		sse := buildChatSSE(
			// Tool call 0 starts.
			`{"id":"chatcmpl-10","model":"gpt-4","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"fn_a","arguments":""}}]},"finish_reason":null}]}`,
			// Tool call 1 starts.
			`{"id":"chatcmpl-10","model":"gpt-4","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"fn_b","arguments":""}}]},"finish_reason":null}]}`,
			// Arguments for tool 0.
			`{"id":"chatcmpl-10","model":"gpt-4","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{}"}}]},"finish_reason":null}]}`,
			// Arguments for tool 1.
			`{"id":"chatcmpl-10","model":"gpt-4","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{}"}}]},"finish_reason":null}]}`,
			`{"id":"chatcmpl-10","model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}}`,
			"[DONE]",
		)

		chunks := collectChunks(t, p, sse)
		assertChunkTypes(t, chunks,
			"stream-start",
			"response-metadata",
			"tool-input-start", // call_a
			"tool-input-start", // call_b
			"tool-input-delta", // call_a args
			"tool-input-delta", // call_b args
			"tool-input-end",   // call_a or call_b (ordered by index)
			"tool-input-end",
			"finish",
		)

		start0 := chunks[2].(message.ToolInputStartChunk)
		start1 := chunks[3].(message.ToolInputStartChunk)
		if start0.ToolCallID != "call_a" || start0.ToolName != "fn_a" {
			t.Errorf("expected call_a/fn_a, got %q/%q", start0.ToolCallID, start0.ToolName)
		}
		if start1.ToolCallID != "call_b" || start1.ToolName != "fn_b" {
			t.Errorf("expected call_b/fn_b, got %q/%q", start1.ToolCallID, start1.ToolName)
		}

		f := chunks[8].(message.FinishChunk)
		if f.FinishReason.Unified != "tool-calls" {
			t.Errorf("expected tool-calls, got %q", f.FinishReason.Unified)
		}
	})

	// --- Tests ported from @ai-sdk/openai-compatible ---

	t.Run("tool call name and first args arrive in same chunk", func(t *testing.T) {
		// Some providers (e.g. xAI Grok) send the name and initial arguments
		// together in the first tool_call delta.
		sse := buildChatSSE(
			`{"id":"chatcmpl-1","model":"grok-3","choices":[{"index":0,"delta":{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"call_x","type":"function","function":{"name":"test-tool","arguments":"{\"v"}}]},"finish_reason":null}]}`,
			`{"id":"chatcmpl-1","model":"grok-3","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"alue\":1}"}}]},"finish_reason":null}]}`,
			`{"id":"chatcmpl-1","model":"grok-3","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":18,"completion_tokens":5}}`,
			"[DONE]",
		)

		chunks := collectChunks(t, p, sse)
		assertChunkTypes(t, chunks,
			"stream-start",
			"response-metadata",
			"tool-input-start",
			"tool-input-delta", // first partial args from first chunk
			"tool-input-delta", // remaining args from second chunk
			"tool-input-end",
			"finish",
		)

		start := chunks[2].(message.ToolInputStartChunk)
		if start.ToolCallID != "call_x" || start.ToolName != "test-tool" {
			t.Errorf("unexpected start chunk: %+v", start)
		}
		d0 := chunks[3].(message.ToolInputDeltaChunk)
		d1 := chunks[4].(message.ToolInputDeltaChunk)
		if d0.InputTextDelta+d1.InputTextDelta != `{"value":1}` {
			t.Errorf("expected combined args %q, got %q+%q", `{"value":1}`, d0.InputTextDelta, d1.InputTextDelta)
		}
	})

	t.Run("tool call sent in one chunk", func(t *testing.T) {
		// The entire tool call (name + full arguments) arrives in a single chunk.
		sse := buildChatSSE(
			`{"id":"chatcmpl-2","model":"grok-3","choices":[{"index":0,"delta":{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"call_one","type":"function","function":{"name":"test-tool","arguments":"{\"value\":\"Sparkle Day\"}"}}]},"finish_reason":null}]}`,
			`{"id":"chatcmpl-2","model":"grok-3","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":18,"completion_tokens":439}}`,
			"[DONE]",
		)

		chunks := collectChunks(t, p, sse)
		assertChunkTypes(t, chunks,
			"stream-start",
			"response-metadata",
			"tool-input-start",
			"tool-input-delta", // full args in single delta
			"tool-input-end",
			"finish",
		)

		delta := chunks[3].(message.ToolInputDeltaChunk)
		if delta.InputTextDelta != `{"value":"Sparkle Day"}` {
			t.Errorf("expected full args in single delta, got %q", delta.InputTextDelta)
		}
		f := chunks[5].(message.FinishChunk)
		if f.FinishReason.Unified != "tool-calls" {
			t.Errorf("expected tool-calls, got %q", f.FinishReason.Unified)
		}
	})

	t.Run("empty tool call sent in one chunk", func(t *testing.T) {
		// Tool call with empty arguments string — no delta should be emitted.
		sse := buildChatSSE(
			`{"id":"chatcmpl-3","model":"grok-3","choices":[{"index":0,"delta":{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"call_empty","type":"function","function":{"name":"no-args-tool","arguments":""}}]},"finish_reason":null}]}`,
			`{"id":"chatcmpl-3","model":"grok-3","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":18,"completion_tokens":5}}`,
			"[DONE]",
		)

		chunks := collectChunks(t, p, sse)
		assertChunkTypes(t, chunks,
			"stream-start",
			"response-metadata",
			"tool-input-start",
			"tool-input-end", // no delta — arguments were empty
			"finish",
		)
	})

	t.Run("no duplicate tool-input-end after empty args follow-up chunk", func(t *testing.T) {
		// Some providers send an additional empty arguments chunk with
		// finish_reason:"tool_calls" after the tool call is complete.
		// Each tool call should produce exactly one tool-input-end.
		sse := buildChatSSE(
			`{"id":"chatcmpl-4","model":"grok-3","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_dup","type":"function","function":{"name":"fn","arguments":"{\"k\":1}"}}]},"finish_reason":null}]}`,
			// Empty args follow-up chunk — should not produce another start/delta.
			`{"id":"chatcmpl-4","model":"grok-3","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":""}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
			"[DONE]",
		)

		chunks := collectChunks(t, p, sse)
		assertChunkTypes(t, chunks,
			"stream-start",
			"response-metadata",
			"tool-input-start",
			"tool-input-delta",
			"tool-input-end", // exactly one — not duplicated
			"finish",
		)
	})

	t.Run("error stream part emits ErrorChunk", func(t *testing.T) {
		// When the server streams an error object the provider should yield an
		// ErrorChunk followed by a FinishChunk (from the [DONE] sentinel).
		sse := "data: {\"error\":{\"message\":\"Incorrect API key provided.\",\"type\":\"invalid_request_error\"}}\n\ndata: [DONE]\n\n"

		chunks := collectChunks(t, p, sse)
		// stream-start is emitted before the ErrorChunk even though the error
		// was the first data line.
		assertChunkTypes(t, chunks,
			"stream-start",
			"error",
			"finish",
		)

		ec := chunks[1].(message.ErrorChunk)
		if ec.ErrorText != "Incorrect API key provided." {
			t.Errorf("unexpected error text: %q", ec.ErrorText)
		}
		if ec.Err == nil {
			t.Error("expected non-nil Err")
		}
	})

	t.Run("partial usage missing completion_tokens", func(t *testing.T) {
		// Some providers omit completion_tokens when the model was cut short.
		sse := buildChatSSE(
			`{"id":"chatcmpl-5","model":"gpt-4","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-5","model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"length"}],"usage":{"prompt_tokens":20}}`,
			"[DONE]",
		)

		chunks := collectChunks(t, p, sse)
		f := chunks[len(chunks)-1].(message.FinishChunk)
		if f.Usage.InputTokens.Total != 20 {
			t.Errorf("expected 20 input tokens, got %d", f.Usage.InputTokens.Total)
		}
		if f.Usage.OutputTokens.Total != 0 {
			t.Errorf("expected 0 output tokens when completion_tokens absent, got %d", f.Usage.OutputTokens.Total)
		}
	})

	t.Run("unknown finish reason maps to other", func(t *testing.T) {
		// Providers may use non-standard finish reasons (e.g. "eos", "max_tokens").
		sse := buildChatSSE(
			`{"id":"chatcmpl-6","model":"gpt-4","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-6","model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"eos"}],"usage":{"prompt_tokens":5,"completion_tokens":1}}`,
			"[DONE]",
		)

		chunks := collectChunks(t, p, sse)
		f := chunks[len(chunks)-1].(message.FinishChunk)
		if f.FinishReason.Raw != "eos" {
			t.Errorf("expected raw reason 'eos', got %q", f.FinishReason.Raw)
		}
		if f.FinishReason.Unified != "other" {
			t.Errorf("expected unified reason 'other', got %q", f.FinishReason.Unified)
		}
	})

	t.Run("detailed usage fields prompt_tokens_details and completion_tokens_details", func(t *testing.T) {
		// Providers that support prompt caching and reasoning expose sub-fields.
		// prompt_tokens_details.cached_tokens → InputTokens.CacheRead
		// completion_tokens_details.reasoning_tokens → OutputTokens.Reasoning
		sse := buildChatSSE(
			`{"id":"chatcmpl-7","model":"gpt-4","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-7","model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":30,"total_tokens":50,"prompt_tokens_details":{"cached_tokens":5},"completion_tokens_details":{"reasoning_tokens":10}}}`,
			"[DONE]",
		)

		chunks := collectChunks(t, p, sse)
		f := chunks[len(chunks)-1].(message.FinishChunk)

		if f.Usage.InputTokens.Total != 20 {
			t.Errorf("InputTokens.Total: want 20, got %d", f.Usage.InputTokens.Total)
		}
		if f.Usage.InputTokens.CacheRead != 5 {
			t.Errorf("InputTokens.CacheRead: want 5, got %d", f.Usage.InputTokens.CacheRead)
		}
		if f.Usage.InputTokens.NoCache != 15 {
			t.Errorf("InputTokens.NoCache: want 15, got %d", f.Usage.InputTokens.NoCache)
		}
		if f.Usage.OutputTokens.Total != 30 {
			t.Errorf("OutputTokens.Total: want 30, got %d", f.Usage.OutputTokens.Total)
		}
		if f.Usage.OutputTokens.Reasoning != 10 {
			t.Errorf("OutputTokens.Reasoning: want 10, got %d", f.Usage.OutputTokens.Reasoning)
		}
		if f.Usage.OutputTokens.Text != 20 {
			t.Errorf("OutputTokens.Text: want 20, got %d", f.Usage.OutputTokens.Text)
		}
	})

	t.Run("detailed usage with partial token details", func(t *testing.T) {
		// Handles the case where only some sub-fields are present (e.g. only
		// reasoning_tokens, no cached_tokens).
		sse := buildChatSSE(
			`{"id":"chatcmpl-8","model":"gpt-4","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`,
			`{"id":"chatcmpl-8","model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":30,"prompt_tokens_details":{"cached_tokens":5},"completion_tokens_details":{"reasoning_tokens":10}}}`,
			"[DONE]",
		)

		chunks := collectChunks(t, p, sse)
		f := chunks[len(chunks)-1].(message.FinishChunk)

		if f.Usage.InputTokens.CacheRead != 5 {
			t.Errorf("CacheRead: want 5, got %d", f.Usage.InputTokens.CacheRead)
		}
		if f.Usage.OutputTokens.Reasoning != 10 {
			t.Errorf("Reasoning: want 10, got %d", f.Usage.OutputTokens.Reasoning)
		}
	})
}

// --- TestComplete ---

func TestComplete(t *testing.T) {
	t.Run("streams text response via /chat/completions", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				t.Errorf("expected POST, got %s", r.Method)
			}
			// Chat Completions endpoint (NOT /responses).
			if r.URL.Path != "/chat/completions" {
				t.Errorf("expected /chat/completions, got %s", r.URL.Path)
			}
			if r.Header.Get("Authorization") != "Bearer test-key" {
				t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
			}

			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
			if body["model"] != "deepseek-chat" {
				t.Errorf("expected model deepseek-chat, got %v", body["model"])
			}
			if body["stream"] != true {
				t.Errorf("expected stream true, got %v", body["stream"])
			}
			// Verify stream_options are set for usage.
			opts, _ := body["stream_options"].(map[string]any)
			if opts["include_usage"] != true {
				t.Errorf("expected stream_options.include_usage true, got %v", opts)
			}

			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			fmt.Fprint(w, buildChatSSE(
				`{"id":"chatcmpl-1","model":"deepseek-chat","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi!"},"finish_reason":null}]}`,
				`{"id":"chatcmpl-1","model":"deepseek-chat","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}}`,
				"[DONE]",
			))
		}))
		defer server.Close()

		p := &Provider{id: "deepseek", apiKey: "test-key", baseURL: server.URL, client: server.Client()}
		req := providers.CompleteRequest{
			Model:    providers.ModelRef{ProviderID: "deepseek", ModelID: "deepseek-chat"},
			Messages: []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hi"}}}},
		}

		var chunks []message.ProviderMessageChunk
		for chunk, err := range p.Complete(context.Background(), req) {
			if err != nil {
				t.Fatal(err)
			}
			chunks = append(chunks, chunk)
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

	t.Run("sends tools in function-nested format and optional parameters", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck

			tools, ok := body["tools"].([]any)
			if !ok || len(tools) != 1 {
				t.Errorf("expected 1 tool, got %v", body["tools"])
			}
			// Verify the tool is in Chat Completions nested-function format.
			tool := tools[0].(map[string]any)
			fn, hasFn := tool["function"].(map[string]any)
			if !hasFn {
				t.Error("expected tool to have nested 'function' key")
			} else if fn["name"] != "fn" {
				t.Errorf("expected function name 'fn', got %q", fn["name"])
			}
			// Chat Completions uses max_tokens, not max_output_tokens.
			if body["max_tokens"] != float64(100) {
				t.Errorf("expected max_tokens 100, got %v", body["max_tokens"])
			}
			if body["temperature"] != 0.5 {
				t.Errorf("expected temperature 0.5, got %v", body["temperature"])
			}

			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, buildChatSSE(
				`{"id":"chatcmpl-t","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}}`,
				"[DONE]",
			))
		}))
		defer server.Close()

		p := &Provider{id: "test", apiKey: "k", baseURL: server.URL, client: server.Client()}
		maxTokens := 100
		temp := 0.5
		req := providers.CompleteRequest{
			Model:    providers.ModelRef{ProviderID: "test", ModelID: "m"},
			Messages: []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "x"}}}},
			Tools:    []providers.ToolDefinition{{Name: "fn", InputSchema: json.RawMessage(`{}`)}},

			MaxTokens:   &maxTokens,
			Temperature: &temp,
		}
		for _, err := range p.Complete(context.Background(), req) {
			if err != nil {
				t.Fatal(err)
			}
		}
	})

	t.Run("sends reasoning_effort when Reasoning=enabled", func(t *testing.T) {
		// Chat Completions: reasoning uses "reasoning_effort" field (not the
		// Responses API's "reasoning" object with effort/summary/include).
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck

			if body["reasoning_effort"] != "high" {
				t.Errorf("expected reasoning_effort 'high', got %v", body["reasoning_effort"])
			}
			// Verify the Responses API-specific "include" and "reasoning" fields
			// are NOT present in Chat Completions requests.
			if _, hasInclude := body["include"]; hasInclude {
				t.Error("'include' field should not be present in Chat Completions requests")
			}
			if _, hasReasoning := body["reasoning"]; hasReasoning {
				t.Error("'reasoning' object should not be present in Chat Completions requests")
			}

			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, buildChatSSE(
				`{"id":"r","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}}`,
				"[DONE]",
			))
		}))
		defer server.Close()

		p := &Provider{id: "test", apiKey: "k", baseURL: server.URL, client: server.Client()}
		req := providers.CompleteRequest{
			Model:     providers.ModelRef{ProviderID: "test", ModelID: "m"},
			Messages:  []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "x"}}}},
			Reasoning: "enabled",
		}
		for _, err := range p.Complete(context.Background(), req) {
			if err != nil {
				t.Fatal(err)
			}
		}
	})

	t.Run("merges ProviderOptions into request body", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
			if body["custom_param"] != "custom_value" {
				t.Errorf("expected custom_param in body, got %v", body["custom_param"])
			}

			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, buildChatSSE(
				`{"id":"r","model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":0}}}`,
				"[DONE]",
			))
		}))
		defer server.Close()

		p := &Provider{id: "test", apiKey: "k", baseURL: server.URL, client: server.Client()}
		req := providers.CompleteRequest{
			Model:           providers.ModelRef{ProviderID: "test", ModelID: "m"},
			Messages:        []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "x"}}}},
			ProviderOptions: json.RawMessage(`{"custom_param":"custom_value"}`),
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
			fmt.Fprint(w, `{"error":{"message":"rate limited"}}`)
		}))
		defer server.Close()

		p := &Provider{id: "test", apiKey: "k", baseURL: server.URL, client: server.Client()}
		req := providers.CompleteRequest{
			Model:    providers.ModelRef{ProviderID: "test", ModelID: "m"},
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

// --- TestCountTokens ---

func TestCountTokens(t *testing.T) {
	t.Run("posts to /chat/completions with max_tokens=1 and reads prompt_tokens", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Token counting uses the same /chat/completions endpoint, not a
			// dedicated endpoint (unlike the OpenAI Responses API).
			if r.URL.Path != "/chat/completions" {
				t.Errorf("expected /chat/completions, got %s", r.URL.Path)
			}
			if r.Method != "POST" {
				t.Errorf("expected POST, got %s", r.Method)
			}

			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
			if body["max_tokens"] != float64(1) {
				t.Errorf("expected max_tokens 1, got %v", body["max_tokens"])
			}
			if body["stream"] != false {
				t.Errorf("expected stream false, got %v", body["stream"])
			}
			if body["model"] != "deepseek-chat" {
				t.Errorf("expected model, got %v", body["model"])
			}

			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"id":"chatcmpl-1","choices":[{"message":{"role":"assistant","content":""},"finish_reason":"length"}],"usage":{"prompt_tokens":42,"completion_tokens":1,"total_tokens":43}}`)
		}))
		defer server.Close()

		p := &Provider{id: "test", apiKey: "k", baseURL: server.URL, client: server.Client()}
		resp, err := p.CountTokens(context.Background(), providers.CountTokensRequest{
			Model:    providers.ModelRef{ProviderID: "deepseek", ModelID: "deepseek-chat"},
			Messages: []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hello world"}}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if resp.TotalTokens != 42 {
			t.Errorf("expected 42 tokens, got %d", resp.TotalTokens)
		}
	})

	t.Run("includes tools in request", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
			if _, ok := body["tools"]; !ok {
				t.Error("expected tools in request body")
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"usage":{"prompt_tokens":100,"completion_tokens":1,"total_tokens":101}}`)
		}))
		defer server.Close()

		p := &Provider{id: "test", apiKey: "k", baseURL: server.URL, client: server.Client()}
		resp, err := p.CountTokens(context.Background(), providers.CountTokensRequest{
			Model:    providers.ModelRef{ProviderID: "test", ModelID: "m"},
			Messages: []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "x"}}}},
			Tools:    []providers.ToolDefinition{{Name: "fn", InputSchema: json.RawMessage(`{}`)}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if resp.TotalTokens != 100 {
			t.Errorf("expected 100 tokens, got %d", resp.TotalTokens)
		}
	})

	t.Run("handles API error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(500)
			fmt.Fprint(w, `{"error":"bad"}`)
		}))
		defer server.Close()

		p := &Provider{id: "test", apiKey: "k", baseURL: server.URL, client: server.Client()}
		_, err := p.CountTokens(context.Background(), providers.CountTokensRequest{
			Model:    providers.ModelRef{ProviderID: "test", ModelID: "m"},
			Messages: []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "x"}}}},
		})
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "500") {
			t.Errorf("error should contain status code, got: %v", err)
		}
	})
}

// --- TestListModels ---

func TestListModels(t *testing.T) {
	t.Run("fetches from /models and enriches with modelsdev", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/models" {
				t.Errorf("expected /models, got %s", r.URL.Path)
			}
			if r.Header.Get("Authorization") != "Bearer test-key" {
				t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Type", "application/json")
			// deepseek-chat is a known model in modelsdev for the "deepseek" provider.
			fmt.Fprint(w, `{"data":[{"id":"deepseek-chat","object":"model"},{"id":"unknown-custom-model","object":"model"}]}`)
		}))
		defer server.Close()

		// Use the "deepseek" provider ID so modelsdev.Lookup finds enrichment data.
		p := &Provider{id: "deepseek", apiKey: "test-key", baseURL: server.URL, client: server.Client()}
		models, err := p.ListModels(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(models) != 2 {
			t.Fatalf("expected 2 models, got %d", len(models))
		}

		var known *providers.ModelInfo
		var custom *providers.ModelInfo
		for i := range models {
			switch models[i].ID {
			case "deepseek-chat":
				known = &models[i]
			case "unknown-custom-model":
				custom = &models[i]
			}
		}

		if known == nil {
			t.Fatal("expected deepseek-chat in results")
		}
		// Known model is enriched from modelsdev (context window, display name).
		if known.ContextWindow == 0 {
			t.Error("expected non-zero context window for deepseek-chat")
		}
		if known.DisplayName == "deepseek-chat" {
			t.Error("expected modelsdev to provide a display name for deepseek-chat")
		}

		// Unknown model falls back to ID as display name.
		if custom == nil {
			t.Fatal("expected unknown-custom-model in results")
		}
		if custom.DisplayName != "unknown-custom-model" {
			t.Errorf("expected ID as display name for unknown model, got %q", custom.DisplayName)
		}
	})

	t.Run("handles API error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(401)
			fmt.Fprint(w, `{"error":"unauthorized"}`)
		}))
		defer server.Close()

		p := &Provider{id: "test", apiKey: "bad", baseURL: server.URL, client: server.Client()}
		_, err := p.ListModels(context.Background())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

// --- TestFactoryRegistration ---

func TestFactoryRegistration(t *testing.T) {
	// All 66 @ai-sdk/openai-compatible providers should be registered by init().
	// Use well-known providers as canaries.
	for _, id := range []string{"deepseek", "fireworks-ai", "aihubmix"} {
		if !providers.Has(id) {
			t.Errorf("expected provider %q to be registered via init()", id)
		}
	}

	// Verify a registered provider can be constructed and returns the correct ID.
	p, err := providers.New("deepseek", providers.Config{"api_key": "test"})
	if err != nil {
		t.Fatal(err)
	}
	if p.ID() != "deepseek" {
		t.Errorf("expected ID 'deepseek', got %q", p.ID())
	}
}
