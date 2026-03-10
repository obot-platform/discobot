package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strings"
	"time"

	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
	"github.com/obot-platform/discobot/agent-go/providers/modelsdev"
	"github.com/obot-platform/discobot/agent-go/providers/transport"
)

const (
	providerID            = "openai"
	defaultBaseURL        = "https://api.openai.com/v1"
	missingToolOutputText = "interrupted by transient system failure"
)

func init() {
	providers.Register(providerID, New)
}

// Provider implements providers.Provider using the OpenAI Responses API
// (POST /v1/responses).
type Provider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// New creates a new OpenAI Responses API provider.
func New(cfg providers.Config) (providers.Provider, error) {
	apiKey := cfg.APIKey()
	if apiKey == "" {
		return nil, fmt.Errorf("openai: api_key is required")
	}
	baseURL := cfg.BaseURL()
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Provider{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  transport.NewClient(10 * time.Minute),
	}, nil
}

func (p *Provider) ID() string { return providerID }

func (p *Provider) Complete(ctx context.Context, req providers.CompleteRequest) iter.Seq2[message.ProviderMessageChunk, error] {
	return func(yield func(message.ProviderMessageChunk, error) bool) {
		inputItems, err := convertMessages(req.Messages)
		if err != nil {
			yield(nil, fmt.Errorf("openai: convert messages: %w", err))
			return
		}

		body := map[string]any{
			"model":      req.Model.ModelID,
			"input":      inputItems,
			"stream":     true,
			"store":      false,
			"truncation": "disabled",
		}
		if tools := convertTools(req.Tools); len(tools) > 0 {
			body["tools"] = tools
		}
		if req.MaxTokens != nil {
			body["max_output_tokens"] = *req.MaxTokens
		}
		if req.Temperature != nil {
			body["temperature"] = *req.Temperature
		}
		if req.TopP != nil {
			body["top_p"] = *req.TopP
		}
		// Determine effective reasoning: explicit "enabled"/"disabled", or auto-detect
		// from model capability when unset (matching Claude CLI default behaviour).
		effectiveReasoning := req.Reasoning
		if effectiveReasoning == "" {
			if md := modelsdev.Lookup(providerID, req.Model.ModelID); md != nil && md.Reasoning {
				effectiveReasoning = "enabled"
			}
		}
		if effectiveReasoning == "enabled" {
			body["reasoning"] = map[string]any{
				"effort":  "high",
				"summary": "detailed",
			}
			body["include"] = []string{"reasoning.encrypted_content"}
		}

		jsonBody, err := json.Marshal(body)
		if err != nil {
			yield(nil, fmt.Errorf("openai: marshal request: %w", err))
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/responses", bytes.NewReader(jsonBody))
		if err != nil {
			yield(nil, fmt.Errorf("openai: create request: %w", err))
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

		resp, err := p.client.Do(httpReq)
		if err != nil {
			yield(nil, fmt.Errorf("openai: request failed: %w", err))
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			_, parseErr := parseError(resp.StatusCode, bodyBytes)
			yield(nil, parseErr)
			return
		}

		parseSSEStream(resp.Body, yield)
	}
}

func (p *Provider) CountTokens(ctx context.Context, req providers.CountTokensRequest) (providers.CountTokensResponse, error) {
	inputItems, err := convertMessages(req.Messages)
	if err != nil {
		return providers.CountTokensResponse{}, fmt.Errorf("openai: convert messages: %w", err)
	}

	body := map[string]any{
		"model": req.Model.ModelID,
		"input": inputItems,
	}
	if tools := convertTools(req.Tools); len(tools) > 0 {
		body["tools"] = tools
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return providers.CountTokensResponse{}, fmt.Errorf("openai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/responses/input_tokens", bytes.NewReader(jsonBody))
	if err != nil {
		return providers.CountTokensResponse{}, fmt.Errorf("openai: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return providers.CountTokensResponse{}, fmt.Errorf("openai: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return providers.CountTokensResponse{}, fmt.Errorf("openai: API error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		InputTokens int `json:"input_tokens"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return providers.CountTokensResponse{}, fmt.Errorf("openai: decode response: %w", err)
	}

	return providers.CountTokensResponse{TotalTokens: result.InputTokens}, nil
}

func (p *Provider) DefaultModels() map[string]providers.ModelRef {
	return map[string]providers.ModelRef{
		providers.ModelTaskChat: {ProviderID: providerID, ModelID: "gpt-5.3-codex"},
	}
}

func (p *Provider) ListModels(ctx context.Context) ([]providers.ModelInfo, error) {
	// Fetch live model IDs from the OpenAI API.
	httpReq, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("openai: create models request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: models request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai: models API error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("openai: decode models response: %w", err)
	}

	// Enrich each model with metadata from models.dev (context window,
	// max output tokens, reasoning, display name).
	var models []providers.ModelInfo
	for _, m := range result.Data {
		info := providers.ModelInfo{ID: m.ID, DisplayName: m.ID}
		if md := modelsdev.Lookup(providerID, m.ID); md != nil {
			info.DisplayName = md.Name
			info.Reasoning = md.Reasoning
			info.ContextWindow = md.ContextWindow
			info.MaxOutputTokens = md.MaxOutputTokens
		}
		models = append(models, info)
	}
	return models, nil
}

// --- Message conversion ---

// convertMessages converts internal messages to OpenAI Responses API input items.
// The Responses API input array contains role-based messages (user, developer)
// and typed items (function_call, function_call_output, message).
func convertMessages(msgs []message.Message) ([]json.RawMessage, error) {
	var items []json.RawMessage
	for _, msg := range msgs {
		converted, err := convertMessage(msg)
		if err != nil {
			return nil, err
		}
		items = append(items, converted...)
	}
	return ensureFunctionCallOutputs(items), nil
}

// ensureFunctionCallOutputs appends synthetic function_call_output items for
// any function_call items that are still unresolved at the end of the input.
//
// OpenAI Responses rejects requests when a function_call has no matching
// function_call_output. This guard keeps crashed/interrupted histories usable by
// synthesizing a deterministic error output for unresolved calls.
func ensureFunctionCallOutputs(items []json.RawMessage) []json.RawMessage {
	type itemHeader struct {
		Type   string `json:"type"`
		CallID string `json:"call_id"`
	}

	pendingOrder := make([]string, 0)
	pending := make(map[string]struct{})

	for _, item := range items {
		var header itemHeader
		if err := json.Unmarshal(item, &header); err != nil {
			continue
		}
		switch header.Type {
		case "function_call":
			if header.CallID == "" {
				continue
			}
			if _, exists := pending[header.CallID]; !exists {
				pendingOrder = append(pendingOrder, header.CallID)
			}
			pending[header.CallID] = struct{}{}
		case "function_call_output":
			delete(pending, header.CallID)
		}
	}

	if len(pending) == 0 {
		return items
	}

	result := make([]json.RawMessage, 0, len(items)+len(pending))
	result = append(result, items...)
	for _, callID := range pendingOrder {
		if _, ok := pending[callID]; !ok {
			continue
		}
		data, err := json.Marshal(map[string]any{
			"type":    "function_call_output",
			"call_id": callID,
			"output":  missingToolOutputText,
		})
		if err != nil {
			continue
		}
		result = append(result, data)
	}
	return result
}

func convertMessage(msg message.Message) ([]json.RawMessage, error) {
	switch msg.Role {
	case "system":
		return convertSystemMessage(msg)
	case "user":
		return convertUserMessage(msg)
	case "assistant":
		return convertAssistantMessage(msg)
	case "tool":
		return convertToolMessage(msg)
	default:
		return nil, fmt.Errorf("unknown message role: %q", msg.Role)
	}
}

// convertSystemMessage maps system → developer role (OpenAI convention).
func convertSystemMessage(msg message.Message) ([]json.RawMessage, error) {
	text := extractText(msg.Parts)
	data, err := json.Marshal(map[string]any{
		"role":    "developer",
		"content": text,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal system message: %w", err)
	}
	return []json.RawMessage{data}, nil
}

func convertUserMessage(msg message.Message) ([]json.RawMessage, error) {
	content := convertUserContent(msg.Parts)
	var item any
	if len(content) == 1 {
		if textItem, ok := content[0].(map[string]any); ok && textItem["type"] == "input_text" {
			// Single text → use simple string content.
			item = map[string]any{"role": "user", "content": textItem["text"]}
		} else {
			item = map[string]any{"role": "user", "content": content}
		}
	} else if len(content) == 0 {
		item = map[string]any{"role": "user", "content": ""}
	} else {
		item = map[string]any{"role": "user", "content": content}
	}
	data, err := json.Marshal(item)
	if err != nil {
		return nil, fmt.Errorf("marshal user message: %w", err)
	}
	return []json.RawMessage{data}, nil
}

// convertAssistantMessage splits an assistant message into reasoning items,
// a typed "message" item (for text), and separate tool items
// (function_call/function_call_output). Items are ordered:
// reasoning → message → function_calls → function_call_outputs.
func convertAssistantMessage(msg message.Message) ([]json.RawMessage, error) {
	var reasoningItems []json.RawMessage
	var functionCallItems []json.RawMessage
	var functionCallOutputItems []json.RawMessage
	var textParts []map[string]any
	var messageItemID string // ID of the parent message output item

	for _, part := range msg.Parts {
		switch p := part.(type) {
		case message.ReasoningPart:
			data, err := convertReasoningPart(p)
			if err != nil {
				return nil, err
			}
			if data != nil {
				reasoningItems = append(reasoningItems, data)
			}
		case message.TextPart:
			if p.Text != "" {
				textParts = append(textParts, map[string]any{
					"type": "output_text",
					"text": p.Text,
				})
			}
			if p.ID != "" && messageItemID == "" {
				messageItemID = p.ID
			}
		case message.ToolCallPart:
			data, err := json.Marshal(map[string]any{
				"type":      "function_call",
				"call_id":   p.ToolCallID,
				"name":      p.ToolName,
				"arguments": p.Input,
			})
			if err != nil {
				return nil, fmt.Errorf("marshal function_call: %w", err)
			}
			functionCallItems = append(functionCallItems, data)
		case message.ToolResultPart:
			data, err := json.Marshal(map[string]any{
				"type":    "function_call_output",
				"call_id": p.ToolCallID,
				"output":  toolResultToString(p.Output),
			})
			if err != nil {
				return nil, fmt.Errorf("marshal function_call_output: %w", err)
			}
			functionCallOutputItems = append(functionCallOutputItems, data)
		}
	}

	// Order: reasoning → text message → function calls → function call outputs.
	var items []json.RawMessage
	items = append(items, reasoningItems...)
	if len(textParts) > 0 {
		msgItem := map[string]any{
			"type":    "message",
			"role":    "assistant",
			"content": textParts,
		}
		// Note: do NOT include "id" or "status" here. With store=false, the
		// API does not persist output items, so referencing their IDs in
		// subsequent requests causes a 404 lookup error.
		data, err := json.Marshal(msgItem)
		if err != nil {
			return nil, fmt.Errorf("marshal assistant message: %w", err)
		}
		items = append(items, data)
	}
	items = append(items, functionCallItems...)
	items = append(items, functionCallOutputItems...)

	return items, nil
}

// convertReasoningPart converts a ReasoningPart to a Responses API reasoning
// input item. If ProviderMetadata is OpenAI-format (type "reasoning", contains
// encrypted_content), it is used directly after stripping the "id" field.
// Metadata from a different provider is ignored and a summary-only item is
// constructed from p.Text instead, allowing cross-provider threads to work.
func convertReasoningPart(p message.ReasoningPart) (json.RawMessage, error) {
	if p.MetadataType() == "reasoning" {
		// OpenAI-native: extract the inner block from the nested format
		// {"openai": {...}} and pass through with encrypted_content intact.
		var nested map[string]json.RawMessage
		if json.Unmarshal(p.ProviderMetadata, &nested) != nil {
			return nil, nil
		}
		openaiMeta, ok := nested[providerID]
		if !ok {
			return nil, nil
		}
		// Strip "id" since with store=false the API doesn't persist items and
		// referencing their IDs causes a 404 lookup error.
		var item map[string]json.RawMessage
		if err := json.Unmarshal(openaiMeta, &item); err != nil {
			return openaiMeta, nil
		}
		delete(item, "id")
		stripped, err := json.Marshal(item)
		if err != nil {
			return openaiMeta, nil
		}
		return stripped, nil
	}
	if p.Text == "" {
		return nil, nil
	}
	data, err := json.Marshal(map[string]any{
		"type": "reasoning",
		"summary": []map[string]string{
			{"type": "summary_text", "text": p.Text},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal reasoning: %w", err)
	}
	return data, nil
}

// convertToolMessage maps each ToolResultPart to a "function_call_output" item.
func convertToolMessage(msg message.Message) ([]json.RawMessage, error) {
	var items []json.RawMessage
	for _, part := range msg.Parts {
		tr, ok := part.(message.ToolResultPart)
		if !ok {
			continue
		}
		data, err := json.Marshal(map[string]any{
			"type":    "function_call_output",
			"call_id": tr.ToolCallID,
			"output":  toolResultToString(tr.Output),
		})
		if err != nil {
			return nil, fmt.Errorf("marshal function_call_output: %w", err)
		}
		items = append(items, data)
	}
	return items, nil
}

// convertUserContent converts message parts to Responses API content items.
func convertUserContent(parts []message.Part) []any {
	var content []any
	for _, part := range parts {
		switch p := part.(type) {
		case message.TextPart:
			content = append(content, map[string]any{
				"type": "input_text",
				"text": p.Text,
			})
		case message.ImagePart:
			url := p.Image
			if !strings.HasPrefix(url, "http") && !strings.HasPrefix(url, "data:") {
				url = "data:" + p.MediaType + ";base64," + p.Image
			}
			content = append(content, map[string]any{
				"type":      "input_image",
				"image_url": url,
			})
		case message.FilePart:
			if strings.HasPrefix(p.MediaType, "image/") {
				url := p.Data
				if !strings.HasPrefix(url, "http") && !strings.HasPrefix(url, "data:") {
					url = "data:" + p.MediaType + ";base64," + p.Data
				}
				content = append(content, map[string]any{
					"type":      "input_image",
					"image_url": url,
				})
			} else {
				content = append(content, map[string]any{
					"type": "input_text",
					"text": fmt.Sprintf("[file: %s]\n%s", p.Filename, p.Data),
				})
			}
		}
	}
	return content
}

func extractText(parts []message.Part) string {
	var texts []string
	for _, part := range parts {
		if tp, ok := part.(message.TextPart); ok {
			texts = append(texts, tp.Text)
		}
	}
	return strings.Join(texts, "\n")
}

func toolResultToString(output message.ToolResultOutput) string {
	switch v := output.(type) {
	case message.TextOutput:
		return v.Value
	case message.JSONOutput:
		return string(v.Value)
	case message.ErrorTextOutput:
		return v.Value
	case message.ErrorJSONOutput:
		return string(v.Value)
	case message.ExecutionDeniedOutput:
		if v.Reason != "" {
			return "Execution denied: " + v.Reason
		}
		return "Execution denied"
	case message.ContentOutput:
		var parts []string
		for _, item := range v.Value {
			if textItem, ok := item.(message.ContentTextItem); ok {
				parts = append(parts, textItem.Text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

// --- Tool conversion ---

// convertTools maps our ToolDefinition to Responses API function tools.
// In the Responses API, tool fields (name, description, parameters) are
// at the top level — NOT nested under a "function" key.
func convertTools(tools []providers.ToolDefinition) []map[string]any {
	if len(tools) == 0 {
		return nil
	}
	result := make([]map[string]any, len(tools))
	for i, t := range tools {
		tool := map[string]any{
			"type":       "function",
			"name":       t.Name,
			"parameters": json.RawMessage(t.InputSchema),
		}
		if t.Description != "" {
			tool["description"] = t.Description
		}
		result[i] = tool
	}
	return result
}

// --- SSE stream parsing ---

// streamState holds per-stream state needed across SSE events.
// The OpenAI Responses API emits call_id in response.output_item.added but
// omits it (empty string) in subsequent response.function_call_arguments.delta
// and response.function_call_arguments.done events, which only carry item_id.
// We track the item_id → call_id mapping here so delta/done handlers can
// resolve the correct call_id.
type streamState struct {
	itemCallIDs map[string]string // item_id → call_id for function_call items
}

// parseSSEStream reads Server-Sent Events from the response body and
// dispatches each event to handleSSEEvent.
func parseSSEStream(body io.Reader, yield func(message.ProviderMessageChunk, error) bool) {
	state := &streamState{itemCallIDs: make(map[string]string)}
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var eventType string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if eventType != "" {
				if !state.handleSSEEvent(eventType, []byte(data), yield) {
					return
				}
				eventType = ""
			}
			continue
		}
		if line == "" {
			eventType = ""
		}
	}
	if err := scanner.Err(); err != nil {
		yield(nil, fmt.Errorf("openai: read SSE stream: %w", err))
	}
}

func (s *streamState) handleSSEEvent(eventType string, data []byte, yield func(message.ProviderMessageChunk, error) bool) bool {
	switch eventType {
	case "response.created":
		return handleResponseCreated(data, yield)
	case "response.output_item.added":
		return s.handleOutputItemAdded(data, yield)
	case "response.output_item.done":
		return handleOutputItemDone(data, yield)
	case "response.content_part.added":
		return handleContentPartAdded(data, yield)
	case "response.output_text.delta":
		return handleTextDelta(data, yield)
	case "response.output_text.done":
		return handleTextDone(data, yield)
	case "response.function_call_arguments.delta":
		return s.handleFunctionCallDelta(data, yield)
	case "response.function_call_arguments.done":
		return s.handleFunctionCallDone(data, yield)
	case "response.reasoning_summary_text.delta":
		return handleReasoningDelta(data, yield)
	case "response.completed":
		return handleResponseCompleted(data, yield)
	case "response.incomplete":
		return handleResponseIncomplete(data, yield)
	case "response.failed":
		return handleResponseFailed(data, yield)
	case "error":
		return handleStreamError(data, yield)
	default:
		// Silently ignore unhandled events (response.in_progress,
		// response.content_part.done, response.reasoning_summary_part.*,
		// response.queued, etc.)
		return true
	}
}

func handleResponseCreated(data []byte, yield func(message.ProviderMessageChunk, error) bool) bool {
	var event struct {
		Response struct {
			ID    string `json:"id"`
			Model string `json:"model"`
		} `json:"response"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return yield(nil, fmt.Errorf("openai: parse response.created: %w", err))
	}
	if !yield(message.StreamStartChunk{}, nil) {
		return false
	}
	now := time.Now()
	return yield(message.ResponseMetadataChunk{
		ID:        event.Response.ID,
		Timestamp: &now,
		ModelID:   event.Response.Model,
	}, nil)
}

func (s *streamState) handleOutputItemAdded(data []byte, yield func(message.ProviderMessageChunk, error) bool) bool {
	var event struct {
		Item struct {
			ID     string `json:"id"`
			Type   string `json:"type"`
			Name   string `json:"name"`
			CallID string `json:"call_id"`
		} `json:"item"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return yield(nil, fmt.Errorf("openai: parse output_item.added: %w", err))
	}
	switch event.Item.Type {
	case "function_call":
		// Store item_id → call_id so delta/done events can resolve call_id
		// (the real API emits empty call_id in those events, only item_id).
		s.itemCallIDs[event.Item.ID] = event.Item.CallID
		return yield(message.ToolInputStartChunk{
			ToolCallID: event.Item.CallID,
			ToolName:   event.Item.Name,
		}, nil)
	case "reasoning":
		return yield(message.ReasoningStartChunk{ID: event.Item.ID}, nil)
	}
	return true
}

// handleOutputItemDone emits ReasoningEndChunk when a reasoning output item
// completes. The full reasoning item (including encrypted_content and summary)
// is preserved in ProviderMetadata so it can be sent back in multi-turn
// conversations. Other item types (message, function_call) are ignored since
// their content is finalized by more specific done events.
func handleOutputItemDone(data []byte, yield func(message.ProviderMessageChunk, error) bool) bool {
	var event struct {
		Item json.RawMessage `json:"item"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return yield(nil, fmt.Errorf("openai: parse output_item.done: %w", err))
	}
	var itemHeader struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(event.Item, &itemHeader); err != nil {
		return yield(nil, fmt.Errorf("openai: parse output_item.done item: %w", err))
	}
	if itemHeader.Type == "reasoning" {
		// Wrap the reasoning item under "openai" for the Vercel AI SDK v6
		// nested providerMetadata format: {"openai": {...}}.
		wrapped, wrapErr := json.Marshal(map[string]json.RawMessage{
			providerID: event.Item,
		})
		if wrapErr != nil {
			wrapped = event.Item // fallback to flat format on marshal error
		}
		return yield(message.ReasoningEndChunk{
			ID:               itemHeader.ID,
			ProviderMetadata: wrapped,
		}, nil)
	}
	return true
}

func handleContentPartAdded(data []byte, yield func(message.ProviderMessageChunk, error) bool) bool {
	var event struct {
		Part struct {
			Type string `json:"type"`
		} `json:"part"`
		ItemID string `json:"item_id"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return yield(nil, fmt.Errorf("openai: parse content_part.added: %w", err))
	}
	if event.Part.Type == "output_text" {
		return yield(message.TextStartChunk{ID: event.ItemID}, nil)
	}
	return true
}

func handleTextDelta(data []byte, yield func(message.ProviderMessageChunk, error) bool) bool {
	var event struct {
		ItemID string `json:"item_id"`
		Delta  string `json:"delta"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return yield(nil, fmt.Errorf("openai: parse output_text.delta: %w", err))
	}
	return yield(message.TextDeltaChunk{ID: event.ItemID, Delta: event.Delta}, nil)
}

func handleTextDone(data []byte, yield func(message.ProviderMessageChunk, error) bool) bool {
	var event struct {
		ItemID string `json:"item_id"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return yield(nil, fmt.Errorf("openai: parse output_text.done: %w", err))
	}
	return yield(message.TextEndChunk{ID: event.ItemID}, nil)
}

func (s *streamState) handleFunctionCallDelta(data []byte, yield func(message.ProviderMessageChunk, error) bool) bool {
	var event struct {
		ItemID string `json:"item_id"`
		CallID string `json:"call_id"`
		Delta  string `json:"delta"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return yield(nil, fmt.Errorf("openai: parse function_call_arguments.delta: %w", err))
	}
	callID := event.CallID
	if callID == "" {
		callID = s.itemCallIDs[event.ItemID]
	}
	return yield(message.ToolInputDeltaChunk{
		ToolCallID:     callID,
		InputTextDelta: event.Delta,
	}, nil)
}

func (s *streamState) handleFunctionCallDone(data []byte, yield func(message.ProviderMessageChunk, error) bool) bool {
	var event struct {
		ItemID string `json:"item_id"`
		CallID string `json:"call_id"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return yield(nil, fmt.Errorf("openai: parse function_call_arguments.done: %w", err))
	}
	callID := event.CallID
	if callID == "" {
		callID = s.itemCallIDs[event.ItemID]
	}
	return yield(message.ToolInputEndChunk{ToolCallID: callID}, nil)
}

func handleReasoningDelta(data []byte, yield func(message.ProviderMessageChunk, error) bool) bool {
	var event struct {
		ItemID string `json:"item_id"`
		Delta  string `json:"delta"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return yield(nil, fmt.Errorf("openai: parse reasoning_summary_text.delta: %w", err))
	}
	return yield(message.ReasoningDeltaChunk{ID: event.ItemID, Delta: event.Delta}, nil)
}

func handleResponseCompleted(data []byte, yield func(message.ProviderMessageChunk, error) bool) bool {
	var event struct {
		Response struct {
			Status string `json:"status"`
			Output []struct {
				Type string `json:"type"`
			} `json:"output"`
			Usage responseUsage `json:"usage"`
		} `json:"response"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return yield(nil, fmt.Errorf("openai: parse response.completed: %w", err))
	}

	hasToolCalls := false
	for _, item := range event.Response.Output {
		if item.Type == "function_call" {
			hasToolCalls = true
			break
		}
	}

	return yield(message.FinishChunk{
		FinishReason: message.FinishReason{
			Unified: unifyFinishReason(event.Response.Status, hasToolCalls),
			Raw:     event.Response.Status,
		},
		Usage: convertUsage(event.Response.Usage),
	}, nil)
}

func handleResponseIncomplete(data []byte, yield func(message.ProviderMessageChunk, error) bool) bool {
	var event struct {
		Response struct {
			Usage responseUsage `json:"usage"`
		} `json:"response"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return yield(nil, fmt.Errorf("openai: parse response.incomplete: %w", err))
	}
	return yield(message.FinishChunk{
		FinishReason: message.FinishReason{Unified: "length", Raw: "incomplete"},
		Usage:        convertUsage(event.Response.Usage),
	}, nil)
}

func handleResponseFailed(data []byte, yield func(message.ProviderMessageChunk, error) bool) bool {
	var event struct {
		Response struct {
			Error struct {
				Message string `json:"message"`
				Code    string `json:"code"`
			} `json:"error"`
		} `json:"response"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return yield(nil, fmt.Errorf("openai: parse response.failed: %w", err))
	}
	errMsg := event.Response.Error.Message
	if errMsg == "" {
		errMsg = "unknown error"
	}
	return yield(nil, fmt.Errorf("openai: response failed: %s", errMsg))
}

func handleStreamError(data []byte, yield func(message.ProviderMessageChunk, error) bool) bool {
	var event struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return yield(nil, fmt.Errorf("openai: parse error event: %w", err))
	}
	msg := event.Error.Message
	if msg == "" {
		msg = event.Error.Type
	}
	if msg == "" {
		msg = string(data)
	}
	if event.Error.Code != "" {
		return yield(nil, fmt.Errorf("openai: stream error: %s (code: %s)", msg, event.Error.Code))
	}
	return yield(nil, fmt.Errorf("openai: stream error: %s", msg))
}

// --- Usage conversion ---

type responseUsage struct {
	InputTokens        int `json:"input_tokens"`
	InputTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputTokens        int `json:"output_tokens"`
	OutputTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}

func convertUsage(u responseUsage) message.Usage {
	return message.Usage{
		InputTokens: message.InputTokens{
			Total:     u.InputTokens,
			CacheRead: u.InputTokensDetails.CachedTokens,
			NoCache:   u.InputTokens - u.InputTokensDetails.CachedTokens,
		},
		OutputTokens: message.OutputTokens{
			Total:     u.OutputTokens,
			Reasoning: u.OutputTokensDetails.ReasoningTokens,
			Text:      u.OutputTokens - u.OutputTokensDetails.ReasoningTokens,
		},
	}
}

func unifyFinishReason(status string, hasToolCalls bool) string {
	if hasToolCalls {
		return "tool-calls"
	}
	switch status {
	case "completed":
		return "stop"
	case "incomplete":
		return "length"
	case "failed":
		return "error"
	default:
		return "other"
	}
}

// parseError converts a non-200 OpenAI API response into a descriptive error.
// OpenAI error bodies have the form:
//
//	{"error":{"message":"...","type":"...","code":"..."}}
//
// The error is retriable for 429 and 5xx status codes.
func parseError(statusCode int, body []byte) (bool, error) {
	msg := extractErrorMessage(body)
	retriable := statusCode == http.StatusTooManyRequests || statusCode >= 500
	return retriable, fmt.Errorf("openai: API error %d: %s", statusCode, msg)
}

// extractErrorMessage parses the human-readable message from an OpenAI error body.
// Falls back to the raw body string when the structure cannot be parsed.
func extractErrorMessage(body []byte) string {
	var apiErr struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Error.Message != "" {
		if apiErr.Error.Type != "" {
			return apiErr.Error.Type + ": " + apiErr.Error.Message
		}
		return apiErr.Error.Message
	}
	return string(body)
}
