package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
	"github.com/obot-platform/discobot/agent-go/providers/modelsdev"
	"github.com/obot-platform/discobot/agent-go/providers/transport"
)

const (
	providerID         = "anthropic"
	defaultBaseURL     = "https://api.anthropic.com/v1"
	apiVersion         = "2023-06-01"
	defaultMaxTokens   = 8192
	reasoningMaxTokens = 16000
	reasoningBudget    = 10000
	thinkingBetaHeader = "interleaved-thinking-2025-05-14"
	oauthBetaHeader    = "oauth-2025-04-20"
)

func init() {
	providers.Register(providerID, New)
}

// supportsAdaptiveThinking reports whether the model uses the adaptive
// thinking API (claude-*-4-6 and later, e.g. "claude-sonnet-4-6").
// Adaptive thinking does not require a beta header and replaces the
// deprecated type:"enabled"+budget_tokens approach.
//
// New-style model IDs end with a short major-minor pair (e.g. "4-6").
// Old-style IDs append a date suffix (e.g. "claude-haiku-4-5-20251001");
// we detect dates by rejecting trailing segments longer than 4 characters.
func supportsAdaptiveThinking(modelID string) bool {
	parts := strings.Split(modelID, "-")
	if len(parts) < 2 {
		return false
	}
	majorStr := parts[len(parts)-2]
	minorStr := parts[len(parts)-1]
	// Date suffixes like "20251001" are 8 chars; real minor versions are ≤4.
	if len(majorStr) > 4 || len(minorStr) > 4 {
		return false
	}
	major, err1 := strconv.Atoi(majorStr)
	minor, err2 := strconv.Atoi(minorStr)
	if err1 != nil || err2 != nil {
		return false
	}
	return major > 4 || (major == 4 && minor >= 6)
}

// Provider implements providers.Provider using the Anthropic Messages API
// (POST /v1/messages).
type Provider struct {
	apiKey    string
	authToken string // OAuth bearer token (mutually exclusive with apiKey)
	baseURL   string
	client    *http.Client
}

// New creates a new Anthropic provider.
//
// Authentication is resolved in this order:
//  1. "auth_token" config key or CLAUDE_CODE_OAUTH_TOKEN env var → OAuth bearer token
//     (uses Authorization: Bearer + anthropic-beta: oauth-2025-04-20)
//  2. "api_key" config key → API key (uses x-api-key header)
//
// At least one must be set.
func New(cfg providers.Config) (providers.Provider, error) {
	authToken := cfg["auth_token"]
	apiKey := cfg.APIKey()

	// Fall back to CLAUDE_CODE_OAUTH_TOKEN only when no credentials are in the config.
	if authToken == "" && apiKey == "" {
		authToken = os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")
	}

	if authToken == "" && apiKey == "" {
		return nil, fmt.Errorf("anthropic: api_key or auth_token (CLAUDE_CODE_OAUTH_TOKEN) is required")
	}

	baseURL := cfg.BaseURL()
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Provider{
		apiKey:    apiKey,
		authToken: authToken,
		baseURL:   strings.TrimRight(baseURL, "/"),
		client:    transport.NewClient(10 * time.Minute),
	}, nil
}

// setAuthHeader applies the appropriate authentication header to the request.
// OAuth bearer tokens (CLAUDE_CODE_OAUTH_TOKEN) use Authorization: Bearer and
// require the anthropic-beta: oauth-2025-04-20 header. API keys use x-api-key.
func (p *Provider) setAuthHeader(req *http.Request) {
	if p.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+p.authToken)
		req.Header.Add("anthropic-beta", oauthBetaHeader)
	} else {
		req.Header.Set("x-api-key", p.apiKey)
	}
}

func (p *Provider) ID() string { return providerID }

func (p *Provider) Complete(ctx context.Context, req providers.CompleteRequest) iter.Seq2[message.ProviderMessageChunk, error] {
	return func(yield func(message.ProviderMessageChunk, error) bool) {
		system, msgs, err := convertMessages(req.Messages)
		if err != nil {
			yield(nil, fmt.Errorf("anthropic: convert messages: %w", err))
			return
		}

		maxTokens := defaultMaxTokens
		if req.MaxTokens != nil {
			maxTokens = *req.MaxTokens
		}

		body := map[string]any{
			"model":      req.Model.ModelID,
			"messages":   msgs,
			"max_tokens": maxTokens,
			"stream":     true,
		}
		if system != "" {
			body["system"] = system
		}
		if tools := convertTools(req.Tools); len(tools) > 0 {
			body["tools"] = tools
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
			if supportsAdaptiveThinking(req.Model.ModelID) {
				body["thinking"] = map[string]any{
					"type": "adaptive",
				}
			} else {
				body["thinking"] = map[string]any{
					"type":          "enabled",
					"budget_tokens": reasoningBudget,
				}
			}
			if maxTokens < reasoningMaxTokens {
				body["max_tokens"] = reasoningMaxTokens
			}
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
			yield(nil, fmt.Errorf("anthropic: marshal request: %w", err))
			return
		}

		adaptiveThinking := effectiveReasoning == "enabled" && supportsAdaptiveThinking(req.Model.ModelID)
		httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/messages", bytes.NewReader(jsonBody))
		if err != nil {
			yield(nil, fmt.Errorf("anthropic: create request: %w", err))
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")
		p.setAuthHeader(httpReq)
		httpReq.Header.Set("anthropic-version", apiVersion)
		if effectiveReasoning == "enabled" && !adaptiveThinking {
			httpReq.Header.Add("anthropic-beta", thinkingBetaHeader)
		}

		resp, err := p.client.Do(httpReq)
		if err != nil {
			yield(nil, fmt.Errorf("anthropic: request failed: %w", err))
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
	system, msgs, err := convertMessages(req.Messages)
	if err != nil {
		return providers.CountTokensResponse{}, fmt.Errorf("anthropic: convert messages: %w", err)
	}

	body := map[string]any{
		"model":    req.Model.ModelID,
		"messages": msgs,
	}
	if system != "" {
		body["system"] = system
	}
	if tools := convertTools(req.Tools); len(tools) > 0 {
		body["tools"] = tools
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return providers.CountTokensResponse{}, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/messages/count_tokens", bytes.NewReader(jsonBody))
	if err != nil {
		return providers.CountTokensResponse{}, fmt.Errorf("anthropic: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	p.setAuthHeader(httpReq)
	httpReq.Header.Set("anthropic-version", apiVersion)
	httpReq.Header.Add("anthropic-beta", "token-counting-2024-11-01")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return providers.CountTokensResponse{}, fmt.Errorf("anthropic: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return providers.CountTokensResponse{}, fmt.Errorf("anthropic: API error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		InputTokens int `json:"input_tokens"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return providers.CountTokensResponse{}, fmt.Errorf("anthropic: decode response: %w", err)
	}
	return providers.CountTokensResponse{TotalTokens: result.InputTokens}, nil
}

func (p *Provider) DefaultModels() map[string]providers.ModelRef {
	return map[string]providers.ModelRef{
		providers.ModelTaskChat: {ProviderID: providerID, ModelID: "claude-sonnet-4-6"},
	}
}

func (p *Provider) ListModels(ctx context.Context) ([]providers.ModelInfo, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("anthropic: create models request: %w", err)
	}
	p.setAuthHeader(httpReq)
	httpReq.Header.Set("anthropic-version", apiVersion)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: models request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("anthropic: models API error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("anthropic: decode models response: %w", err)
	}

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

// convertMessages extracts the system prompt and converts messages to the
// Anthropic Messages API format. System messages are joined into a single
// system string; all other roles are converted to Anthropic message objects.
func convertMessages(msgs []message.Message) (system string, converted []map[string]any, err error) {
	var sysParts []string
	for _, msg := range msgs {
		if msg.Role == "system" {
			sysParts = append(sysParts, extractText(msg.Parts))
		}
	}
	system = strings.Join(sysParts, "\n")

	for _, msg := range msgs {
		switch msg.Role {
		case "system":
			continue
		case "user":
			item, e := convertUserMessage(msg)
			if e != nil {
				return "", nil, e
			}
			converted = append(converted, item)
		case "assistant":
			item, e := convertAssistantMessage(msg)
			if e != nil {
				return "", nil, e
			}
			if item != nil {
				converted = append(converted, item)
			}
		case "tool":
			item, e := convertToolMessage(msg)
			if e != nil {
				return "", nil, e
			}
			if item != nil {
				converted = append(converted, item)
			}
		default:
			return "", nil, fmt.Errorf("unknown message role: %q", msg.Role)
		}
	}
	return system, converted, nil
}

func convertUserMessage(msg message.Message) (map[string]any, error) {
	content := convertUserContent(msg.Parts)
	if len(content) == 0 {
		content = []any{map[string]any{"type": "text", "text": ""}}
	}
	data, err := json.Marshal(map[string]any{
		"role":    "user",
		"content": content,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal user message: %w", err)
	}
	var result map[string]any
	json.Unmarshal(data, &result) //nolint:errcheck // marshaled above, safe to unmarshal
	return result, nil
}

func convertUserContent(parts []message.Part) []any {
	var content []any
	for _, part := range parts {
		switch p := part.(type) {
		case message.TextPart:
			content = append(content, map[string]any{
				"type": "text",
				"text": p.Text,
			})
		case message.ImagePart:
			if strings.HasPrefix(p.Image, "http") {
				content = append(content, map[string]any{
					"type":   "image",
					"source": map[string]any{"type": "url", "url": p.Image},
				})
			} else {
				img, mt := stripDataURL(p.Image, p.MediaType)
				content = append(content, map[string]any{
					"type": "image",
					"source": map[string]any{
						"type":       "base64",
						"media_type": mt,
						"data":       img,
					},
				})
			}
		case message.FilePart:
			if strings.HasPrefix(p.MediaType, "image/") {
				if strings.HasPrefix(p.Data, "http") {
					content = append(content, map[string]any{
						"type":   "image",
						"source": map[string]any{"type": "url", "url": p.Data},
					})
				} else {
					img, mt := stripDataURL(p.Data, p.MediaType)
					content = append(content, map[string]any{
						"type": "image",
						"source": map[string]any{
							"type":       "base64",
							"media_type": mt,
							"data":       img,
						},
					})
				}
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

// convertAssistantMessage maps an assistant message to Anthropic's content
// block format. ReasoningPart with Anthropic-format ProviderMetadata (type
// "thinking" or "redacted_thinking") is passed through directly, preserving
// the required signature for multi-turn conversations. Reasoning from a
// different provider (e.g. OpenAI) is degraded to a plain text block wrapping
// the summary, so cross-provider threads don't cause API errors.
// TextPart and ToolCallPart are converted to text and tool_use blocks.
func convertAssistantMessage(msg message.Message) (map[string]any, error) {
	var content []any
	for _, part := range msg.Parts {
		switch p := part.(type) {
		case message.ReasoningPart:
			metaType := p.MetadataType()
			if metaType == "thinking" || metaType == "redacted_thinking" {
				// Anthropic-native: extract and pass through the inner block
				// from the nested format {"anthropic": {...}} with signature intact.
				var nested map[string]json.RawMessage
				if json.Unmarshal(p.ProviderMetadata, &nested) != nil {
					break
				}
				anthropicMeta, ok := nested[providerID]
				if !ok {
					break
				}
				var block any
				if err := json.Unmarshal(anthropicMeta, &block); err != nil {
					return nil, fmt.Errorf("anthropic: unmarshal reasoning metadata: %w", err)
				}
				content = append(content, block)
			} else if len(p.ProviderMetadata) > 0 && p.Text != "" {
				// Foreign provider's reasoning (has metadata but not Anthropic's
				// type) — degrade to a text block so the conversation history
				// stays usable across provider switches.
				content = append(content, map[string]any{
					"type": "text",
					"text": "<thinking>\n" + p.Text + "\n</thinking>",
				})
			}
			// Skip if no metadata at all (Anthropic requires the signature to
			// send reasoning blocks back; without it the block must be omitted).
		case message.TextPart:
			if p.Text != "" {
				content = append(content, map[string]any{
					"type": "text",
					"text": p.Text,
				})
			}
		case message.ToolCallPart:
			var input any
			if p.Input != "" {
				if err := json.Unmarshal([]byte(p.Input), &input); err != nil {
					input = map[string]any{}
				}
			}
			if input == nil {
				input = map[string]any{}
			}
			content = append(content, map[string]any{
				"type":  "tool_use",
				"id":    p.ToolCallID,
				"name":  p.ToolName,
				"input": input,
			})
		}
	}
	if len(content) == 0 {
		return nil, nil
	}
	return map[string]any{
		"role":    "assistant",
		"content": content,
	}, nil
}

// convertToolMessage maps a tool role message (ToolResultPart) to an Anthropic
// user message containing tool_result content blocks.
func convertToolMessage(msg message.Message) (map[string]any, error) {
	var content []any
	for _, part := range msg.Parts {
		tr, ok := part.(message.ToolResultPart)
		if !ok {
			continue
		}
		content = append(content, map[string]any{
			"type":        "tool_result",
			"tool_use_id": tr.ToolCallID,
			"content":     toolResultToAnthropicContent(tr.Output),
		})
	}
	if len(content) == 0 {
		return nil, nil
	}
	return map[string]any{
		"role":    "user",
		"content": content,
	}, nil
}

func toolResultToAnthropicContent(output message.ToolResultOutput) any {
	contentOutput, ok := output.(message.ContentOutput)
	if !ok {
		return toolResultToString(output)
	}

	blocks := make([]any, 0, len(contentOutput.Value))
	for _, item := range contentOutput.Value {
		switch v := item.(type) {
		case message.ContentTextItem:
			blocks = append(blocks, map[string]any{
				"type": "text",
				"text": v.Text,
			})
		case message.ContentImageDataItem:
			if strings.TrimSpace(v.Data) == "" {
				continue
			}
			mediaType := v.MediaType
			if mediaType == "" {
				mediaType = "image/jpeg"
			}
			blocks = append(blocks, map[string]any{
				"type": "image",
				"source": map[string]any{
					"type":       "base64",
					"media_type": mediaType,
					"data":       v.Data,
				},
			})
		case message.ContentFileDataItem:
			if strings.TrimSpace(v.Data) == "" {
				continue
			}
			if v.MediaType == "application/pdf" {
				blocks = append(blocks, map[string]any{
					"type": "document",
					"source": map[string]any{
						"type":       "base64",
						"media_type": "application/pdf",
						"data":       v.Data,
					},
				})
			}
		}
	}

	if len(blocks) == 0 {
		return toolResultToString(output)
	}

	return blocks
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

// stripDataURL strips a data URL prefix (e.g. "data:image/png;base64,") from
// data, returning the bare base64 string and the media type. If data does not
// have a data URL prefix, it is returned unchanged with the provided mediaType.
func stripDataURL(data, mediaType string) (string, string) {
	if !strings.HasPrefix(data, "data:") {
		if mediaType == "" {
			mediaType = "image/jpeg"
		}
		return data, mediaType
	}
	// data:image/png;base64,<data>
	rest := strings.TrimPrefix(data, "data:")
	if idx := strings.Index(rest, ","); idx >= 0 {
		header := rest[:idx]
		raw := rest[idx+1:]
		if parts := strings.SplitN(header, ";", 2); len(parts) > 0 && parts[0] != "" {
			mediaType = parts[0]
		}
		return raw, mediaType
	}
	return data, mediaType
}

// --- Tool conversion ---

// convertTools maps ToolDefinition to Anthropic's tool format.
// Anthropic places input_schema at the top level (not nested under "function").
func convertTools(tools []providers.ToolDefinition) []map[string]any {
	if len(tools) == 0 {
		return nil
	}
	result := make([]map[string]any, len(tools))
	for i, t := range tools {
		tool := map[string]any{
			"name":         t.Name,
			"input_schema": json.RawMessage(t.InputSchema),
		}
		if t.Description != "" {
			tool["description"] = t.Description
		}
		result[i] = tool
	}
	return result
}

// --- SSE stream parsing ---

// streamState tracks in-progress content block information needed to
// correlate delta and stop events with their originating blocks.
type streamState struct {
	blockTypes  map[int]string // index → "text", "tool_use", "thinking"
	toolCallIDs map[int]string // tool_use index → tool call ID
	toolNames   map[int]string // tool_use index → tool name
	thinkTexts  map[int]string // thinking index → accumulated thinking text
	signatures  map[int]string // thinking index → accumulated signature
	hasToolUse  bool

	// Token usage, populated across message_start and message_delta events.
	inputTokens  int
	cacheCreate  int
	cacheRead    int
	outputTokens int
}

func parseSSEStream(body io.Reader, yield func(message.ProviderMessageChunk, error) bool) {
	state := &streamState{
		blockTypes:  make(map[int]string),
		toolCallIDs: make(map[int]string),
		toolNames:   make(map[int]string),
		thinkTexts:  make(map[int]string),
		signatures:  make(map[int]string),
	}

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
				if !handleSSEEvent(eventType, []byte(data), state, yield) {
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
		yield(nil, fmt.Errorf("anthropic: read SSE stream: %w", err))
	}
}

func handleSSEEvent(eventType string, data []byte, state *streamState, yield func(message.ProviderMessageChunk, error) bool) bool {
	switch eventType {
	case "message_start":
		return handleMessageStart(data, state, yield)
	case "content_block_start":
		return handleContentBlockStart(data, state, yield)
	case "content_block_delta":
		return handleContentBlockDelta(data, state, yield)
	case "content_block_stop":
		return handleContentBlockStop(data, state, yield)
	case "message_delta":
		return handleMessageDelta(data, state, yield)
	case "error":
		return handleStreamError(data, yield)
	default:
		// ping, message_stop, etc. — silently ignore.
		return true
	}
}

func handleMessageStart(data []byte, state *streamState, yield func(message.ProviderMessageChunk, error) bool) bool {
	var event struct {
		Message struct {
			ID    string `json:"id"`
			Model string `json:"model"`
			Usage struct {
				InputTokens              int `json:"input_tokens"`
				CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
				CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return yield(nil, fmt.Errorf("anthropic: parse message_start: %w", err))
	}
	state.inputTokens = event.Message.Usage.InputTokens
	state.cacheCreate = event.Message.Usage.CacheCreationInputTokens
	state.cacheRead = event.Message.Usage.CacheReadInputTokens
	if !yield(message.StreamStartChunk{}, nil) {
		return false
	}
	now := time.Now()
	return yield(message.ResponseMetadataChunk{
		ID:        event.Message.ID,
		Timestamp: &now,
		ModelID:   event.Message.Model,
	}, nil)
}

func handleContentBlockStart(data []byte, state *streamState, yield func(message.ProviderMessageChunk, error) bool) bool {
	var event struct {
		Index        int `json:"index"`
		ContentBlock struct {
			Type string `json:"type"`
			ID   string `json:"id"`   // tool_use
			Name string `json:"name"` // tool_use
		} `json:"content_block"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return yield(nil, fmt.Errorf("anthropic: parse content_block_start: %w", err))
	}

	idx := event.Index
	blockType := event.ContentBlock.Type
	state.blockTypes[idx] = blockType

	switch blockType {
	case "text":
		return yield(message.TextStartChunk{ID: blockID("text", idx)}, nil)
	case "tool_use":
		state.toolCallIDs[idx] = event.ContentBlock.ID
		state.toolNames[idx] = event.ContentBlock.Name
		state.hasToolUse = true
		return yield(message.ToolInputStartChunk{
			ToolCallID: event.ContentBlock.ID,
			ToolName:   event.ContentBlock.Name,
		}, nil)
	case "thinking":
		state.thinkTexts[idx] = ""
		state.signatures[idx] = ""
		return yield(message.ReasoningStartChunk{ID: blockID("thinking", idx)}, nil)
	}
	return true
}

func handleContentBlockDelta(data []byte, state *streamState, yield func(message.ProviderMessageChunk, error) bool) bool {
	var event struct {
		Index int `json:"index"`
		Delta struct {
			Type        string `json:"type"`
			Text        string `json:"text"`         // text_delta
			PartialJSON string `json:"partial_json"` // input_json_delta
			Thinking    string `json:"thinking"`     // thinking_delta
			Signature   string `json:"signature"`    // signature_delta
		} `json:"delta"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return yield(nil, fmt.Errorf("anthropic: parse content_block_delta: %w", err))
	}

	idx := event.Index
	switch event.Delta.Type {
	case "text_delta":
		return yield(message.TextDeltaChunk{ID: blockID("text", idx), Delta: event.Delta.Text}, nil)
	case "input_json_delta":
		return yield(message.ToolInputDeltaChunk{
			ToolCallID:     state.toolCallIDs[idx],
			InputTextDelta: event.Delta.PartialJSON,
		}, nil)
	case "thinking_delta":
		state.thinkTexts[idx] += event.Delta.Thinking
		return yield(message.ReasoningDeltaChunk{ID: blockID("thinking", idx), Delta: event.Delta.Thinking}, nil)
	case "signature_delta":
		state.signatures[idx] += event.Delta.Signature
		// Signature is stored in state; no chunk is emitted.
	}
	return true
}

func handleContentBlockStop(data []byte, state *streamState, yield func(message.ProviderMessageChunk, error) bool) bool {
	var event struct {
		Index int `json:"index"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return yield(nil, fmt.Errorf("anthropic: parse content_block_stop: %w", err))
	}

	idx := event.Index
	switch state.blockTypes[idx] {
	case "text":
		return yield(message.TextEndChunk{ID: blockID("text", idx)}, nil)
	case "tool_use":
		return yield(message.ToolInputEndChunk{ToolCallID: state.toolCallIDs[idx]}, nil)
	case "thinking":
		// Build the ProviderMetadata in the Vercel AI SDK v6 nested format:
		// {"anthropic": {"type": "thinking", "thinking": "...", "signature": "..."}}.
		// The anthropic block contains the signature required for multi-turn
		// continuity; convertAssistantMessage extracts it when replaying turns.
		meta, err := json.Marshal(map[string]any{
			providerID: map[string]any{
				"type":      "thinking",
				"thinking":  state.thinkTexts[idx],
				"signature": state.signatures[idx],
			},
		})
		if err != nil {
			return yield(nil, fmt.Errorf("anthropic: marshal thinking metadata: %w", err))
		}
		return yield(message.ReasoningEndChunk{
			ID:               blockID("thinking", idx),
			ProviderMetadata: meta,
		}, nil)
	}
	return true
}

func handleMessageDelta(data []byte, state *streamState, yield func(message.ProviderMessageChunk, error) bool) bool {
	var event struct {
		Delta struct {
			StopReason string `json:"stop_reason"`
		} `json:"delta"`
		Usage struct {
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return yield(nil, fmt.Errorf("anthropic: parse message_delta: %w", err))
	}
	state.outputTokens = event.Usage.OutputTokens

	noCache := state.inputTokens - state.cacheCreate - state.cacheRead
	if noCache < 0 {
		noCache = 0
	}
	return yield(message.FinishChunk{
		FinishReason: message.FinishReason{
			Unified: unifyStopReason(event.Delta.StopReason, state.hasToolUse),
			Raw:     event.Delta.StopReason,
		},
		Usage: message.Usage{
			InputTokens: message.InputTokens{
				Total:      state.inputTokens,
				CacheWrite: state.cacheCreate,
				CacheRead:  state.cacheRead,
				NoCache:    noCache,
			},
			OutputTokens: message.OutputTokens{
				Total: state.outputTokens,
				Text:  state.outputTokens,
			},
		},
	}, nil)
}

func handleStreamError(data []byte, yield func(message.ProviderMessageChunk, error) bool) bool {
	var event struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return yield(nil, fmt.Errorf("anthropic: parse error event: %w", err))
	}
	return yield(nil, fmt.Errorf("anthropic: stream error: %s (type: %s)", event.Error.Message, event.Error.Type))
}

func unifyStopReason(stopReason string, hasToolUse bool) string {
	if hasToolUse {
		return "tool-calls"
	}
	switch stopReason {
	case "end_turn", "stop_sequence":
		return "stop"
	case "max_tokens":
		return "length"
	default:
		return "other"
	}
}

// blockID returns a stable string ID for a content block given its type and
// index (e.g. "text_0", "thinking_1"). Used as the ID field in streaming
// chunks where Anthropic uses positional indexing rather than named IDs.
func blockID(blockType string, idx int) string {
	return fmt.Sprintf("%s_%d", blockType, idx)
}

// parseError converts a non-200 Anthropic API response into a descriptive error.
// Anthropic error bodies have the form:
//
//	{"type":"error","error":{"type":"rate_limit_error","message":"..."}}
//
// The error is retriable for 429 and 5xx status codes.
func parseError(statusCode int, body []byte) (bool, error) {
	msg := extractErrorMessage(body)
	retriable := statusCode == http.StatusTooManyRequests || statusCode >= 500
	return retriable, fmt.Errorf("anthropic: API error %d: %s", statusCode, msg)
}

// extractErrorMessage parses the human-readable message from an Anthropic error body.
// Falls back to the raw body string when the structure cannot be parsed.
func extractErrorMessage(body []byte) string {
	var apiErr struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
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
