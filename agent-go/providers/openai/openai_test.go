package openai

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
	t.Run("requires api key", func(t *testing.T) {
		_, err := New(providers.Config{})
		if err == nil {
			t.Fatal("expected error for missing api key")
		}
	})

	t.Run("uses default base url", func(t *testing.T) {
		p, err := New(providers.Config{"api_key": "test-key"})
		if err != nil {
			t.Fatal(err)
		}
		op := p.(*Provider)
		if op.baseURL != "https://api.openai.com/v1" {
			t.Errorf("expected normalised base URL %q, got %q", "https://api.openai.com/v1", op.baseURL)
		}
		if op.ws == nil {
			t.Fatal("expected default config to enable WebSocket mode")
		}
		if op.ws.wsURL != "wss://api.openai.com/v1/responses" {
			t.Errorf("expected ws URL %q, got %q", "wss://api.openai.com/v1/responses", op.ws.wsURL)
		}
	})

	t.Run("uses custom base url and strips trailing slash", func(t *testing.T) {
		p, err := New(providers.Config{"api_key": "test-key", "base_url": "https://custom.api.com/v1/"})
		if err != nil {
			t.Fatal(err)
		}
		op := p.(*Provider)
		if op.baseURL != "https://custom.api.com/v1" {
			t.Errorf("expected base URL %q, got %q", "https://custom.api.com/v1", op.baseURL)
		}
	})
}

func TestProviderID(t *testing.T) {
	p, _ := New(providers.Config{"api_key": "test"})
	if p.ID() != "openai" {
		t.Errorf("expected ID %q, got %q", "openai", p.ID())
	}
}

func TestConvertMessages(t *testing.T) {
	t.Run("system message becomes developer", func(t *testing.T) {
		msgs := []message.Message{
			{Role: "system", Parts: []message.Part{message.TextPart{Text: "You are helpful"}}},
		}
		items, err := convertMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 {
			t.Fatalf("expected 1 item, got %d", len(items))
		}
		var item map[string]any
		json.Unmarshal(items[0], &item)
		if item["role"] != "developer" {
			t.Errorf("expected role 'developer', got %q", item["role"])
		}
		if item["content"] != "You are helpful" {
			t.Errorf("expected content 'You are helpful', got %q", item["content"])
		}
	})

	t.Run("user message simple text uses string shorthand", func(t *testing.T) {
		msgs := []message.Message{
			{Role: "user", Parts: []message.Part{message.TextPart{Text: "Hello"}}},
		}
		items, err := convertMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		var item map[string]any
		json.Unmarshal(items[0], &item)
		if item["role"] != "user" {
			t.Errorf("expected role 'user', got %q", item["role"])
		}
		// Single text uses string shorthand, not array.
		if item["content"] != "Hello" {
			t.Errorf("expected content 'Hello', got %q", item["content"])
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
		var item map[string]any
		json.Unmarshal(items[0], &item)
		content, ok := item["content"].([]any)
		if !ok {
			t.Fatalf("expected content to be array, got %T", item["content"])
		}
		if len(content) != 2 {
			t.Fatalf("expected 2 content parts, got %d", len(content))
		}
		imgPart := content[1].(map[string]any)
		if imgPart["type"] != "input_image" {
			t.Errorf("expected type 'input_image', got %q", imgPart["type"])
		}
		if imgPart["image_url"] != "https://example.com/img.jpg" {
			t.Errorf("expected image URL, got %q", imgPart["image_url"])
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
		var item map[string]any
		json.Unmarshal(items[0], &item)
		content := item["content"].([]any)
		imgPart := content[0].(map[string]any)
		expected := "data:image/png;base64,abc123base64"
		if imgPart["image_url"] != expected {
			t.Errorf("expected %q, got %q", expected, imgPart["image_url"])
		}
	})

	t.Run("assistant message with text and tool call", func(t *testing.T) {
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
		if len(items) != 3 {
			t.Fatalf("expected 3 items (message + function_call + synthetic output), got %d", len(items))
		}
		// First: typed message.
		var msgItem map[string]any
		json.Unmarshal(items[0], &msgItem)
		if msgItem["type"] != "message" {
			t.Errorf("expected type 'message', got %q", msgItem["type"])
		}
		if msgItem["role"] != "assistant" {
			t.Errorf("expected role 'assistant', got %q", msgItem["role"])
		}
		// Second: function_call.
		var fcItem map[string]any
		json.Unmarshal(items[1], &fcItem)
		if fcItem["type"] != "function_call" {
			t.Errorf("expected type 'function_call', got %q", fcItem["type"])
		}
		if fcItem["call_id"] != "call_123" {
			t.Errorf("expected call_id 'call_123', got %q", fcItem["call_id"])
		}
		if fcItem["name"] != "get_weather" {
			t.Errorf("expected name 'get_weather', got %q", fcItem["name"])
		}
		if fcItem["arguments"] != `{"location":"Paris"}` {
			t.Errorf("unexpected arguments: %q", fcItem["arguments"])
		}

		// Third: synthetic function_call_output for unresolved call.
		var outItem map[string]any
		json.Unmarshal(items[2], &outItem)
		if outItem["type"] != "function_call_output" {
			t.Errorf("expected type 'function_call_output', got %q", outItem["type"])
		}
		if outItem["call_id"] != "call_123" {
			t.Errorf("expected call_id 'call_123', got %q", outItem["call_id"])
		}
		if outItem["output"] != missingToolOutputText {
			t.Errorf("expected output %q, got %q", missingToolOutputText, outItem["output"])
		}
	})

	t.Run("assistant message with only tool calls omits message item", func(t *testing.T) {
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
		if len(items) != 2 {
			t.Fatalf("expected 2 items (function_call + synthetic output), got %d", len(items))
		}
		var item map[string]any
		json.Unmarshal(items[0], &item)
		if item["type"] != "function_call" {
			t.Errorf("expected type 'function_call', got %q", item["type"])
		}
		var outItem map[string]any
		json.Unmarshal(items[1], &outItem)
		if outItem["type"] != "function_call_output" {
			t.Errorf("expected type 'function_call_output', got %q", outItem["type"])
		}
		if outItem["call_id"] != "call_1" {
			t.Errorf("expected call_id 'call_1', got %q", outItem["call_id"])
		}
		if outItem["output"] != missingToolOutputText {
			t.Errorf("expected output %q, got %q", missingToolOutputText, outItem["output"])
		}
	})

	t.Run("assistant message with provider-executed inline tool result", func(t *testing.T) {
		msgs := []message.Message{
			{Role: "assistant", Parts: []message.Part{
				message.ToolCallPart{ToolCallID: "call_inline", ToolName: "code_interpreter", Input: `{"code":"1+1"}`},
				message.ToolResultPart{ToolCallID: "call_inline", ToolName: "code_interpreter", Output: message.TextOutput{Value: "2"}},
				message.TextPart{Text: "Done."},
			}},
		}
		items, err := convertMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 3 {
			t.Fatalf("expected 3 items (message + function_call + function_call_output), got %d", len(items))
		}

		var fc map[string]any
		json.Unmarshal(items[1], &fc)
		if fc["type"] != "function_call" {
			t.Errorf("expected type 'function_call', got %q", fc["type"])
		}

		var out map[string]any
		json.Unmarshal(items[2], &out)
		if out["type"] != "function_call_output" {
			t.Errorf("expected type 'function_call_output', got %q", out["type"])
		}
		if out["call_id"] != "call_inline" {
			t.Errorf("expected call_id 'call_inline', got %q", out["call_id"])
		}
		if out["output"] != "2" {
			t.Errorf("expected output '2', got %q", out["output"])
		}
	})

	t.Run("tool result message", func(t *testing.T) {
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
			t.Fatalf("expected 1 item, got %d", len(items))
		}
		var item map[string]any
		json.Unmarshal(items[0], &item)
		if item["type"] != "function_call_output" {
			t.Errorf("expected type 'function_call_output', got %q", item["type"])
		}
		if item["call_id"] != "call_123" {
			t.Errorf("expected call_id 'call_123', got %q", item["call_id"])
		}
		if item["output"] != "25C sunny" {
			t.Errorf("expected output '25C sunny', got %q", item["output"])
		}
	})

	t.Run("assistant message with reasoning part and provider metadata", func(t *testing.T) {
		providerMeta := json.RawMessage(`{"openai":{"id":"rs_1","type":"reasoning","encrypted_content":"gAAAA_enc","summary":[{"type":"summary_text","text":"Thinking about it..."}]}}`)
		msgs := []message.Message{
			{Role: "assistant", Parts: []message.Part{
				message.ReasoningPart{
					ID:               "rs_1",
					Text:             "Thinking about it...",
					ProviderMetadata: providerMeta,
				},
				message.TextPart{Text: "The answer is 42."},
			}},
		}
		items, err := convertMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 2 {
			t.Fatalf("expected 2 items (reasoning + message), got %d", len(items))
		}

		// First: reasoning item (from ProviderMetadata, with id stripped).
		var rsItem map[string]any
		json.Unmarshal(items[0], &rsItem)
		if rsItem["type"] != "reasoning" {
			t.Errorf("expected type 'reasoning', got %q", rsItem["type"])
		}
		if rsItem["encrypted_content"] != "gAAAA_enc" {
			t.Errorf("expected encrypted_content preserved, got %v", rsItem["encrypted_content"])
		}
		if _, hasID := rsItem["id"]; hasID {
			t.Errorf("expected id to be stripped (store=false), but got %v", rsItem["id"])
		}

		// Second: text message.
		var msgItem map[string]any
		json.Unmarshal(items[1], &msgItem)
		if msgItem["type"] != "message" {
			t.Errorf("expected type 'message', got %q", msgItem["type"])
		}
	})

	t.Run("assistant message with reasoning part without provider metadata", func(t *testing.T) {
		msgs := []message.Message{
			{Role: "assistant", Parts: []message.Part{
				message.ReasoningPart{
					ID:   "rs_2",
					Text: "Let me think...",
				},
				message.TextPart{Text: "Done."},
			}},
		}
		items, err := convertMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 2 {
			t.Fatalf("expected 2 items (reasoning + message), got %d", len(items))
		}

		// First: constructed reasoning item with summary (no id, store=false).
		var rsItem map[string]any
		json.Unmarshal(items[0], &rsItem)
		if rsItem["type"] != "reasoning" {
			t.Errorf("expected type 'reasoning', got %q", rsItem["type"])
		}
		if _, hasID := rsItem["id"]; hasID {
			t.Errorf("expected no id (store=false), but got %v", rsItem["id"])
		}
		summary, ok := rsItem["summary"].([]any)
		if !ok || len(summary) != 1 {
			t.Fatalf("expected summary with 1 item, got %v", rsItem["summary"])
		}
		summaryItem := summary[0].(map[string]any)
		if summaryItem["text"] != "Let me think..." {
			t.Errorf("expected summary text 'Let me think...', got %v", summaryItem["text"])
		}
	})

	t.Run("assistant message with reasoning and tool calls", func(t *testing.T) {
		providerMeta := json.RawMessage(`{"openai":{"id":"rs_1","type":"reasoning","summary":[]}}`)
		msgs := []message.Message{
			{Role: "assistant", Parts: []message.Part{
				message.ReasoningPart{ID: "rs_1", ProviderMetadata: providerMeta},
				message.ToolCallPart{ToolCallID: "call_1", ToolName: "fn", Input: `{}`},
			}},
		}
		items, err := convertMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 3 {
			t.Fatalf("expected 3 items (reasoning + function_call + synthetic output), got %d", len(items))
		}

		var rsItem map[string]any
		json.Unmarshal(items[0], &rsItem)
		if rsItem["type"] != "reasoning" {
			t.Errorf("expected first item to be reasoning, got %q", rsItem["type"])
		}
		if _, hasID := rsItem["id"]; hasID {
			t.Errorf("expected id to be stripped (store=false), but got %v", rsItem["id"])
		}

		var fcItem map[string]any
		json.Unmarshal(items[1], &fcItem)
		if fcItem["type"] != "function_call" {
			t.Errorf("expected second item to be function_call, got %q", fcItem["type"])
		}

		var outItem map[string]any
		json.Unmarshal(items[2], &outItem)
		if outItem["type"] != "function_call_output" {
			t.Errorf("expected third item to be function_call_output, got %q", outItem["type"])
		}
	})

	t.Run("multiple tool results from one message", func(t *testing.T) {
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
		if len(items) != 2 {
			t.Fatalf("expected 2 items, got %d", len(items))
		}
	})

	t.Run("custom tool call uses custom_tool_call format", func(t *testing.T) {
		msgs := []message.Message{
			{Role: "assistant", Parts: []message.Part{
				message.ToolCallPart{
					ToolCallID: "call_custom_1",
					ToolName:   "apply_patch",
					Input:      "*** Begin Patch\n*** End Patch",
				},
			}},
		}
		items, err := convertMessagesWithCustomTools(msgs, map[string]struct{}{"apply_patch": {}})
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 2 {
			t.Fatalf("expected 2 items (custom call + synthetic custom output), got %d", len(items))
		}

		var callItem map[string]any
		json.Unmarshal(items[0], &callItem)
		if callItem["type"] != "custom_tool_call" {
			t.Fatalf("expected type custom_tool_call, got %v", callItem["type"])
		}
		if callItem["name"] != "apply_patch" {
			t.Fatalf("expected name apply_patch, got %v", callItem["name"])
		}
		if callItem["input"] != "*** Begin Patch\n*** End Patch" {
			t.Fatalf("unexpected custom input: %v", callItem["input"])
		}

		var outItem map[string]any
		json.Unmarshal(items[1], &outItem)
		if outItem["type"] != "custom_tool_call_output" {
			t.Fatalf("expected type custom_tool_call_output, got %v", outItem["type"])
		}
	})

	t.Run("custom tool result maps to custom_tool_call_output", func(t *testing.T) {
		msgs := []message.Message{
			{Role: "tool", Parts: []message.Part{
				message.ToolResultPart{
					ToolCallID: "call_custom_2",
					ToolName:   "apply_patch",
					Output:     message.TextOutput{Value: "ok"},
				},
			}},
		}
		items, err := convertMessagesWithCustomTools(msgs, map[string]struct{}{"apply_patch": {}})
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 {
			t.Fatalf("expected 1 item, got %d", len(items))
		}
		var item map[string]any
		json.Unmarshal(items[0], &item)
		if item["type"] != "custom_tool_call_output" {
			t.Fatalf("expected custom_tool_call_output, got %v", item["type"])
		}
	})
}

func TestConvertTools(t *testing.T) {
	t.Run("maps to Responses API function format", func(t *testing.T) {
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
		// In Responses API, name is at top level, NOT nested under "function".
		if result[0]["name"] != "get_weather" {
			t.Errorf("expected name 'get_weather', got %q", result[0]["name"])
		}
		if result[0]["description"] != "Get current weather" {
			t.Errorf("expected description, got %q", result[0]["description"])
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

	t.Run("maps custom tool format", func(t *testing.T) {
		tools := []providers.ToolDefinition{
			{
				Type:        "custom",
				Name:        "apply_patch",
				Description: "Raw patch tool",
				Format: &providers.ToolFormat{
					Type:       "grammar",
					Syntax:     "lark",
					Definition: "start: /[\\s\\S]+/",
				},
			},
		}
		result := convertTools(tools)
		if got := result[0]["type"]; got != "custom" {
			t.Fatalf("expected custom tool type, got %v", got)
		}
		if got := result[0]["name"]; got != "apply_patch" {
			t.Fatalf("expected custom tool name, got %v", got)
		}
		format, ok := result[0]["format"].(map[string]any)
		if !ok {
			t.Fatalf("expected custom format map, got %T", result[0]["format"])
		}
		if format["type"] != "grammar" || format["syntax"] != "lark" {
			t.Fatalf("unexpected custom format: %#v", format)
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

func TestToolResultToOpenAIOutput(t *testing.T) {
	t.Run("non-content output remains string", func(t *testing.T) {
		result := toolResultToOpenAIOutput(message.TextOutput{Value: "hello"})
		text, ok := result.(string)
		if !ok {
			t.Fatalf("expected string output, got %T", result)
		}
		if text != "hello" {
			t.Errorf("expected hello, got %q", text)
		}
	})

	t.Run("text-only content output remains string", func(t *testing.T) {
		result := toolResultToOpenAIOutput(message.ContentOutput{
			Value: []message.ToolResultContentItem{
				message.ContentTextItem{Text: "line1"},
				message.ContentTextItem{Text: "line2"},
			},
		})
		text, ok := result.(string)
		if !ok {
			t.Fatalf("expected string output, got %T", result)
		}
		if text != "line1\nline2" {
			t.Errorf("expected joined text, got %q", text)
		}
	})

	t.Run("mixed content output becomes content item array", func(t *testing.T) {
		result := toolResultToOpenAIOutput(message.ContentOutput{
			Value: []message.ToolResultContentItem{
				message.ContentTextItem{Text: "summary"},
				message.ContentImageDataItem{Data: "aGVsbG8=", MediaType: "image/png"},
			},
		})

		items, ok := result.([]any)
		if !ok {
			t.Fatalf("expected []any output, got %T", result)
		}
		if len(items) != 2 {
			t.Fatalf("expected 2 content items, got %d", len(items))
		}

		first, ok := items[0].(map[string]any)
		if !ok {
			t.Fatalf("expected map for first item, got %T", items[0])
		}
		if first["type"] != "input_text" {
			t.Errorf("expected first type input_text, got %v", first["type"])
		}

		second, ok := items[1].(map[string]any)
		if !ok {
			t.Fatalf("expected map for second item, got %T", items[1])
		}
		if second["type"] != "input_image" {
			t.Errorf("expected second type input_image, got %v", second["type"])
		}
		imageURL, _ := second["image_url"].(string)
		if imageURL != "data:image/png;base64,aGVsbG8=" {
			t.Errorf("unexpected image_url: %q", imageURL)
		}
	})
}

func TestParseSSEStream(t *testing.T) {
	t.Run("text response", func(t *testing.T) {
		sse := buildSSE(
			"response.created", `{"response":{"id":"resp_1","model":"gpt-4o"}}`,
			"response.output_item.added", `{"item":{"id":"msg_1","type":"message","role":"assistant"}}`,
			"response.content_part.added", `{"part":{"type":"output_text"},"item_id":"msg_1"}`,
			"response.output_text.delta", `{"item_id":"msg_1","delta":"Hello "}`,
			"response.output_text.delta", `{"item_id":"msg_1","delta":"world!"}`,
			"response.output_text.done", `{"item_id":"msg_1","text":"Hello world!"}`,
			"response.content_part.done", `{}`,
			"response.output_item.done", `{"item":{"id":"msg_1","type":"message"}}`,
			"response.completed", `{"response":{"status":"completed","output":[{"type":"message"}],"usage":{"input_tokens":10,"input_tokens_details":{"cached_tokens":0},"output_tokens":5,"output_tokens_details":{"reasoning_tokens":0}}}}`,
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
		if f.Usage.InputTokens.Total != 10 {
			t.Errorf("expected 10 input tokens, got %d", f.Usage.InputTokens.Total)
		}
		if f.Usage.OutputTokens.Total != 5 {
			t.Errorf("expected 5 output tokens, got %d", f.Usage.OutputTokens.Total)
		}
	})

	t.Run("tool call response with call_id in delta (legacy format)", func(t *testing.T) {
		sse := buildSSE(
			"response.created", `{"response":{"id":"resp_2","model":"gpt-4o"}}`,
			"response.output_item.added", `{"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"get_weather"}}`,
			"response.function_call_arguments.delta", `{"call_id":"call_1","delta":"{\"loc"}`,
			"response.function_call_arguments.delta", `{"call_id":"call_1","delta":"ation\":\"Paris\"}"}`,
			"response.function_call_arguments.done", `{"call_id":"call_1","arguments":"{\"location\":\"Paris\"}"}`,
			"response.output_item.done", `{"item":{"id":"fc_1","type":"function_call"}}`,
			"response.completed", `{"response":{"status":"completed","output":[{"type":"function_call"}],"usage":{"input_tokens":20,"input_tokens_details":{"cached_tokens":5},"output_tokens":15,"output_tokens_details":{"reasoning_tokens":0}}}}`,
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

	t.Run("tool call response with item_id fallback (real API format)", func(t *testing.T) {
		// The real OpenAI Responses API emits empty call_id in
		// response.function_call_arguments.delta and .done events — only
		// item_id is populated there. The actual call_id is only present in the
		// preceding response.output_item.added event. This test verifies that
		// the parser correctly resolves call_id via item_id state tracking.
		sse := buildSSE(
			"response.created", `{"response":{"id":"resp_2","model":"gpt-4o"}}`,
			"response.output_item.added", `{"item":{"id":"fc_item_1","type":"function_call","call_id":"call_abc123","name":"get_weather"}}`,
			"response.function_call_arguments.delta", `{"item_id":"fc_item_1","output_index":0,"call_id":"","delta":"{\"loc"}`,
			"response.function_call_arguments.delta", `{"item_id":"fc_item_1","output_index":0,"call_id":"","delta":"ation\":\"Paris\"}"}`,
			"response.function_call_arguments.done", `{"item_id":"fc_item_1","output_index":0,"call_id":"","arguments":"{\"location\":\"Paris\"}"}`,
			"response.output_item.done", `{"item":{"id":"fc_item_1","type":"function_call"}}`,
			"response.completed", `{"response":{"status":"completed","output":[{"type":"function_call"}],"usage":{"input_tokens":20,"input_tokens_details":{"cached_tokens":5},"output_tokens":15,"output_tokens_details":{"reasoning_tokens":0}}}}`,
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
		if ts.ToolCallID != "call_abc123" {
			t.Errorf("expected ToolCallID 'call_abc123', got %q", ts.ToolCallID)
		}
		if ts.ToolName != "get_weather" {
			t.Errorf("expected ToolName 'get_weather', got %q", ts.ToolName)
		}

		// Deltas must have call_id resolved from item_id via state tracking.
		td1 := chunks[3].(message.ToolInputDeltaChunk)
		if td1.ToolCallID != "call_abc123" {
			t.Errorf("expected delta ToolCallID 'call_abc123', got %q", td1.ToolCallID)
		}
		td2 := chunks[4].(message.ToolInputDeltaChunk)
		if td2.ToolCallID != "call_abc123" {
			t.Errorf("expected delta ToolCallID 'call_abc123', got %q", td2.ToolCallID)
		}

		te := chunks[5].(message.ToolInputEndChunk)
		if te.ToolCallID != "call_abc123" {
			t.Errorf("expected end ToolCallID 'call_abc123', got %q", te.ToolCallID)
		}
	})

	t.Run("custom tool call from output_item.done", func(t *testing.T) {
		sse := buildSSE(
			"response.created", `{"response":{"id":"resp_custom","model":"gpt-5"}}`,
			"response.output_item.done", `{"item":{"id":"ct_1","type":"custom_tool_call","call_id":"call_patch_1","name":"apply_patch","input":"*** Begin Patch\n*** End Patch"}}`,
			"response.completed", `{"response":{"status":"completed","output":[{"type":"custom_tool_call"}],"usage":{"input_tokens":12,"input_tokens_details":{"cached_tokens":0},"output_tokens":3,"output_tokens_details":{"reasoning_tokens":0}}}}`,
		)

		chunks := collectChunks(t, sse)
		assertChunkTypes(t, chunks,
			"stream-start",
			"response-metadata",
			"tool-call",
			"finish",
		)

		call := chunks[2].(message.ToolCallChunk)
		if call.ToolCallID != "call_patch_1" {
			t.Fatalf("expected tool call id call_patch_1, got %q", call.ToolCallID)
		}
		if call.ToolName != "apply_patch" {
			t.Fatalf("expected tool name apply_patch, got %q", call.ToolName)
		}
		if call.Input != "*** Begin Patch\n*** End Patch" {
			t.Fatalf("unexpected custom tool input: %q", call.Input)
		}

		finish := chunks[3].(message.FinishChunk)
		if finish.FinishReason.Unified != "tool-calls" {
			t.Fatalf("expected finish reason tool-calls, got %q", finish.FinishReason.Unified)
		}
	})

	t.Run("reasoning response", func(t *testing.T) {
		sse := buildSSE(
			"response.created", `{"response":{"id":"resp_3","model":"o3"}}`,
			"response.output_item.added", `{"item":{"id":"rs_1","type":"reasoning"}}`,
			"response.reasoning_summary_part.added", `{}`,
			"response.reasoning_summary_text.delta", `{"item_id":"rs_1","delta":"Thinking..."}`,
			"response.reasoning_summary_text.done", `{"item_id":"rs_1"}`,
			"response.reasoning_summary_part.done", `{}`,
			"response.output_item.done", `{"item":{"id":"rs_1","type":"reasoning","encrypted_content":"gAAAA_encrypted","summary":[{"type":"summary_text","text":"Thinking..."}]}}`,
			"response.output_item.added", `{"item":{"id":"msg_1","type":"message"}}`,
			"response.content_part.added", `{"part":{"type":"output_text"},"item_id":"msg_1"}`,
			"response.output_text.delta", `{"item_id":"msg_1","delta":"The answer is 42."}`,
			"response.output_text.done", `{"item_id":"msg_1"}`,
			"response.content_part.done", `{}`,
			"response.output_item.done", `{"item":{"id":"msg_1","type":"message"}}`,
			"response.completed", `{"response":{"status":"completed","output":[{"type":"reasoning"},{"type":"message"}],"usage":{"input_tokens":50,"input_tokens_details":{"cached_tokens":0},"output_tokens":100,"output_tokens_details":{"reasoning_tokens":80}}}}`,
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
		if rd.Delta != "Thinking..." {
			t.Errorf("expected reasoning delta 'Thinking...', got %q", rd.Delta)
		}

		// Verify encrypted_content is preserved in nested ProviderMetadata.
		re := chunks[4].(message.ReasoningEndChunk)
		if len(re.ProviderMetadata) == 0 {
			t.Fatal("expected ProviderMetadata on ReasoningEndChunk")
		}
		// Outer map should be {"openai": {...}} (nested Vercel AI SDK v6 format).
		var outerMap map[string]map[string]any
		if err := json.Unmarshal(re.ProviderMetadata, &outerMap); err != nil {
			t.Fatalf("expected nested JSON ProviderMetadata: %v", err)
		}
		openaiItem, ok := outerMap["openai"]
		if !ok {
			t.Fatal("expected 'openai' key in ProviderMetadata")
		}
		if openaiItem["encrypted_content"] != "gAAAA_encrypted" {
			t.Errorf("expected encrypted_content in ProviderMetadata, got %v", openaiItem["encrypted_content"])
		}

		f := chunks[8].(message.FinishChunk)
		if f.Usage.OutputTokens.Reasoning != 80 {
			t.Errorf("expected 80 reasoning tokens, got %d", f.Usage.OutputTokens.Reasoning)
		}
		if f.Usage.OutputTokens.Text != 20 {
			t.Errorf("expected 20 text tokens, got %d", f.Usage.OutputTokens.Text)
		}
	})

	t.Run("incomplete response", func(t *testing.T) {
		sse := buildSSE(
			"response.created", `{"response":{"id":"resp_4","model":"gpt-4o"}}`,
			"response.output_item.added", `{"item":{"id":"msg_1","type":"message"}}`,
			"response.content_part.added", `{"part":{"type":"output_text"},"item_id":"msg_1"}`,
			"response.output_text.delta", `{"item_id":"msg_1","delta":"partial..."}`,
			"response.output_text.done", `{"item_id":"msg_1"}`,
			"response.content_part.done", `{}`,
			"response.output_item.done", `{"item":{"id":"msg_1","type":"message"}}`,
			"response.incomplete", `{"response":{"usage":{"input_tokens":100,"input_tokens_details":{"cached_tokens":0},"output_tokens":4096,"output_tokens_details":{"reasoning_tokens":0}}}}`,
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

	t.Run("error event", func(t *testing.T) {
		sse := buildSSE(
			"error", `{"message":"rate limit exceeded","code":"rate_limit"}`,
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
		if !strings.Contains(gotErr.Error(), "rate limit exceeded") {
			t.Errorf("error should contain 'rate limit exceeded', got: %v", gotErr)
		}
	})

	t.Run("response.failed event", func(t *testing.T) {
		sse := buildSSE(
			"response.created", `{"response":{"id":"resp_5","model":"gpt-4o"}}`,
			"response.failed", `{"response":{"error":{"message":"content filter triggered","code":"content_filter"}}}`,
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
			t.Fatal("expected error from response.failed")
		}
		if !strings.Contains(gotErr.Error(), "content filter triggered") {
			t.Errorf("error should contain 'content filter triggered', got: %v", gotErr)
		}
	})

	t.Run("unknown events are ignored", func(t *testing.T) {
		sse := buildSSE(
			"response.created", `{"response":{"id":"resp_6","model":"gpt-4o"}}`,
			"response.in_progress", `{}`,
			"response.queued", `{}`,
			"some.future.event", `{"data":"ignored"}`,
			"response.completed", `{"response":{"status":"completed","output":[],"usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":0}}}}`,
		)

		chunks := collectChunks(t, sse)
		assertChunkTypes(t, chunks,
			"stream-start",
			"response-metadata",
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
			if r.URL.Path != "/responses" {
				t.Errorf("expected /responses, got %s", r.URL.Path)
			}
			if r.Header.Get("Authorization") != "Bearer test-key" {
				t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
			}

			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["model"] != "gpt-4o" {
				t.Errorf("expected model gpt-4o, got %v", body["model"])
			}
			if body["stream"] != true {
				t.Errorf("expected stream true, got %v", body["stream"])
			}
			if body["store"] != false {
				t.Errorf("expected store false, got %v", body["store"])
			}
			if body["truncation"] != "disabled" {
				t.Errorf("expected truncation disabled, got %v", body["truncation"])
			}

			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			fmt.Fprint(w, buildSSE(
				"response.created", `{"response":{"id":"resp_1","model":"gpt-4o"}}`,
				"response.output_item.added", `{"item":{"id":"msg_1","type":"message"}}`,
				"response.content_part.added", `{"part":{"type":"output_text"},"item_id":"msg_1"}`,
				"response.output_text.delta", `{"item_id":"msg_1","delta":"Hi!"}`,
				"response.output_text.done", `{"item_id":"msg_1"}`,
				"response.content_part.done", `{}`,
				"response.output_item.done", `{"item":{"id":"msg_1","type":"message"}}`,
				"response.completed", `{"response":{"status":"completed","output":[{"type":"message"}],"usage":{"input_tokens":5,"input_tokens_details":{"cached_tokens":0},"output_tokens":2,"output_tokens_details":{"reasoning_tokens":0}}}}`,
			))
		}))
		defer server.Close()

		p := &Provider{
			apiKey:  "test-key",
			baseURL: server.URL,
			client:  server.Client(),
		}

		req := providers.CompleteRequest{
			Model: providers.ModelRef{ProviderID: "openai", ModelID: "gpt-4o"},
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

	t.Run("sends tools and optional parameters", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)

			if _, ok := body["tools"]; !ok {
				t.Error("expected tools in request body")
			}
			tools := body["tools"].([]any)
			if len(tools) != 1 {
				t.Errorf("expected 1 tool, got %d", len(tools))
			}

			if body["max_output_tokens"] != float64(100) {
				t.Errorf("expected max_output_tokens 100, got %v", body["max_output_tokens"])
			}
			if body["temperature"] != 0.5 {
				t.Errorf("expected temperature 0.5, got %v", body["temperature"])
			}

			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, buildSSE(
				"response.created", `{"response":{"id":"resp_t","model":"gpt-4o"}}`,
				"response.completed", `{"response":{"status":"completed","output":[],"usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":0}}}}`,
			))
		}))
		defer server.Close()

		p := &Provider{apiKey: "k", baseURL: server.URL, client: server.Client()}
		maxTokens := 100
		temp := 0.5
		req := providers.CompleteRequest{
			Model: providers.ModelRef{ProviderID: "openai", ModelID: "gpt-4o"},
			Messages: []message.Message{
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

	t.Run("sends custom tools", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)

			tools := body["tools"].([]any)
			if len(tools) != 1 {
				t.Fatalf("expected 1 tool, got %d", len(tools))
			}
			tool := tools[0].(map[string]any)
			if tool["type"] != "custom" {
				t.Fatalf("expected custom tool type, got %v", tool["type"])
			}
			if tool["name"] != "apply_patch" {
				t.Fatalf("expected tool name apply_patch, got %v", tool["name"])
			}
			format, ok := tool["format"].(map[string]any)
			if !ok {
				t.Fatalf("expected format object, got %T", tool["format"])
			}
			if format["type"] != "grammar" || format["syntax"] != "lark" {
				t.Fatalf("unexpected custom format: %#v", format)
			}

			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, buildSSE(
				"response.created", `{"response":{"id":"resp_custom","model":"gpt-5"}}`,
				"response.completed", `{"response":{"status":"completed","output":[],"usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":0}}}}`,
			))
		}))
		defer server.Close()

		p := &Provider{apiKey: "k", baseURL: server.URL, client: server.Client()}
		req := providers.CompleteRequest{
			Model: providers.ModelRef{ProviderID: "openai", ModelID: "gpt-5"},
			Messages: []message.Message{
				{Role: "user", Parts: []message.Part{message.TextPart{Text: "x"}}},
			},
			Tools: []providers.ToolDefinition{{
				Type: "custom",
				Name: "apply_patch",
				Format: &providers.ToolFormat{
					Type:       "grammar",
					Syntax:     "lark",
					Definition: "start: /[\\s\\S]+/",
				},
			}},
		}
		for _, err := range p.Complete(context.Background(), req) {
			if err != nil {
				t.Fatal(err)
			}
		}
	})

	t.Run("sends reasoning config with include encrypted_content", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)

			reasoning, ok := body["reasoning"].(map[string]any)
			if !ok {
				t.Fatal("expected reasoning in request body")
			}
			if reasoning["effort"] != "high" {
				t.Errorf("expected effort 'high', got %v", reasoning["effort"])
			}
			if reasoning["summary"] != "detailed" {
				t.Errorf("expected summary 'detailed', got %v", reasoning["summary"])
			}

			// Verify include parameter for encrypted_content.
			include, ok := body["include"].([]any)
			if !ok {
				t.Fatal("expected include array in request body")
			}
			found := false
			for _, v := range include {
				if v == "reasoning.encrypted_content" {
					found = true
				}
			}
			if !found {
				t.Error("expected 'reasoning.encrypted_content' in include array")
			}

			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, buildSSE(
				"response.created", `{"response":{"id":"r","model":"o3"}}`,
				"response.completed", `{"response":{"status":"completed","output":[],"usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":0}}}}`,
			))
		}))
		defer server.Close()

		p := &Provider{apiKey: "k", baseURL: server.URL, client: server.Client()}
		req := providers.CompleteRequest{
			Model:     providers.ModelRef{ProviderID: "openai", ModelID: "o3"},
			Messages:  []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "x"}}}},
			Reasoning: "enabled",
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

		p := &Provider{apiKey: "k", baseURL: server.URL, client: server.Client()}
		req := providers.CompleteRequest{
			Model:    providers.ModelRef{ProviderID: "openai", ModelID: "gpt-4o"},
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

func TestListModels(t *testing.T) {
	t.Run("fetches from API and enriches with modelsdev", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/models" {
				t.Errorf("expected /models, got %s", r.URL.Path)
			}
			if r.Header.Get("Authorization") != "Bearer test-key" {
				t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"data":[{"id":"gpt-4o","object":"model"},{"id":"o3","object":"model"},{"id":"ft:custom-2024","object":"model"}]}`)
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

		// Known model: enriched from modelsdev.
		var gpt4o *providers.ModelInfo
		var o3model *providers.ModelInfo
		var custom *providers.ModelInfo
		for i := range models {
			switch models[i].ID {
			case "gpt-4o":
				gpt4o = &models[i]
			case "o3":
				o3model = &models[i]
			case "ft:custom-2024":
				custom = &models[i]
			}
		}

		if gpt4o == nil {
			t.Fatal("expected gpt-4o in results")
		}
		if gpt4o.ContextWindow == 0 {
			t.Error("expected non-zero context window for gpt-4o")
		}
		if gpt4o.DisplayName == "gpt-4o" {
			t.Error("expected modelsdev to provide a display name for gpt-4o")
		}

		if o3model == nil {
			t.Fatal("expected o3 in results")
		}
		if !o3model.Reasoning {
			t.Error("expected o3 to have Reasoning=true")
		}

		// Unknown model: falls back to ID as display name.
		if custom == nil {
			t.Fatal("expected ft:custom-2024 in results")
		}
		if custom.DisplayName != "ft:custom-2024" {
			t.Errorf("expected unknown model to use ID as display name, got %q", custom.DisplayName)
		}
	})

	t.Run("handles API error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(401)
			fmt.Fprint(w, `{"error":"unauthorized"}`)
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
	if !providers.Has("openai") {
		t.Fatal("expected openai provider to be registered via init()")
	}
	p, err := providers.New("openai", providers.Config{"api_key": "test"})
	if err != nil {
		t.Fatal(err)
	}
	if p.ID() != "openai" {
		t.Errorf("expected ID 'openai', got %q", p.ID())
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
		"tool-call":         "message.ToolCallChunk",
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

func TestComplete_AutoReasoning(t *testing.T) {
	minimalSSE := buildSSE(
		"response.created", `{"response":{"id":"resp_1","model":"gpt-5"}}`,
		"response.completed", `{"response":{"status":"completed","output":[],"usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":0}}}}`,
	)

	t.Run("auto-enables reasoning for reasoning-capable model", func(t *testing.T) {
		var gotBody map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, minimalSSE)
		}))
		defer server.Close()

		p := &Provider{apiKey: "k", baseURL: server.URL, client: server.Client()}
		req := providers.CompleteRequest{
			// gpt-5 has Reasoning=true in modelsdev
			Model:    providers.ModelRef{ProviderID: "openai", ModelID: "gpt-5"},
			Messages: []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "x"}}}},
			// Reasoning intentionally unset — should be auto-detected
		}
		for _, err := range p.Complete(context.Background(), req) {
			if err != nil {
				t.Fatal(err)
			}
		}

		reasoning, ok := gotBody["reasoning"].(map[string]any)
		if !ok {
			t.Fatal("expected reasoning in request body")
		}
		if reasoning["effort"] != "high" {
			t.Errorf("expected effort 'high', got %v", reasoning["effort"])
		}
		include, _ := gotBody["include"].([]any)
		if len(include) == 0 || include[0] != "reasoning.encrypted_content" {
			t.Errorf("expected include reasoning.encrypted_content, got %v", include)
		}
	})

	t.Run("does not auto-enable reasoning for non-reasoning model", func(t *testing.T) {
		var gotBody map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, minimalSSE)
		}))
		defer server.Close()

		p := &Provider{apiKey: "k", baseURL: server.URL, client: server.Client()}
		req := providers.CompleteRequest{
			// gpt-4o has Reasoning=false in modelsdev
			Model:    providers.ModelRef{ProviderID: "openai", ModelID: "gpt-4o"},
			Messages: []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "x"}}}},
		}
		for _, err := range p.Complete(context.Background(), req) {
			if err != nil {
				t.Fatal(err)
			}
		}

		if _, ok := gotBody["reasoning"]; ok {
			t.Error("expected no reasoning in request body for non-reasoning model")
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
			Model:     providers.ModelRef{ProviderID: "openai", ModelID: "gpt-5"},
			Messages:  []message.Message{{Role: "user", Parts: []message.Part{message.TextPart{Text: "x"}}}},
			Reasoning: "disabled",
		}
		for _, err := range p.Complete(context.Background(), req) {
			if err != nil {
				t.Fatal(err)
			}
		}

		if _, ok := gotBody["reasoning"]; ok {
			t.Error("expected no reasoning block when reasoning explicitly disabled")
		}
	})
}
