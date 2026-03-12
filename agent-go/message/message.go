package message

import (
	"encoding/json"
	"fmt"
	"time"
)

// Message represents a chat message. It serves as both the internal
// representation and the storage format. It can be projected to the
// provider wire format (for LLM API calls) via MarshalProviderJSON
// or to the UI format (for the frontend) via ProjectUIMessages.
type Message struct {
	// ID is the message identifier. Used in UI format and internal storage;
	// empty for provider-constructed messages.
	ID string `json:"id,omitempty"`

	// ProviderResponseID stores the provider's opaque response/message ID in the
	// internal storage format. It is not projected to the UI or provider wire
	// format, which lets the thread layer keep a stable UI message ID while still
	// preserving provider-native IDs for follow-up turns.
	ProviderResponseID string `json:"providerResponseId,omitempty"`

	// Role is "system", "user", "assistant", or "tool".
	Role string `json:"role"`

	// Parts contains the message content.
	Parts []Part `json:"-"`

	// ProviderOptions is an opaque JSON blob for provider-specific
	// parameters attached to this message.
	ProviderOptions json.RawMessage `json:"providerOptions,omitempty"`

	// Metadata is UI-specific metadata (e.g., usage info).
	Metadata json.RawMessage `json:"metadata,omitempty"`

	// CreatedAt is when the message was created. UI display only.
	CreatedAt *time.Time `json:"createdAt,omitempty"`

	// Synthetic marks messages that are internal/injected (system prompt,
	// user instructions, runtime reminders) and must not be projected to the UI.
	Synthetic bool `json:"synthetic,omitempty"`
}

// MarshalJSON implements json.Marshaler.
// This is the internal format that preserves ALL fields (used for disk storage).
// Parts are serialized as an array with type discriminators.
func (m Message) MarshalJSON() ([]byte, error) {
	parts := make([]json.RawMessage, len(m.Parts))
	for i, p := range m.Parts {
		data, err := MarshalPart(p)
		if err != nil {
			return nil, fmt.Errorf("marshal Message.Parts[%d]: %w", i, err)
		}
		parts[i] = data
	}

	return json.Marshal(struct {
		ID                 string            `json:"id,omitempty"`
		ProviderResponseID string            `json:"providerResponseId,omitempty"`
		Role               string            `json:"role"`
		Parts              []json.RawMessage `json:"parts"`
		ProviderOptions    json.RawMessage   `json:"providerOptions,omitempty"`
		Metadata           json.RawMessage   `json:"metadata,omitempty"`
		CreatedAt          *time.Time        `json:"createdAt,omitempty"`
		Synthetic          bool              `json:"synthetic,omitempty"`
	}{
		ID:                 m.ID,
		ProviderResponseID: m.ProviderResponseID,
		Role:               m.Role,
		Parts:              parts,
		ProviderOptions:    m.ProviderOptions,
		Metadata:           m.Metadata,
		CreatedAt:          m.CreatedAt,
		Synthetic:          m.Synthetic,
	})
}

// UnmarshalJSON implements json.Unmarshaler.
// Handles the internal format where parts are an array of typed objects.
// Also handles the provider format where system content is a plain string.
func (m *Message) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID                 string          `json:"id,omitempty"`
		ProviderResponseID string          `json:"providerResponseId,omitempty"`
		Role               string          `json:"role"`
		Parts              json.RawMessage `json:"parts"`
		Content            json.RawMessage `json:"content"`
		ProviderOptions    json.RawMessage `json:"providerOptions,omitempty"`
		Metadata           json.RawMessage `json:"metadata,omitempty"`
		CreatedAt          *time.Time      `json:"createdAt,omitempty"`
		Synthetic          bool            `json:"synthetic,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("unmarshal Message: %w", err)
	}

	m.ID = raw.ID
	m.ProviderResponseID = raw.ProviderResponseID
	m.Role = raw.Role
	m.ProviderOptions = raw.ProviderOptions
	m.Metadata = raw.Metadata
	m.CreatedAt = raw.CreatedAt
	m.Synthetic = raw.Synthetic

	// Determine which field has the parts: "parts" (internal format)
	// or "content" (provider format).
	partsData := raw.Parts
	if len(partsData) == 0 || string(partsData) == "null" {
		partsData = raw.Content
	}

	if len(partsData) == 0 || string(partsData) == "null" {
		m.Parts = nil
		return nil
	}

	// Try string content (provider system messages).
	var str string
	if err := json.Unmarshal(partsData, &str); err == nil {
		m.Parts = []Part{TextPart{Text: str}}
		return nil
	}

	// Array of parts.
	var rawParts []json.RawMessage
	if err := json.Unmarshal(partsData, &rawParts); err != nil {
		return fmt.Errorf("unmarshal Message parts: %w", err)
	}

	m.Parts = make([]Part, len(rawParts))
	for i, partData := range rawParts {
		p, err := UnmarshalPart(partData)
		if err != nil {
			return fmt.Errorf("unmarshal Message.Parts[%d]: %w", i, err)
		}
		m.Parts[i] = p
	}

	return nil
}

// MarshalProviderJSON serializes the Message in the provider wire format.
// System messages serialize content as a plain string.
// UI-only parts (SourceURL, SourceDocument, StepStart, Data) are excluded.
// ID, Metadata, and CreatedAt are excluded.
func (m Message) MarshalProviderJSON() ([]byte, error) {
	var contentJSON json.RawMessage
	var err error

	if m.Role == "system" {
		text := ""
		if len(m.Parts) == 1 {
			if tp, ok := m.Parts[0].(TextPart); ok {
				text = tp.Text
			}
		}
		contentJSON, err = json.Marshal(text)
		if err != nil {
			return nil, fmt.Errorf("marshal system content: %w", err)
		}
	} else {
		var parts []json.RawMessage
		for _, p := range m.Parts {
			if isUIOnlyPart(p) {
				continue
			}
			data, err := MarshalPart(p)
			if err != nil {
				return nil, fmt.Errorf("marshal provider content part: %w", err)
			}
			parts = append(parts, data)
		}
		contentJSON, err = json.Marshal(parts)
		if err != nil {
			return nil, fmt.Errorf("marshal provider content array: %w", err)
		}
	}

	return json.Marshal(struct {
		Role            string          `json:"role"`
		Content         json.RawMessage `json:"content"`
		ProviderOptions json.RawMessage `json:"providerOptions,omitempty"`
	}{
		Role:            m.Role,
		Content:         contentJSON,
		ProviderOptions: m.ProviderOptions,
	})
}

// UnmarshalProviderJSON deserializes the provider wire format into a Message.
func (m *Message) UnmarshalProviderJSON(data []byte) error {
	var raw struct {
		Role            string          `json:"role"`
		Content         json.RawMessage `json:"content"`
		ProviderOptions json.RawMessage `json:"providerOptions,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("unmarshal provider Message: %w", err)
	}

	m.Role = raw.Role
	m.ProviderOptions = raw.ProviderOptions

	// String content (system messages).
	var str string
	if err := json.Unmarshal(raw.Content, &str); err == nil {
		m.Parts = []Part{TextPart{Text: str}}
		return nil
	}

	// Array of parts.
	var rawParts []json.RawMessage
	if err := json.Unmarshal(raw.Content, &rawParts); err != nil {
		return fmt.Errorf("unmarshal provider Message content: %w", err)
	}

	m.Parts = make([]Part, len(rawParts))
	for i, partData := range rawParts {
		p, err := UnmarshalPart(partData)
		if err != nil {
			return fmt.Errorf("unmarshal provider Message content[%d]: %w", i, err)
		}
		m.Parts[i] = p
	}

	return nil
}

// isUIOnlyPart returns true for parts that should be excluded from
// the provider wire format.
func isUIOnlyPart(p Part) bool {
	switch p.(type) {
	case SourceURLPart, SourceDocumentPart, StepStartPart, DataPart:
		return true
	default:
		return false
	}
}
