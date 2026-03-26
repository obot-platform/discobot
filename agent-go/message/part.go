package message

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Part represents a content part within a Message.
// The concrete type determines the "type" discriminator in JSON.
type Part interface {
	partType() string
}

// DiscobotPartMetadata holds discobot-specific metadata attached to a part's
// ProviderMetadata field. It is serialized as {"discobot": {...}} to match the
// AI SDK ProviderMetadata shape (Record<providerNamespace, JSONObject>).
type DiscobotPartMetadata struct {
	// OriginalCommand is the raw slash-command string the user typed (e.g.
	// "/commit fix the bug") before it was expanded into the message text.
	OriginalCommand string `json:"originalCommand,omitempty"`
	// ReminderKind classifies framework-injected reminder parts.
	ReminderKind string `json:"reminderKind,omitempty"`
	// Mode records the current execution mode for mode-reminder parts.
	Mode string `json:"mode,omitempty"`
}

// MarshalProviderMetadata encodes a DiscobotPartMetadata value into the
// ProviderMetadata wire format expected by the AI SDK:
//
//	{"discobot": {"originalCommand": "..."}}
//
// Returns nil on marshal error (non-fatal; callers may use nil as a no-op).
func MarshalProviderMetadata(meta DiscobotPartMetadata) json.RawMessage {
	data, err := json.Marshal(map[string]DiscobotPartMetadata{"discobot": meta})
	if err != nil {
		return nil
	}
	return data
}

// UnmarshalProviderMetadata decodes Discobot provider metadata from the nested
// AI SDK providerMetadata shape. It returns false when no discobot metadata is present.
func UnmarshalProviderMetadata(data json.RawMessage) (DiscobotPartMetadata, bool) {
	if len(data) == 0 {
		return DiscobotPartMetadata{}, false
	}
	var nested map[string]json.RawMessage
	if json.Unmarshal(data, &nested) != nil {
		return DiscobotPartMetadata{}, false
	}
	rawMeta, ok := nested["discobot"]
	if !ok {
		return DiscobotPartMetadata{}, false
	}
	var meta DiscobotPartMetadata
	if json.Unmarshal(rawMeta, &meta) != nil {
		return DiscobotPartMetadata{}, false
	}
	return meta, true
}

func toolInputJSONValue(input string) json.RawMessage {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return json.RawMessage("null")
	}
	if json.Valid([]byte(trimmed)) {
		switch trimmed[0] {
		case '{', '[':
			return json.RawMessage(trimmed)
		}
	}
	data, err := json.Marshal(map[string]string{"raw": input})
	if err != nil {
		return json.RawMessage("null")
	}
	return json.RawMessage(data)
}

func persistedToolInputFields(input string) (json.RawMessage, string) {
	trimmed := strings.TrimSpace(input)
	if trimmed != "" && json.Valid([]byte(trimmed)) {
		return json.RawMessage(trimmed), ""
	}
	if input == "" {
		return nil, ""
	}
	return nil, input
}

// TextPart is a text content part.
type TextPart struct {
	ID               string          `json:"id,omitempty"`
	Text             string          `json:"text"`
	State            string          `json:"state,omitempty"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
	ProviderOptions  json.RawMessage `json:"providerOptions,omitempty"`
}

func (TextPart) partType() string { return "text" }

// ReasoningPart is a reasoning/thinking content part.
type ReasoningPart struct {
	ID               string          `json:"id,omitempty"`
	Text             string          `json:"text"`
	State            string          `json:"state,omitempty"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
	ProviderOptions  json.RawMessage `json:"providerOptions,omitempty"`
}

func (ReasoningPart) partType() string { return "reasoning" }

// MetadataType extracts the provider-specific "type" field from ProviderMetadata.
// Providers use this to check whether persisted metadata is their own format
// before passing it back to the API. Returns "" when metadata is absent or
// does not contain a recognisable "type" field.
//
// ProviderMetadata uses the Vercel AI SDK v6 nested format:
// {"<provider>": {"type": "<type>", ...}} e.g. {"anthropic": {"type": "thinking"}}.
//
// Example: Anthropic checks p.MetadataType() == "thinking"; OpenAI checks
// p.MetadataType() == "reasoning". When the type doesn't match, providers
// should fall back to constructing a native item from p.Text instead.
func (p ReasoningPart) MetadataType() string {
	if len(p.ProviderMetadata) == 0 {
		return ""
	}
	var nested map[string]json.RawMessage
	if json.Unmarshal(p.ProviderMetadata, &nested) != nil {
		return ""
	}
	for _, v := range nested {
		var inner struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(v, &inner) == nil && inner.Type != "" {
			return inner.Type
		}
	}
	return ""
}

// ImagePart is an image content part (user messages).
// Image holds the image data as a base64-encoded string or a URL string.
type ImagePart struct {
	Image           string          `json:"image"`
	MediaType       string          `json:"mediaType,omitempty"`
	ProviderOptions json.RawMessage `json:"providerOptions,omitempty"`
}

func (ImagePart) partType() string { return "image" }

// FilePart is a file content part.
// Data holds a URL, data-URI, or base64-encoded string.
type FilePart struct {
	Data             string          `json:"data"`
	MediaType        string          `json:"mediaType"`
	Filename         string          `json:"filename,omitempty"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
	ProviderOptions  json.RawMessage `json:"providerOptions,omitempty"`
}

func (FilePart) partType() string { return "file" }

// ToolCallPart is a tool call invocation (assistant messages).
// Input holds the raw tool input text as accumulated from the provider.
// Most tools use JSON text, but custom grammar tools (for example apply_patch)
// may store non-JSON raw text.
type ToolCallPart struct {
	ToolCallID       string          `json:"toolCallId"`
	ToolName         string          `json:"toolName"`
	Input            string          `json:"-"` // marshaled/unmarshaled via MarshalPart/UnmarshalPart
	ProviderExecuted *bool           `json:"providerExecuted,omitempty"`
	ProviderOptions  json.RawMessage `json:"providerOptions,omitempty"`
}

func (ToolCallPart) partType() string { return "tool-call" }

// ToolResultPart is a tool execution result (tool messages, or
// inline in assistant messages for provider-executed tools).
// Output is marshaled/unmarshaled as a nested discriminated union.
type ToolResultPart struct {
	ToolCallID      string           `json:"toolCallId"`
	ToolName        string           `json:"toolName"`
	Output          ToolResultOutput `json:"-"`
	ProviderOptions json.RawMessage  `json:"providerOptions,omitempty"`
}

func (ToolResultPart) partType() string { return "tool-result" }

// ToolApprovalRequest requests user approval for a tool call.
type ToolApprovalRequest struct {
	ApprovalID string `json:"approvalId"`
	ToolCallID string `json:"toolCallId"`
}

func (ToolApprovalRequest) partType() string { return "tool-approval-request" }

// ToolApprovalResponse responds to a tool approval request.
type ToolApprovalResponse struct {
	ApprovalID       string `json:"approvalId"`
	Approved         bool   `json:"approved"`
	Reason           string `json:"reason,omitempty"`
	ProviderExecuted *bool  `json:"providerExecuted,omitempty"`
}

func (ToolApprovalResponse) partType() string { return "tool-approval-response" }

// SourceURLPart is a URL source reference.
type SourceURLPart struct {
	SourceID         string          `json:"sourceId"`
	URL              string          `json:"url"`
	Title            string          `json:"title,omitempty"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
}

func (SourceURLPart) partType() string { return "source-url" }

// SourceDocumentPart is a document source reference.
type SourceDocumentPart struct {
	SourceID         string          `json:"sourceId"`
	MediaType        string          `json:"mediaType"`
	Title            string          `json:"title"`
	Filename         string          `json:"filename,omitempty"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
}

func (SourceDocumentPart) partType() string { return "source-document" }

// StepStartPart marks a step boundary in the message.
type StepStartPart struct{}

func (StepStartPart) partType() string { return "step-start" }

// DataPart is a custom data part with a type prefix of "data-".
type DataPart struct {
	// DataType is the suffix after "data-" (e.g. "mode-change" for type "data-mode-change").
	DataType string          `json:"-"`
	ID       string          `json:"id,omitempty"`
	Data     json.RawMessage `json:"data"`
}

func (p DataPart) partType() string { return "data-" + p.DataType }

// MarshalPart serializes a Part to JSON, injecting the "type" discriminator
// from partType(). The outer Type field at depth 0 shadows any embedded
// struct's Type field at depth 1, matching the pattern used by MarshalUIPart.
func MarshalPart(p Part) ([]byte, error) {
	t := p.partType()
	switch v := p.(type) {
	case TextPart:
		return json.Marshal(struct {
			Type string `json:"type"`
			TextPart
		}{t, v})
	case ReasoningPart:
		return json.Marshal(struct {
			Type string `json:"type"`
			ReasoningPart
		}{t, v})
	case ImagePart:
		return json.Marshal(struct {
			Type string `json:"type"`
			ImagePart
		}{t, v})
	case FilePart:
		return json.Marshal(struct {
			Type string `json:"type"`
			FilePart
		}{t, v})
	case ToolCallPart:
		inputRaw, inputText := persistedToolInputFields(v.Input)
		return json.Marshal(struct {
			Type             string          `json:"type"`
			ToolCallID       string          `json:"toolCallId"`
			ToolName         string          `json:"toolName"`
			Input            json.RawMessage `json:"input,omitempty"`
			InputText        string          `json:"inputText,omitempty"`
			ProviderExecuted *bool           `json:"providerExecuted,omitempty"`
			ProviderOptions  json.RawMessage `json:"providerOptions,omitempty"`
		}{
			Type:             t,
			ToolCallID:       v.ToolCallID,
			ToolName:         v.ToolName,
			Input:            inputRaw,
			InputText:        inputText,
			ProviderExecuted: v.ProviderExecuted,
			ProviderOptions:  v.ProviderOptions,
		})
	case ToolResultPart:
		outputData, err := MarshalToolResultOutput(v.Output)
		if err != nil {
			return nil, fmt.Errorf("marshal ToolResultPart.Output: %w", err)
		}
		return json.Marshal(struct {
			Type            string          `json:"type"`
			ToolCallID      string          `json:"toolCallId"`
			ToolName        string          `json:"toolName"`
			Output          json.RawMessage `json:"output"`
			ProviderOptions json.RawMessage `json:"providerOptions,omitempty"`
		}{
			Type:            t,
			ToolCallID:      v.ToolCallID,
			ToolName:        v.ToolName,
			Output:          outputData,
			ProviderOptions: v.ProviderOptions,
		})
	case ToolApprovalRequest:
		return json.Marshal(struct {
			Type string `json:"type"`
			ToolApprovalRequest
		}{t, v})
	case ToolApprovalResponse:
		return json.Marshal(struct {
			Type string `json:"type"`
			ToolApprovalResponse
		}{t, v})
	case SourceURLPart:
		return json.Marshal(struct {
			Type string `json:"type"`
			SourceURLPart
		}{t, v})
	case SourceDocumentPart:
		return json.Marshal(struct {
			Type string `json:"type"`
			SourceDocumentPart
		}{t, v})
	case StepStartPart:
		return json.Marshal(struct {
			Type string `json:"type"`
		}{t})
	case DataPart:
		return json.Marshal(struct {
			Type string `json:"type"`
			DataPart
		}{t, v})
	default:
		return nil, fmt.Errorf("unknown Part type: %T", p)
	}
}

// UnmarshalPart deserializes JSON into the appropriate Part variant
// based on the "type" discriminator field.
func UnmarshalPart(data []byte) (Part, error) {
	var disc struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &disc); err != nil {
		return nil, fmt.Errorf("unmarshal Part type discriminator: %w", err)
	}

	switch {
	case disc.Type == "text":
		var p TextPart
		return p, json.Unmarshal(data, &p)
	case disc.Type == "reasoning":
		var p ReasoningPart
		return p, json.Unmarshal(data, &p)
	case disc.Type == "image":
		var p ImagePart
		return p, json.Unmarshal(data, &p)
	case disc.Type == "file":
		var p FilePart
		return p, json.Unmarshal(data, &p)
	case disc.Type == "tool-call":
		var raw struct {
			ToolCallID       string          `json:"toolCallId"`
			ToolName         string          `json:"toolName"`
			Input            json.RawMessage `json:"input"`
			InputText        string          `json:"inputText,omitempty"`
			ProviderExecuted *bool           `json:"providerExecuted,omitempty"`
			ProviderOptions  json.RawMessage `json:"providerOptions,omitempty"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
		input := raw.InputText
		if input == "" {
			input = string(raw.Input)
		}
		return ToolCallPart{
			ToolCallID:       raw.ToolCallID,
			ToolName:         raw.ToolName,
			Input:            input,
			ProviderExecuted: raw.ProviderExecuted,
			ProviderOptions:  raw.ProviderOptions,
		}, nil
	case disc.Type == "tool-result":
		var raw struct {
			ToolCallID      string          `json:"toolCallId"`
			ToolName        string          `json:"toolName"`
			Output          json.RawMessage `json:"output"`
			ProviderOptions json.RawMessage `json:"providerOptions,omitempty"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
		output, err := UnmarshalToolResultOutput(raw.Output)
		if err != nil {
			return nil, fmt.Errorf("unmarshal ToolResultPart.Output: %w", err)
		}
		return ToolResultPart{
			ToolCallID:      raw.ToolCallID,
			ToolName:        raw.ToolName,
			Output:          output,
			ProviderOptions: raw.ProviderOptions,
		}, nil
	case disc.Type == "tool-approval-request":
		var p ToolApprovalRequest
		return p, json.Unmarshal(data, &p)
	case disc.Type == "tool-approval-response":
		var p ToolApprovalResponse
		return p, json.Unmarshal(data, &p)
	case disc.Type == "source-url":
		var p SourceURLPart
		return p, json.Unmarshal(data, &p)
	case disc.Type == "source-document":
		var p SourceDocumentPart
		return p, json.Unmarshal(data, &p)
	case disc.Type == "step-start":
		return StepStartPart{}, nil
	case strings.HasPrefix(disc.Type, "data-"):
		var p DataPart
		if err := json.Unmarshal(data, &p); err != nil {
			return nil, err
		}
		p.DataType = strings.TrimPrefix(disc.Type, "data-")
		return p, nil
	default:
		return nil, fmt.Errorf("unknown Part type: %q", disc.Type)
	}
}
