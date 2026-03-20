package message

import (
	"encoding/json"
	"fmt"
	"strings"
)

// UIPart represents a content part within a UIMessage.
// The concrete type determines the "type" discriminator in JSON.
type UIPart interface {
	uiPartType() string
}

// UITextPart is a text content part in the AI SDK v6 UIMessage format.
type UITextPart struct {
	Type             string          `json:"type"`
	Text             string          `json:"text"`
	State            string          `json:"state"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
}

func (UITextPart) uiPartType() string { return "text" }

// UIReasoningPart is a reasoning/thinking content part in the AI SDK v6 UIMessage format.
type UIReasoningPart struct {
	Type             string          `json:"type"`
	Text             string          `json:"text"`
	State            string          `json:"state"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
}

func (UIReasoningPart) uiPartType() string { return "reasoning" }

// UIFilePart is a file content part in the AI SDK v6 UIMessage format.
type UIFilePart struct {
	Type             string          `json:"type"`
	URL              string          `json:"url"`
	MediaType        string          `json:"mediaType"`
	Filename         string          `json:"filename,omitempty"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
}

func (UIFilePart) uiPartType() string { return "file" }

// UIStepStartPart marks the start of an agent step in the AI SDK v6 UIMessage format.
type UIStepStartPart struct {
	Type string `json:"type"`
}

func (UIStepStartPart) uiPartType() string { return "step-start" }

// UISourceURLPart is a URL source reference in the AI SDK v6 UIMessage format.
type UISourceURLPart struct {
	Type             string          `json:"type"`
	SourceID         string          `json:"sourceId"`
	URL              string          `json:"url"`
	Title            string          `json:"title,omitempty"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
}

func (UISourceURLPart) uiPartType() string { return "source-url" }

// UISourceDocumentPart is a document source reference in the AI SDK v6 UIMessage format.
type UISourceDocumentPart struct {
	Type             string          `json:"type"`
	SourceID         string          `json:"sourceId"`
	MediaType        string          `json:"mediaType"`
	Title            string          `json:"title"`
	Filename         string          `json:"filename,omitempty"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
}

func (UISourceDocumentPart) uiPartType() string { return "source-document" }

// UIDataPart is a custom data part in the AI SDK v6 UIMessage format.
// Type contains the full "data-{dataType}" discriminator string.
type UIDataPart struct {
	Type string          `json:"type"`
	ID   string          `json:"id,omitempty"`
	Data json.RawMessage `json:"data"`
}

func (p UIDataPart) uiPartType() string { return p.Type }

// MarshalUIPart serializes a UIPart to JSON, always injecting the correct
// "type" discriminator from uiPartType() regardless of the struct's Type field
// value. The outer Type field at depth 0 shadows the embedded struct's Type
// field at depth 1, matching the pattern used by MarshalPart.
func MarshalUIPart(p UIPart) (json.RawMessage, error) {
	t := p.uiPartType()
	var (
		data []byte
		err  error
	)
	switch v := p.(type) {
	case UITextPart:
		data, err = json.Marshal(struct {
			Type string `json:"type"`
			UITextPart
		}{t, v})
	case UIReasoningPart:
		data, err = json.Marshal(struct {
			Type string `json:"type"`
			UIReasoningPart
		}{t, v})
	case UIFilePart:
		data, err = json.Marshal(struct {
			Type string `json:"type"`
			UIFilePart
		}{t, v})
	case UIStepStartPart:
		data, err = json.Marshal(struct {
			Type string `json:"type"`
		}{t})
	case UISourceURLPart:
		data, err = json.Marshal(struct {
			Type string `json:"type"`
			UISourceURLPart
		}{t, v})
	case UISourceDocumentPart:
		data, err = json.Marshal(struct {
			Type string `json:"type"`
			UISourceDocumentPart
		}{t, v})
	case UIDataPart:
		data, err = json.Marshal(struct {
			Type string `json:"type"`
			UIDataPart
		}{t, v})
	case DynamicToolPart:
		data, err = json.Marshal(struct {
			Type string `json:"type"`
			DynamicToolPart
		}{t, v})
	default:
		return nil, fmt.Errorf("unknown UIPart type: %T", p)
	}
	if err != nil {
		return nil, fmt.Errorf("marshal UIPart (%T): %w", p, err)
	}
	return data, nil
}

// UnmarshalUIPart deserializes JSON into the appropriate UIPart variant
// based on the "type" discriminator field.
func UnmarshalUIPart(data []byte) (UIPart, error) {
	var disc struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &disc); err != nil {
		return nil, fmt.Errorf("unmarshal UIPart type: %w", err)
	}
	switch {
	case disc.Type == "text":
		var p UITextPart
		return p, json.Unmarshal(data, &p)
	case disc.Type == "reasoning":
		var p UIReasoningPart
		return p, json.Unmarshal(data, &p)
	case disc.Type == "file":
		var p UIFilePart
		return p, json.Unmarshal(data, &p)
	case disc.Type == "step-start":
		var p UIStepStartPart
		return p, json.Unmarshal(data, &p)
	case disc.Type == "source-url":
		var p UISourceURLPart
		return p, json.Unmarshal(data, &p)
	case disc.Type == "source-document":
		var p UISourceDocumentPart
		return p, json.Unmarshal(data, &p)
	case disc.Type == "dynamic-tool":
		var p DynamicToolPart
		return p, json.Unmarshal(data, &p)
	case strings.HasPrefix(disc.Type, "data-"):
		var p UIDataPart
		return p, json.Unmarshal(data, &p)
	default:
		return nil, fmt.Errorf("unknown UIPart type: %q", disc.Type)
	}
}

// UIPartToPart converts a UI-formatted part into the agent's internal Part form.
// Parts that exist only in UI projection format are not convertible.
func UIPartToPart(p UIPart) (Part, bool) {
	switch v := p.(type) {
	case UITextPart:
		return TextPart{
			Text:             v.Text,
			State:            v.State,
			ProviderMetadata: v.ProviderMetadata,
		}, true
	case UIReasoningPart:
		return ReasoningPart{
			Text:             v.Text,
			State:            v.State,
			ProviderMetadata: v.ProviderMetadata,
		}, true
	case UIFilePart:
		if strings.HasPrefix(v.MediaType, "image/") {
			return ImagePart{
				Image:     v.URL,
				MediaType: v.MediaType,
			}, true
		}
		return FilePart{
			Data:             v.URL,
			MediaType:        v.MediaType,
			Filename:         v.Filename,
			ProviderMetadata: v.ProviderMetadata,
		}, true
	case UIStepStartPart:
		return StepStartPart{}, true
	case UISourceURLPart:
		return SourceURLPart{
			SourceID:         v.SourceID,
			URL:              v.URL,
			Title:            v.Title,
			ProviderMetadata: v.ProviderMetadata,
		}, true
	case UISourceDocumentPart:
		return SourceDocumentPart{
			SourceID:         v.SourceID,
			MediaType:        v.MediaType,
			Title:            v.Title,
			Filename:         v.Filename,
			ProviderMetadata: v.ProviderMetadata,
		}, true
	case UIDataPart:
		return DataPart{
			DataType: strings.TrimPrefix(v.Type, "data-"),
			ID:       v.ID,
			Data:     v.Data,
		}, true
	default:
		return nil, false
	}
}

// UIPartsToParts converts UI-formatted parts into internal Parts, skipping
// any parts that have no internal representation.
func UIPartsToParts(parts []UIPart) []Part {
	if len(parts) == 0 {
		return nil
	}
	result := make([]Part, 0, len(parts))
	for _, p := range parts {
		part, ok := UIPartToPart(p)
		if ok {
			result = append(result, part)
		}
	}
	return result
}
