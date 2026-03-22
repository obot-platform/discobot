// Package openaicompatible implements providers.Provider for the 60+ LLM
// providers that expose a standard OpenAI Chat Completions API
// (POST /chat/completions). These providers are identified in models.dev by
// npm: "@ai-sdk/openai-compatible".
//
// The init function reads models.dev metadata and registers a factory for
// every such provider, keyed by its models.dev ID (e.g. "deepseek",
// "fireworks-ai", "groq"). Each factory closes over the provider's default
// base URL and uses cfg.APIKey() / cfg.BaseURL() from credentials.
//
// Key differences from the providers/openai package (OpenAI Responses API):
//
//   - Endpoint: /chat/completions (not /responses)
//   - Messages: "messages" array with roles system/user/assistant/tool
//   - System prompt: role stays "system" (not "developer")
//   - Tools: nested under "function" key; tool results are role "tool"
//   - Streaming: standard SSE with choices[0].delta (no event: prefix lines)
//   - Reasoning: via delta.reasoning_content or delta.reasoning fields
//   - Usage: prompt_tokens / completion_tokens (not input_tokens / output_tokens)
//   - Token counting: non-streaming call with max_tokens=1 (no dedicated endpoint)
package openaicompatible

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
	"github.com/obot-platform/discobot/agent-go/providers/transport"
	"github.com/obot-platform/discobot/modelsdev"
)

func init() {
	for _, info := range modelsdev.ProvidersByNPM("@ai-sdk/openai-compatible") {
		info := info // capture loop variable
		providers.Register(info.ID, func(cfg providers.Config) (providers.Provider, error) {
			return newProvider(info.ID, info.API, cfg)
		})
	}
}

// Provider implements providers.Provider using the OpenAI Chat Completions API.
type Provider struct {
	id      string
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewProvider constructs a Provider for any OpenAI-compatible endpoint.
// id is used as the provider identifier; baseURL is the API root (e.g.
// "https://api.openai.com/v1"). cfg may override baseURL via cfg.BaseURL().
func NewProvider(id, baseURL string, cfg providers.Config) (*Provider, error) {
	return newProvider(id, baseURL, cfg)
}

func newProvider(providerID, defaultBaseURL string, cfg providers.Config) (*Provider, error) {
	apiKey := cfg.APIKey()
	if apiKey == "" {
		return nil, fmt.Errorf("%s: api_key is required", providerID)
	}
	baseURL := cfg.BaseURL()
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Provider{
		id:      providerID,
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  transport.NewClient(10 * time.Minute),
	}, nil
}

func (p *Provider) ID() string { return p.id }

// Complete sends a streaming chat completion request and yields response chunks.
func (p *Provider) Complete(ctx context.Context, req providers.CompleteRequest) iter.Seq2[message.ProviderMessageChunk, error] {
	return func(yield func(message.ProviderMessageChunk, error) bool) {
		msgs, err := convertMessages(req.Messages)
		if err != nil {
			yield(nil, fmt.Errorf("%s: convert messages: %w", p.id, err))
			return
		}

		body := map[string]any{
			"model":    req.Model.ModelID,
			"messages": msgs,
			"stream":   true,
			"stream_options": map[string]any{
				"include_usage": true,
			},
		}
		if tools := convertTools(req.Tools); len(tools) > 0 {
			body["tools"] = tools
			body["tool_choice"] = "auto"
		}
		if req.MaxTokens != nil {
			body["max_tokens"] = *req.MaxTokens
		}
		if req.Temperature != nil {
			body["temperature"] = *req.Temperature
		}
		if req.TopP != nil {
			body["top_p"] = *req.TopP
		}
		// reasoning_effort is the Chat Completions analogue of extended thinking.
		if effort := p.resolveReasoningEffort(req.Reasoning, req.Model.ModelID); effort != "" {
			body["reasoning_effort"] = effort
		}
		if req.ProviderOptions != nil {
			var opts map[string]any
			if json.Unmarshal(req.ProviderOptions, &opts) == nil {
				for k, v := range opts {
					body[k] = v
				}
			}
		}

		jsonBody, err := json.Marshal(body)
		if err != nil {
			yield(nil, fmt.Errorf("%s: marshal request: %w", p.id, err))
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(jsonBody))
		if err != nil {
			yield(nil, fmt.Errorf("%s: create request: %w", p.id, err))
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

		resp, err := p.client.Do(httpReq)
		if err != nil {
			yield(nil, fmt.Errorf("%s: request failed: %w", p.id, err))
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			yield(nil, fmt.Errorf("%s: API error %d: %s", p.id, resp.StatusCode, string(bodyBytes)))
			return
		}

		p.parseSSEStream(resp.Body, yield)
	}
}

// DefaultModels returns an empty map; openai-compatible providers don't have
// a universal default model — each provider's defaults should be configured
// via models.dev metadata or explicit user selection.
func (p *Provider) DefaultModels() map[string]providers.ModelRef {
	return map[string]providers.ModelRef{}
}

// ListModels fetches available models from GET /models and enriches them with
// metadata from models.dev (context window, max output tokens, reasoning support).
func (p *Provider) ListModels(ctx context.Context) ([]providers.ModelInfo, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("%s: create models request: %w", p.id, err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("%s: models request failed: %w", p.id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s: models API error %d: %s", p.id, resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("%s: decode models response: %w", p.id, err)
	}

	var models []providers.ModelInfo
	for _, m := range result.Data {
		info := providers.ModelInfo{ID: m.ID, DisplayName: m.ID}
		if md := modelsdev.Lookup(p.id, m.ID); md != nil {
			info.DisplayName = md.Name
			info.Reasoning = md.Reasoning
			info.ReasoningLevels = toReasoningSlice(md.ReasoningLevels)
			info.DefaultReasoning = providers.Reasoning(md.DefaultReasonLevel)
			info.ContextWindow = md.ContextWindow
			info.MaxOutputTokens = md.MaxOutputTokens
			// If the model is known to support reasoning but no levels are specified,
			// apply safe defaults so callers can present a choice.
			if md.Reasoning && len(info.ReasoningLevels) == 0 {
				info.ReasoningLevels = []providers.Reasoning{providers.ReasoningLow, providers.ReasoningMedium, providers.ReasoningHigh}
				info.DefaultReasoning = providers.ReasoningMedium
			}
		}
		models = append(models, info)
	}
	return models, nil
}

// --- Message conversion ---

// convertMessages converts internal messages to the Chat Completions messages
// array. System → system, user → user (string or content array), assistant →
// assistant (with optional tool_calls), tool → one "tool" message per result.
func convertMessages(msgs []message.Message) ([]map[string]any, error) {
	var result []map[string]any
	for _, msg := range msgs {
		converted, err := convertMessage(msg)
		if err != nil {
			return nil, err
		}
		result = append(result, converted...)
	}
	return result, nil
}

func convertMessage(msg message.Message) ([]map[string]any, error) {
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

func convertSystemMessage(msg message.Message) ([]map[string]any, error) {
	return []map[string]any{
		{"role": "system", "content": extractText(msg.Parts)},
	}, nil
}

func convertUserMessage(msg message.Message) ([]map[string]any, error) {
	parts := buildUserContent(msg.Parts)
	if len(parts) == 0 {
		return []map[string]any{{"role": "user", "content": ""}}, nil
	}
	// Single plain-text part: use the string shorthand for cleaner payloads.
	if len(parts) == 1 {
		if m, ok := parts[0].(map[string]any); ok && m["type"] == "text" {
			return []map[string]any{{"role": "user", "content": m["text"]}}, nil
		}
	}
	return []map[string]any{{"role": "user", "content": parts}}, nil
}

func buildUserContent(parts []message.Part) []any {
	var content []any
	for _, part := range parts {
		switch p := part.(type) {
		case message.TextPart:
			content = append(content, map[string]any{
				"type": "text",
				"text": p.Text,
			})
		case message.ImagePart:
			url := p.Image
			if !strings.HasPrefix(url, "http") && !strings.HasPrefix(url, "data:") {
				url = "data:" + p.MediaType + ";base64," + p.Image
			}
			content = append(content, map[string]any{
				"type":      "image_url",
				"image_url": map[string]any{"url": url},
			})
		case message.FilePart:
			if strings.HasPrefix(p.MediaType, "image/") {
				url := p.Data
				if !strings.HasPrefix(url, "http") && !strings.HasPrefix(url, "data:") {
					url = "data:" + p.MediaType + ";base64," + p.Data
				}
				content = append(content, map[string]any{
					"type":      "image_url",
					"image_url": map[string]any{"url": url},
				})
			} else {
				content = append(content, map[string]any{
					"type": "text",
					"text": fmt.Sprintf("[file: %s]\n%s", p.Filename, p.Data),
				})
			}
		}
	}
	return content
}

// convertAssistantMessage maps an assistant message to the Chat Completions
// format. Text content, optional reasoning_content, and tool_calls are all
// placed in a single message object. ReasoningPart.Text is sent as
// reasoning_content for providers that support it in multi-turn context.
func convertAssistantMessage(msg message.Message) ([]map[string]any, error) {
	m := map[string]any{"role": "assistant"}

	var textParts []string
	var reasoningText string
	var toolCalls []map[string]any

	for _, part := range msg.Parts {
		switch p := part.(type) {
		case message.TextPart:
			if p.Text != "" {
				textParts = append(textParts, p.Text)
			}
		case message.ReasoningPart:
			if p.Text != "" {
				reasoningText = p.Text
			}
		case message.ToolCallPart:
			toolCalls = append(toolCalls, map[string]any{
				"id":   p.ToolCallID,
				"type": "function",
				"function": map[string]any{
					"name":      p.ToolName,
					"arguments": string(p.Input),
				},
			})
		}
	}

	m["content"] = strings.Join(textParts, "\n")
	if reasoningText != "" {
		m["reasoning_content"] = reasoningText
	}
	if len(toolCalls) > 0 {
		m["tool_calls"] = toolCalls
	}
	return []map[string]any{m}, nil
}

// convertToolMessage maps each ToolResultPart to a separate "tool" role
// message. The Chat Completions API expects one message per tool result.
func convertToolMessage(msg message.Message) ([]map[string]any, error) {
	var msgs []map[string]any
	for _, part := range msg.Parts {
		tr, ok := part.(message.ToolResultPart)
		if !ok {
			continue
		}
		msgs = append(msgs, map[string]any{
			"role":         "tool",
			"tool_call_id": tr.ToolCallID,
			"content":      toolResultToString(tr.Output),
		})
	}
	return msgs, nil
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
			switch contentItem := item.(type) {
			case message.ContentTextItem:
				if contentItem.Text != "" {
					parts = append(parts, contentItem.Text)
				}
			case message.ContentImageDataItem:
				mediaType := contentItem.MediaType
				if mediaType == "" {
					mediaType = "image/*"
				}
				parts = append(parts, fmt.Sprintf("[image data omitted (%s)]", mediaType))
			case message.ContentFileDataItem:
				mediaType := contentItem.MediaType
				if mediaType == "" {
					mediaType = "application/octet-stream"
				}
				if contentItem.Filename != "" {
					parts = append(parts, fmt.Sprintf("[file data omitted (%s, filename=%s)]", mediaType, contentItem.Filename))
				} else {
					parts = append(parts, fmt.Sprintf("[file data omitted (%s)]", mediaType))
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

// --- Tool conversion ---

// convertTools maps ToolDefinition to the Chat Completions function tool format.
// Unlike the Responses API, the function's name/description/parameters are
// nested under a "function" key.
func convertTools(tools []providers.ToolDefinition) []map[string]any {
	if len(tools) == 0 {
		return nil
	}
	result := make([]map[string]any, len(tools))
	for i, t := range tools {
		fn := map[string]any{
			"name":       t.Name,
			"parameters": json.RawMessage(t.InputSchema),
		}
		if t.Description != "" {
			fn["description"] = t.Description
		}
		result[i] = map[string]any{
			"type":     "function",
			"function": fn,
		}
	}
	return result
}

// --- SSE stream parsing ---

// chatChunk is a single Chat Completions streaming chunk.
type chatChunk struct {
	ID      string       `json:"id"`
	Model   string       `json:"model"`
	Created int64        `json:"created"`
	Choices []chatChoice `json:"choices"`
	Usage   *chatUsage   `json:"usage"`
	// Error is present when the server streams an error object instead of a
	// normal chunk, e.g. {"error":{"message":"...","type":"server_error"}}.
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

type chatChoice struct {
	Index        int       `json:"index"`
	Delta        chatDelta `json:"delta"`
	FinishReason *string   `json:"finish_reason"`
}

type chatDelta struct {
	Role             string          `json:"role"`
	Content          string          `json:"content"`
	ReasoningContent string          `json:"reasoning_content"` // most providers
	Reasoning        string          `json:"reasoning"`         // some providers (e.g. DeepSeek R1 via proxy)
	ToolCalls        []toolCallDelta `json:"tool_calls"`
}

type toolCallDelta struct {
	Index    *int   `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type chatUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	PromptTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails struct {
		ReasoningTokens int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}

// streamState tracks in-flight state across SSE chunks.
type streamState struct {
	gotMetadata      bool
	textStarted      bool
	reasoningStarted bool
	reasoningEnded   bool
	// toolCalls maps tool_call index to its accumulated state.
	toolCalls map[int]*toolCallInfo
	// toolCallOrder tracks indices in the order they first appeared.
	toolCallOrder []int
	finishReason  string
	usage         *chatUsage
	hasToolCalls  bool
}

type toolCallInfo struct {
	callID string
}

// parseSSEStream reads Chat Completions SSE lines and dispatches chunks.
// Each data line is either "[DONE]" (end of stream) or a JSON chat chunk.
// Usage arrives in the final chunk before [DONE] when stream_options.include_usage is true.
func (p *Provider) parseSSEStream(body io.Reader, yield func(message.ProviderMessageChunk, error) bool) {
	state := &streamState{
		toolCalls: make(map[int]*toolCallInfo),
	}

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			p.emitFinish(state, yield) //nolint:errcheck // best-effort at stream end
			return
		}
		if !p.handleChunk([]byte(data), state, yield) {
			return
		}
	}

	if err := scanner.Err(); err != nil {
		yield(nil, fmt.Errorf("%s: read SSE stream: %w", p.id, err))
		return
	}
	// Some providers omit [DONE]; emit FinishChunk if we got any data.
	if state.gotMetadata {
		p.emitFinish(state, yield) //nolint:errcheck // best-effort at stream end
	}
}

// handleChunk processes a single parsed SSE data line.
func (p *Provider) handleChunk(data []byte, state *streamState, yield func(message.ProviderMessageChunk, error) bool) bool {
	var chunk chatChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		// Malformed chunks are silently skipped (some providers send comments).
		return true
	}

	// Some providers stream an error object instead of a normal chunk, e.g.:
	//   {"error":{"message":"Incorrect API key...","type":"invalid_request_error"}}
	if chunk.Error != nil {
		msg := chunk.Error.Message
		if msg == "" {
			msg = chunk.Error.Type
		}
		// Ensure stream-start is emitted before any other chunk.
		if !state.gotMetadata {
			state.gotMetadata = true
			if !yield(message.StreamStartChunk{}, nil) {
				return false
			}
		}
		return yield(message.ErrorChunk{ErrorText: msg, Err: fmt.Errorf("%s: stream error: %s", p.id, msg)}, nil)
	}

	// First chunk: emit StreamStart + ResponseMetadata.
	if !state.gotMetadata {
		state.gotMetadata = true
		if !yield(message.StreamStartChunk{}, nil) {
			return false
		}
		now := time.Now()
		if !yield(message.ResponseMetadataChunk{
			ID:        chunk.ID,
			Timestamp: &now,
			ModelID:   chunk.Model,
		}, nil) {
			return false
		}
	}

	// Accumulate usage whenever present (the final usage chunk has Choices=[]).
	if chunk.Usage != nil {
		state.usage = chunk.Usage
	}

	if len(chunk.Choices) == 0 {
		return true
	}

	choice := chunk.Choices[0]
	delta := choice.Delta

	// --- Reasoning content ---
	// Providers use either delta.reasoning_content or delta.reasoning.
	reasoning := delta.ReasoningContent
	if reasoning == "" {
		reasoning = delta.Reasoning
	}
	if reasoning != "" {
		if !state.reasoningStarted {
			state.reasoningStarted = true
			if !yield(message.ReasoningStartChunk{ID: "reasoning-0"}, nil) {
				return false
			}
		}
		if !yield(message.ReasoningDeltaChunk{ID: "reasoning-0", Delta: reasoning}, nil) {
			return false
		}
	}

	// --- Text content ---
	if delta.Content != "" {
		// Close reasoning block when text content begins.
		if state.reasoningStarted && !state.reasoningEnded {
			state.reasoningEnded = true
			if !yield(message.ReasoningEndChunk{ID: "reasoning-0"}, nil) {
				return false
			}
		}
		if !state.textStarted {
			state.textStarted = true
			if !yield(message.TextStartChunk{ID: "txt-0"}, nil) {
				return false
			}
		}
		if !yield(message.TextDeltaChunk{ID: "txt-0", Delta: delta.Content}, nil) {
			return false
		}
	}

	// --- Tool calls ---
	for _, tc := range delta.ToolCalls {
		idx := 0
		if tc.Index != nil {
			idx = *tc.Index
		}
		if _, exists := state.toolCalls[idx]; !exists {
			state.toolCalls[idx] = &toolCallInfo{callID: tc.ID}
			state.toolCallOrder = append(state.toolCallOrder, idx)
			state.hasToolCalls = true
			if !yield(message.ToolInputStartChunk{
				ToolCallID: tc.ID,
				ToolName:   tc.Function.Name,
			}, nil) {
				return false
			}
		}
		if tc.Function.Arguments != "" {
			info := state.toolCalls[idx]
			if !yield(message.ToolInputDeltaChunk{
				ToolCallID:     info.callID,
				InputTextDelta: tc.Function.Arguments,
			}, nil) {
				return false
			}
		}
	}

	// Record finish_reason from this choice (may be set before [DONE]).
	if choice.FinishReason != nil && *choice.FinishReason != "" {
		state.finishReason = *choice.FinishReason
	}

	return true
}

// emitFinish closes any open streaming blocks and emits the final FinishChunk.
func (p *Provider) emitFinish(state *streamState, yield func(message.ProviderMessageChunk, error) bool) bool {
	if state.textStarted {
		if !yield(message.TextEndChunk{ID: "txt-0"}, nil) {
			return false
		}
	}
	if state.reasoningStarted && !state.reasoningEnded {
		if !yield(message.ReasoningEndChunk{ID: "reasoning-0"}, nil) {
			return false
		}
	}
	// Emit ToolInputEndChunk for each tool call in the order they started.
	indices := make([]int, len(state.toolCallOrder))
	copy(indices, state.toolCallOrder)
	sort.Ints(indices)
	for _, idx := range indices {
		info := state.toolCalls[idx]
		if !yield(message.ToolInputEndChunk{ToolCallID: info.callID}, nil) {
			return false
		}
	}

	var usage message.Usage
	if state.usage != nil {
		cached := state.usage.PromptTokensDetails.CachedTokens
		reasoning := state.usage.CompletionTokensDetails.ReasoningTokens
		noCache := state.usage.PromptTokens - cached
		if noCache < 0 {
			noCache = 0
		}
		textOut := state.usage.CompletionTokens - reasoning
		if textOut < 0 {
			textOut = 0
		}
		usage = message.Usage{
			InputTokens: message.InputTokens{
				Total:     state.usage.PromptTokens,
				CacheRead: cached,
				NoCache:   noCache,
			},
			OutputTokens: message.OutputTokens{
				Total:     state.usage.CompletionTokens,
				Reasoning: reasoning,
				Text:      textOut,
			},
		}
	}

	return yield(message.FinishChunk{
		FinishReason: message.FinishReason{
			Unified: mapFinishReason(state.finishReason, state.hasToolCalls),
			Raw:     state.finishReason,
		},
		Usage: usage,
	}, nil)
}

// mapFinishReason converts a Chat Completions finish_reason to the unified
// internal finish reason string. If tool calls were observed in the stream,
// the result is always "tool-calls" regardless of the reported reason.
func mapFinishReason(reason string, hasToolCalls bool) string {
	if hasToolCalls {
		return "tool-calls"
	}
	switch reason {
	case "stop":
		return "stop"
	case "length":
		return "length"
	case "content_filter":
		return "content-filter"
	case "tool_calls", "function_call":
		return "tool-calls"
	default:
		return "other"
	}
}

// resolveReasoningEffort maps a Reasoning request to a reasoning_effort string
// for the Chat Completions API. Returns "" if reasoning_effort should be omitted.
func (p *Provider) resolveReasoningEffort(r providers.Reasoning, modelID string) string {
	switch r {
	case providers.ReasoningDisabled, providers.ReasoningNone:
		return "" // omit reasoning_effort
	case providers.ReasoningEmpty, providers.ReasoningDefault:
		// Auto-detect from model metadata.
		if md := modelsdev.Lookup(p.id, modelID); md != nil {
			if md.DefaultReasonLevel != "" {
				return reasoningEffortString(providers.Reasoning(md.DefaultReasonLevel))
			}
			if md.Reasoning {
				return "high" // legacy: default to high
			}
		}
		return ""
	case providers.ReasoningLow:
		return "low"
	case providers.ReasoningMedium:
		return "medium"
	case providers.ReasoningHigh:
		return "high"
	case providers.ReasoningXHigh:
		return "xhigh"
	default: // auto, enabled → high
		return "high"
	}
}

// reasoningEffortString maps a Reasoning level to an effort string.
func reasoningEffortString(r providers.Reasoning) string {
	switch r {
	case providers.ReasoningLow:
		return "low"
	case providers.ReasoningMedium:
		return "medium"
	case providers.ReasoningHigh:
		return "high"
	case providers.ReasoningXHigh:
		return "xhigh"
	case providers.ReasoningEmpty, providers.ReasoningDisabled, providers.ReasoningNone:
		return ""
	default:
		return "high"
	}
}

// toReasoningSlice converts a []string from models.dev into []providers.Reasoning.
func toReasoningSlice(ss []string) []providers.Reasoning {
	if len(ss) == 0 {
		return nil
	}
	out := make([]providers.Reasoning, len(ss))
	for i, s := range ss {
		out[i] = providers.Reasoning(s)
	}
	return out
}
