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
	"os"
	"strings"
	"time"

	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
	"github.com/obot-platform/discobot/agent-go/providers/transport"
	"github.com/obot-platform/discobot/modelsdev"
)

// configUseWebSocket is the Config key that opts the provider into WebSocket
// mode. Set to "true" to enable. WebSocket mode is also enabled automatically
// when base_url begins with "wss://" or "ws://". The default OpenAI base URL
// is `wss://api.openai.com/v1`, so WebSocket mode is the default transport;
// specify an explicit `https://...` base_url to force HTTP SSE instead.
const configUseWebSocket = "use_websocket"

const (
	providerID            = "openai"
	defaultBaseURL        = "wss://api.openai.com/v1"
	codexDefaultBaseURL   = "https://chatgpt.com/backend-api/codex"
	codexProviderID       = "codex"
	missingToolOutputText = "interrupted by transient system failure"
)

func init() {
	providers.Register(providerID, func(cfg providers.Config) (providers.Provider, error) {
		return New(cfg, false, defaultBaseURL)
	})
	providers.Register(codexProviderID, func(cfg providers.Config) (providers.Provider, error) {
		return New(cfg, true, codexDefaultBaseURL)
	})
}

// Provider implements providers.Provider using the OpenAI Responses API.
// It supports both HTTP SSE streaming (POST /v1/responses) and a persistent
// WebSocket connection (wss://…/v1/responses). WebSocket mode is enabled when
// the "use_websocket" config key is "true" or the base_url uses a ws(s)://
// scheme; it maintains a pool of per-session connections that reuse server-side
// cached state and make repeated tool-call loops ~40 % faster.
type Provider struct {
	apiKey    string
	baseURL   string
	client    *http.Client
	ws        *wsPool // non-nil when WebSocket mode is enabled
	accountID string  // ChatGPT account ID for the ChatGPT-Account-Id header (Codex only)
	isCodex   bool    // true when targeting the ChatGPT Codex backend
}

// codexModels is the hardcoded list of models available via the ChatGPT Codex
// backend. We skip the live /models API call when in Codex mode because that
// endpoint is not available on chatgpt.com.
var codexModels = []providers.ModelInfo{
	{ID: "gpt-5.1-codex-max", DisplayName: "GPT-5.1 Codex Max", Reasoning: true, ReasoningLevels: []providers.Reasoning{providers.ReasoningLow, providers.ReasoningMedium, providers.ReasoningHigh}, DefaultReasoning: providers.ReasoningMedium},
	{ID: "gpt-5.1-codex-mini", DisplayName: "GPT-5.1 Codex Mini", Reasoning: true, ReasoningLevels: []providers.Reasoning{providers.ReasoningLow, providers.ReasoningMedium, providers.ReasoningHigh}, DefaultReasoning: providers.ReasoningMedium},
	{ID: "gpt-5.1-codex", DisplayName: "GPT-5.1 Codex", Reasoning: true, ReasoningLevels: []providers.Reasoning{providers.ReasoningLow, providers.ReasoningMedium, providers.ReasoningHigh}, DefaultReasoning: providers.ReasoningMedium},
	{ID: "gpt-5.2", DisplayName: "GPT-5.2", Reasoning: true, ReasoningLevels: []providers.Reasoning{providers.ReasoningLow, providers.ReasoningMedium, providers.ReasoningHigh, providers.ReasoningXHigh}, DefaultReasoning: providers.ReasoningMedium},
	{ID: "gpt-5.2-codex", DisplayName: "GPT-5.2 Codex", Reasoning: true, ReasoningLevels: []providers.Reasoning{providers.ReasoningLow, providers.ReasoningMedium, providers.ReasoningHigh, providers.ReasoningXHigh}, DefaultReasoning: providers.ReasoningMedium},
	{ID: "gpt-5.3-codex", DisplayName: "GPT-5.3 Codex", Reasoning: true, ReasoningLevels: []providers.Reasoning{providers.ReasoningLow, providers.ReasoningMedium, providers.ReasoningHigh, providers.ReasoningXHigh}, DefaultReasoning: providers.ReasoningMedium},
	{ID: "gpt-5.4", DisplayName: "GPT-5.4", Reasoning: true, ReasoningLevels: []providers.Reasoning{providers.ReasoningLow, providers.ReasoningMedium, providers.ReasoningHigh, providers.ReasoningXHigh}, DefaultReasoning: providers.ReasoningMedium},
}

// New creates a new OpenAI Responses API provider.
func New(cfg providers.Config, isCodex bool, defaultURL string) (providers.Provider, error) {
	apiKey := cfg.APIKey()
	if apiKey == "" {
		apiKey = cfg.Token()
	}
	if apiKey == "" {
		return nil, fmt.Errorf("openai: api_key or auth_token is required")
	}
	baseURL := cfg.BaseURL()
	if baseURL == "" {
		baseURL = os.Getenv("OPENAI_API_BASE")
	}
	if baseURL == "" {
		baseURL = defaultURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	// Detect WebSocket mode: explicit flag or ws(s):// scheme on base_url.
	useWS := cfg[configUseWebSocket] == "true" ||
		strings.HasPrefix(baseURL, "wss://") ||
		strings.HasPrefix(baseURL, "ws://")

	// Normalise base_url to http(s) so REST endpoints (CountTokens, ListModels)
	// work regardless of which scheme the caller provided.
	httpBaseURL := baseURL
	httpBaseURL = strings.Replace(httpBaseURL, "wss://", "https://", 1)
	httpBaseURL = strings.Replace(httpBaseURL, "ws://", "http://", 1)

	accountID := cfg["account_id"]
	if accountID == "" {
		accountID = os.Getenv("CHATGPT_ACCOUNT_ID")
	}

	p := &Provider{
		apiKey:    apiKey,
		baseURL:   httpBaseURL,
		client:    transport.NewClient(10 * time.Minute),
		accountID: accountID,
		isCodex:   isCodex,
	}
	if useWS {
		p.ws = newWSPool(apiKey, httpBaseURL)
		if isCodex && accountID != "" {
			p.ws.accountID = accountID
		}
	}
	return p, nil
}

func (p *Provider) ID() string { return providerID }

// setAuthHeaders sets Authorization and, when in Codex mode with an account ID
// configured, the ChatGPT-Account-Id header required for organisation subscriptions.
func (p *Provider) setAuthHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	if p.isCodex && p.accountID != "" {
		req.Header.Set("ChatGPT-Account-Id", p.accountID)
	}
}

func (p *Provider) Complete(ctx context.Context, req providers.CompleteRequest) iter.Seq2[message.ProviderMessageChunk, error] {
	return func(yield func(message.ProviderMessageChunk, error) bool) {
		customToolNameSet := customToolNames(req.Tools)
		instructions, inputMessages := extractInstructionsFromMessages(req.Messages)
		inputItems, err := convertMessagesWithCustomTools(inputMessages, customToolNameSet)
		if err != nil {
			yield(nil, fmt.Errorf("openai: convert messages: %w", err))
			return
		}

		body := map[string]any{
			"model":        req.Model.ModelID,
			"input":        inputItems,
			"instructions": instructions,
			"store":        false,
		}
		if !p.isCodex {
			body["truncation"] = "disabled"
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
		// Resolve the effective reasoning level and map to an OpenAI effort parameter.
		// Returns "" if reasoning should be omitted.
		if effort := resolveOpenAIEffort(req.Reasoning, req.Model.ModelID); effort != "" {
			body["reasoning"] = map[string]any{
				"effort":  effort,
				"summary": "detailed",
			}
			body["include"] = []string{"reasoning.encrypted_content"}
		}

		if p.ws != nil {
			prevRespID := lastAssistantID(req.Messages)
			incrementalBody := body
			if prevRespID != "" {
				incrementalBody, err = buildWebSocketIncrementalBody(body, req.Messages, customToolNameSet, prevRespID)
				if err != nil {
					yield(nil, fmt.Errorf("openai: build websocket incremental body: %w", err))
					return
				}
			}
			p.completeViaWebSocket(ctx, body, incrementalBody, prevRespID, yield)
			return
		}

		// HTTP SSE path.
		body["stream"] = true

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
		p.setAuthHeaders(httpReq)

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

func buildWebSocketIncrementalBody(baseBody map[string]any, msgs []message.Message, customToolNames map[string]struct{}, prevRespID string) (map[string]any, error) {
	incrementalMsgs := messagesAfterAssistantID(msgs, prevRespID)
	incrementalInstructions, incrementalInputMessages := extractInstructionsFromMessages(incrementalMsgs)
	incrementalInput, err := convertMessagesWithCustomTools(incrementalInputMessages, customToolNames)
	if err != nil {
		return nil, fmt.Errorf("convert incremental messages: %w", err)
	}

	incrementalBody := cloneWebSocketBody(baseBody)
	incrementalBody["input"] = incrementalInput
	delete(incrementalBody, "tools")
	if incrementalInstructions == "" {
		delete(incrementalBody, "instructions")
	} else {
		incrementalBody["instructions"] = incrementalInstructions
	}

	return incrementalBody, nil
}

func (p *Provider) DefaultModels() map[string]providers.ModelRef {
	if p.isCodex {
		return map[string]providers.ModelRef{
			providers.ModelTaskChat: {ProviderID: codexProviderID, ModelID: "gpt-5.4"},
		}
	}
	return map[string]providers.ModelRef{
		providers.ModelTaskChat: {ProviderID: providerID, ModelID: "gpt-5.4"},
	}
}

func (p *Provider) ListModels(ctx context.Context) ([]providers.ModelInfo, error) {
	// When targeting the ChatGPT Codex backend the /models endpoint is not
	// available, so return the known Codex model list directly.
	if p.isCodex {
		return codexModels, nil
	}

	// Fetch live model IDs from the OpenAI API.
	httpReq, err := http.NewRequestWithContext(ctx, "GET", p.baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("openai: create models request: %w", err)
	}
	p.setAuthHeaders(httpReq)

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
			info.ReasoningLevels = toReasoningSlice(md.ReasoningLevels)
			info.DefaultReasoning = providers.Reasoning(md.DefaultReasonLevel)
			info.ContextWindow = md.ContextWindow
			info.MaxOutputTokens = md.MaxOutputTokens
		} else if isReasoningModelID(m.ID) {
			// Unknown model that looks like a reasoning model: use safe defaults.
			info.Reasoning = true
			info.ReasoningLevels = []providers.Reasoning{providers.ReasoningLow, providers.ReasoningMedium, providers.ReasoningHigh}
			info.DefaultReasoning = providers.ReasoningMedium
		}
		models = append(models, info)
	}
	return models, nil
}

// --- Message conversion ---

// convertMessages converts internal messages to OpenAI Responses API input items.
// The Responses API input array contains role-based messages (user, developer)
// and typed items (function_call, custom_tool_call, *_output, message).
func convertMessages(msgs []message.Message) ([]json.RawMessage, error) {
	return convertMessagesWithCustomTools(msgs, nil)
}

func convertMessagesWithCustomTools(msgs []message.Message, customToolNames map[string]struct{}) ([]json.RawMessage, error) {
	var items []json.RawMessage
	for _, msg := range msgs {
		converted, err := convertMessage(msg, customToolNames)
		if err != nil {
			return nil, err
		}
		items = append(items, converted...)
	}
	return ensureToolCallOutputs(items), nil
}

func convertMessage(msg message.Message, customToolNames map[string]struct{}) ([]json.RawMessage, error) {
	switch msg.Role {
	case "system":
		return convertSystemMessage(msg)
	case "user":
		return convertUserMessage(msg)
	case "assistant":
		return convertAssistantMessage(msg, customToolNames)
	case "tool":
		return convertToolMessage(msg, customToolNames)
	default:
		return nil, fmt.Errorf("unknown message role: %q", msg.Role)
	}
}

// ensureToolCallOutputs appends synthetic *_tool_call_output items for any
// unresolved function/custom tool calls in the reconstructed input history.
func ensureToolCallOutputs(items []json.RawMessage) []json.RawMessage {
	type itemHeader struct {
		Type   string `json:"type"`
		CallID string `json:"call_id"`
	}

	type pendingCall struct {
		callID string
	}

	pendingOrder := make([]pendingCall, 0)
	pending := make(map[string]string)

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
				pendingOrder = append(pendingOrder, pendingCall{callID: header.CallID})
			}
			pending[header.CallID] = "function_call_output"
		case "custom_tool_call":
			if header.CallID == "" {
				continue
			}
			if _, exists := pending[header.CallID]; !exists {
				pendingOrder = append(pendingOrder, pendingCall{callID: header.CallID})
			}
			pending[header.CallID] = "custom_tool_call_output"
		case "function_call_output", "custom_tool_call_output":
			delete(pending, header.CallID)
		}
	}

	if len(pending) == 0 {
		return items
	}

	result := make([]json.RawMessage, 0, len(items)+len(pending))
	result = append(result, items...)
	for _, call := range pendingOrder {
		outputType, ok := pending[call.callID]
		if !ok {
			continue
		}
		data, err := json.Marshal(map[string]any{
			"type":    outputType,
			"call_id": call.callID,
			"output":  missingToolOutputText,
		})
		if err != nil {
			continue
		}
		result = append(result, data)
	}
	return result
}

func customToolNames(tools []providers.ToolDefinition) map[string]struct{} {
	if len(tools) == 0 {
		return nil
	}
	names := make(map[string]struct{})
	for _, t := range tools {
		if t.Type != "custom" || t.Name == "" {
			continue
		}
		names[t.Name] = struct{}{}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

func isCustomTool(name string, customToolNames map[string]struct{}) bool {
	if name == "" || len(customToolNames) == 0 {
		return false
	}
	_, ok := customToolNames[name]
	return ok
}

func customToolInput(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ""
	}
	var wrapped struct {
		Input string `json:"input"`
	}
	if err := json.Unmarshal([]byte(trimmed), &wrapped); err == nil && strings.TrimSpace(wrapped.Input) != "" {
		return wrapped.Input
	}
	var plain string
	if err := json.Unmarshal([]byte(trimmed), &plain); err == nil {
		return plain
	}
	return input
}

func toolOutputType(toolName string, customToolNames map[string]struct{}) string {
	if isCustomTool(toolName, customToolNames) {
		return "custom_tool_call_output"
	}
	return "function_call_output"
}

// extractInstructionsFromMessages removes the first system message from msgs and
// returns its text as top-level instructions. We do this before converting
// messages into JSON input items so we avoid extra JSON unmarshal passes.
func extractInstructionsFromMessages(msgs []message.Message) (string, []message.Message) {
	for i, msg := range msgs {
		if msg.Role != "system" {
			continue
		}
		instructions := extractText(msg.Parts)
		remaining := make([]message.Message, 0, len(msgs)-1)
		remaining = append(remaining, msgs[:i]...)
		remaining = append(remaining, msgs[i+1:]...)
		return instructions, remaining
	}
	return "", msgs
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
// (function_call/custom_tool_call and *_tool_call_output). Items are ordered:
// reasoning → message → tool calls → tool call outputs.
func convertAssistantMessage(msg message.Message, customToolNames map[string]struct{}) ([]json.RawMessage, error) {
	var reasoningItems []json.RawMessage
	var toolCallItems []json.RawMessage
	var toolCallOutputItems []json.RawMessage
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
			callType := "function_call"
			payload := map[string]any{
				"type":    callType,
				"call_id": p.ToolCallID,
				"name":    p.ToolName,
			}
			if isCustomTool(p.ToolName, customToolNames) {
				callType = "custom_tool_call"
				payload["type"] = callType
				payload["input"] = customToolInput(p.Input)
			} else {
				payload["arguments"] = p.Input
			}
			data, err := json.Marshal(payload)
			if err != nil {
				return nil, fmt.Errorf("marshal %s: %w", callType, err)
			}
			toolCallItems = append(toolCallItems, data)
		case message.ToolResultPart:
			outputType := toolOutputType(p.ToolName, customToolNames)
			data, err := json.Marshal(map[string]any{
				"type":    outputType,
				"call_id": p.ToolCallID,
				"output":  toolResultToOpenAIOutput(p.Output),
			})
			if err != nil {
				return nil, fmt.Errorf("marshal %s: %w", outputType, err)
			}
			toolCallOutputItems = append(toolCallOutputItems, data)
		}
	}

	// Order: reasoning → text message → tool calls → tool call outputs.
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
	items = append(items, toolCallItems...)
	items = append(items, toolCallOutputItems...)

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

// convertToolMessage maps each ToolResultPart to a *_tool_call_output item.
func convertToolMessage(msg message.Message, customToolNames map[string]struct{}) ([]json.RawMessage, error) {
	var items []json.RawMessage
	for _, part := range msg.Parts {
		tr, ok := part.(message.ToolResultPart)
		if !ok {
			continue
		}
		outputType := toolOutputType(tr.ToolName, customToolNames)
		data, err := json.Marshal(map[string]any{
			"type":    outputType,
			"call_id": tr.ToolCallID,
			"output":  toolResultToOpenAIOutput(tr.Output),
		})
		if err != nil {
			return nil, fmt.Errorf("marshal %s: %w", outputType, err)
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

func toolResultToOpenAIOutput(output message.ToolResultOutput) any {
	contentOutput, ok := output.(message.ContentOutput)
	if !ok {
		return toolResultToString(output)
	}

	items, hasNonText := contentOutputToOpenAIItems(contentOutput)
	if len(items) == 0 {
		return toolResultToString(output)
	}
	if !hasNonText {
		return toolResultToString(output)
	}
	return items
}

func contentOutputToOpenAIItems(contentOutput message.ContentOutput) ([]any, bool) {
	items := make([]any, 0, len(contentOutput.Value))
	hasNonText := false

	for _, item := range contentOutput.Value {
		switch contentItem := item.(type) {
		case message.ContentTextItem:
			if contentItem.Text == "" {
				continue
			}
			items = append(items, map[string]any{
				"type": "input_text",
				"text": contentItem.Text,
			})
		case message.ContentImageDataItem:
			if strings.TrimSpace(contentItem.Data) == "" {
				continue
			}
			hasNonText = true
			mediaType := contentItem.MediaType
			if mediaType == "" {
				mediaType = "image/jpeg"
			}
			items = append(items, map[string]any{
				"type":      "input_image",
				"image_url": "data:" + mediaType + ";base64," + contentItem.Data,
			})
		case message.ContentImageURLItem:
			if strings.TrimSpace(contentItem.URL) == "" {
				continue
			}
			hasNonText = true
			items = append(items, map[string]any{
				"type":      "input_image",
				"image_url": contentItem.URL,
			})
		case message.ContentFileDataItem:
			hasNonText = true
			mediaType := contentItem.MediaType
			if strings.HasPrefix(mediaType, "image/") && strings.TrimSpace(contentItem.Data) != "" {
				items = append(items, map[string]any{
					"type":      "input_image",
					"image_url": "data:" + mediaType + ";base64," + contentItem.Data,
				})
				continue
			}
			if mediaType == "" {
				mediaType = "application/octet-stream"
			}
			placeholder := fmt.Sprintf("[file data omitted (%s)]", mediaType)
			if contentItem.Filename != "" {
				placeholder = fmt.Sprintf("[file data omitted (%s, filename=%s)]", mediaType, contentItem.Filename)
			}
			items = append(items, map[string]any{
				"type": "input_text",
				"text": placeholder,
			})
		}
	}

	return items, hasNonText
}

// --- Tool conversion ---

// convertTools maps our ToolDefinition to Responses API function/custom tools.
func convertTools(tools []providers.ToolDefinition) []map[string]any {
	if len(tools) == 0 {
		return nil
	}
	result := make([]map[string]any, len(tools))
	for i, t := range tools {
		if t.Type == "custom" && t.Format != nil {
			tool := map[string]any{
				"type": "custom",
				"name": t.Name,
				"format": map[string]any{
					"type":       t.Format.Type,
					"syntax":     t.Format.Syntax,
					"definition": t.Format.Definition,
				},
			}
			if t.Description != "" {
				tool["description"] = t.Description
			}
			result[i] = tool
			continue
		}

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
// completes and ToolCallChunk for completed custom tool calls.
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
	switch itemHeader.Type {
	case "reasoning":
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
	case "custom_tool_call":
		var item struct {
			CallID string `json:"call_id"`
			Name   string `json:"name"`
			Input  string `json:"input"`
		}
		if err := json.Unmarshal(event.Item, &item); err != nil {
			return yield(nil, fmt.Errorf("openai: parse custom_tool_call item: %w", err))
		}
		return yield(message.ToolCallChunk{
			ToolCallID: item.CallID,
			ToolName:   item.Name,
			Input:      item.Input,
		}, nil)
	default:
		return true
	}
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
		if item.Type == "function_call" || item.Type == "custom_tool_call" {
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
	return yield(nil, &openAIStreamError{
		message: msg,
		code:    event.Error.Code,
	})
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

// resolveOpenAIEffort maps a Reasoning request to an OpenAI effort string.
// Returns "" if reasoning should be omitted entirely.
func resolveOpenAIEffort(r providers.Reasoning, modelID string) string {
	switch r {
	case providers.ReasoningDisabled, providers.ReasoningNone:
		return "" // omit reasoning block
	case providers.ReasoningEmpty, providers.ReasoningDefault:
		// Auto-detect from model metadata.
		if md := modelsdev.Lookup(providerID, modelID); md != nil {
			if md.DefaultReasonLevel != "" {
				return openAIEffort(providers.Reasoning(md.DefaultReasonLevel))
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

// openAIEffort maps a Reasoning level to an OpenAI effort string.
func openAIEffort(r providers.Reasoning) string {
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

// isReasoningModelID returns true for model IDs that are known OpenAI
// reasoning model families (o1, o3, o4, gpt-5, codex variants).
func isReasoningModelID(id string) bool {
	for _, prefix := range []string{"o1", "o3", "o4", "gpt-5", "codex"} {
		if id == prefix || strings.HasPrefix(id, prefix+"-") {
			return true
		}
	}
	return false
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
