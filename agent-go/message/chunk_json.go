package message

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MarshalChunk serializes a MessageChunk to JSON, injecting the "type" discriminator.
func MarshalChunk(c MessageChunk) ([]byte, error) {
	switch v := c.(type) {
	// --- Provider: Text ---

	case TextStartChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			TextStartChunk
		}{"text-start", v})
	case TextDeltaChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			TextDeltaChunk
		}{"text-delta", v})
	case TextEndChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			TextEndChunk
		}{"text-end", v})

	// --- Provider: Reasoning ---

	case ReasoningStartChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			ReasoningStartChunk
		}{"reasoning-start", v})
	case ReasoningDeltaChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			ReasoningDeltaChunk
		}{"reasoning-delta", v})
	case ReasoningEndChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			ReasoningEndChunk
		}{"reasoning-end", v})

	// --- Provider: Tool input streaming ---

	case ToolInputStartChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			ToolInputStartChunk
		}{"tool-input-start", v})
	case ToolInputDeltaChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			ToolInputDeltaChunk
		}{"tool-input-delta", v})
	case ToolInputEndChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			ToolInputEndChunk
		}{"tool-input-end", v})

	// --- Provider: Non-streaming tool call ---

	case ToolCallChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			ToolCallChunk
		}{"tool-call", v})

	// --- Provider: Tool result ---

	case ToolResultChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			ToolResultChunk
		}{"tool-result", v})

	// --- Provider: Tool approval ---

	case ToolApprovalRequestChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			ToolApprovalRequestChunk
		}{"tool-approval-request", v})
	case ToolApprovalResponseChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			ToolApprovalResponseChunk
		}{"tool-approval-response", v})

	// --- Provider: File ---

	case FileChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			FileChunk
		}{"file", v})

	// --- Provider: Source ---

	case SourceChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			SourceChunk
		}{"source", v})

	// --- Provider: Stream lifecycle ---

	case StreamStartChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			StreamStartChunk
		}{"stream-start", v})
	case ResponseMetadataChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			ResponseMetadataChunk
		}{"response-metadata", v})
	case FinishChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			FinishChunk
		}{"finish", v})
	case RawChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			RawChunk
		}{"raw", v})
	case ErrorChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			ErrorChunk
		}{"error", v})

	// --- Orchestrator: Tool input ---

	case ToolInputAvailableChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			ToolInputAvailableChunk
		}{"tool-input-available", v})
	case ToolInputErrorChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			ToolInputErrorChunk
		}{"tool-input-error", v})

	// --- Orchestrator: Tool output ---

	case ToolOutputAvailableChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			ToolOutputAvailableChunk
		}{"tool-output-available", v})
	case ToolOutputErrorChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			ToolOutputErrorChunk
		}{"tool-output-error", v})
	case ToolOutputDeniedChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			ToolOutputDeniedChunk
		}{"tool-output-denied", v})

	// --- Orchestrator: Data ---

	case ThreadUpdateChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			ThreadUpdateChunk
		}{"data-thread-update", v})
	case ThreadResumeChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			ThreadResumeChunk
		}{"data-thread-resume", v})
	case ToolApprovalResponseDataChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			ToolApprovalResponseDataChunk
		}{"data-tool-approval-response", v})
	case UserMessageChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			UserMessageChunk
		}{"data-user-message", v})
	case DataChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			DataChunk
		}{"data-" + v.DataType, v})

	// --- Orchestrator: Lifecycle ---

	case StartStepChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
		}{"start-step"})
	case FinishStepChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
		}{"finish-step"})
	case StartChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			StartChunk
		}{"start", v})
	case ResponseFinishChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			ResponseFinishChunk
		}{"finish", v})
	case AbortChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			AbortChunk
		}{"abort", v})
	case MessageMetadataChunk:
		return json.Marshal(struct {
			Type string `json:"type"`
			MessageMetadataChunk
		}{"message-metadata", v})

	default:
		return nil, fmt.Errorf("unknown MessageChunk type: %T", c)
	}
}

// UnmarshalChunk deserializes JSON into the appropriate MessageChunk variant
// based on the "type" discriminator field.
func UnmarshalChunk(data []byte) (MessageChunk, error) {
	var disc struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &disc); err != nil {
		return nil, fmt.Errorf("unmarshal MessageChunk type discriminator: %w", err)
	}

	switch {
	// Text
	case disc.Type == "text-start":
		var c TextStartChunk
		return c, json.Unmarshal(data, &c)
	case disc.Type == "text-delta":
		var c TextDeltaChunk
		return c, json.Unmarshal(data, &c)
	case disc.Type == "text-end":
		var c TextEndChunk
		return c, json.Unmarshal(data, &c)

	// Reasoning
	case disc.Type == "reasoning-start":
		var c ReasoningStartChunk
		return c, json.Unmarshal(data, &c)
	case disc.Type == "reasoning-delta":
		var c ReasoningDeltaChunk
		return c, json.Unmarshal(data, &c)
	case disc.Type == "reasoning-end":
		var c ReasoningEndChunk
		return c, json.Unmarshal(data, &c)

	// Tool input streaming
	case disc.Type == "tool-input-start":
		var c ToolInputStartChunk
		return c, json.Unmarshal(data, &c)
	case disc.Type == "tool-input-delta":
		var c ToolInputDeltaChunk
		return c, json.Unmarshal(data, &c)
	case disc.Type == "tool-input-end":
		var c ToolInputEndChunk
		return c, json.Unmarshal(data, &c)

	// Tool input available/error (orchestrator)
	case disc.Type == "tool-input-available":
		var c ToolInputAvailableChunk
		return c, json.Unmarshal(data, &c)
	case disc.Type == "tool-input-error":
		var c ToolInputErrorChunk
		return c, json.Unmarshal(data, &c)

	// Non-streaming tool call
	case disc.Type == "tool-call":
		var c ToolCallChunk
		return c, json.Unmarshal(data, &c)

	// Tool result (provider-executed)
	case disc.Type == "tool-result":
		var c ToolResultChunk
		return c, json.Unmarshal(data, &c)

	// Tool approval
	case disc.Type == "tool-approval-request":
		var c ToolApprovalRequestChunk
		return c, json.Unmarshal(data, &c)
	case disc.Type == "tool-approval-response":
		var c ToolApprovalResponseChunk
		return c, json.Unmarshal(data, &c)

	// Tool output (orchestrator)
	case disc.Type == "tool-output-available":
		var c ToolOutputAvailableChunk
		return c, json.Unmarshal(data, &c)
	case disc.Type == "tool-output-error":
		var c ToolOutputErrorChunk
		return c, json.Unmarshal(data, &c)
	case disc.Type == "tool-output-denied":
		var c ToolOutputDeniedChunk
		return c, json.Unmarshal(data, &c)

	// File
	case disc.Type == "file":
		var c FileChunk
		return c, json.Unmarshal(data, &c)

	// Source
	case disc.Type == "source":
		var c SourceChunk
		return c, json.Unmarshal(data, &c)

	// Stream lifecycle (provider)
	case disc.Type == "stream-start":
		var c StreamStartChunk
		return c, json.Unmarshal(data, &c)
	case disc.Type == "response-metadata":
		var c ResponseMetadataChunk
		return c, json.Unmarshal(data, &c)
	case disc.Type == "raw":
		var c RawChunk
		return c, json.Unmarshal(data, &c)
	case disc.Type == "error":
		var c ErrorChunk
		return c, json.Unmarshal(data, &c)

	// Orchestrator lifecycle
	case disc.Type == "start-step":
		return StartStepChunk{}, nil
	case disc.Type == "finish-step":
		return FinishStepChunk{}, nil
	case disc.Type == "start":
		var c StartChunk
		return c, json.Unmarshal(data, &c)
	case disc.Type == "abort":
		var c AbortChunk
		return c, json.Unmarshal(data, &c)
	case disc.Type == "message-metadata":
		var c MessageMetadataChunk
		return c, json.Unmarshal(data, &c)

	// finish — ambiguous: could be provider FinishChunk or orchestrator ResponseFinishChunk.
	// We try FinishChunk first (has usage field); if usage is zero-value, it's ResponseFinishChunk.
	case disc.Type == "finish":
		return unmarshalFinishChunk(data)

	// Data chunks
	case disc.Type == "data-thread-update":
		var c ThreadUpdateChunk
		return c, json.Unmarshal(data, &c)
	case disc.Type == "data-thread-resume":
		var c ThreadResumeChunk
		return c, json.Unmarshal(data, &c)
	case disc.Type == "data-tool-approval-response":
		var c ToolApprovalResponseDataChunk
		return c, json.Unmarshal(data, &c)
	case disc.Type == "data-user-message":
		var c UserMessageChunk
		return c, json.Unmarshal(data, &c)
	case strings.HasPrefix(disc.Type, "data-"):
		var c DataChunk
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, err
		}
		c.DataType = strings.TrimPrefix(disc.Type, "data-")
		return c, nil

	default:
		return nil, fmt.Errorf("unknown MessageChunk type: %q", disc.Type)
	}
}

// unmarshalFinishChunk disambiguates between FinishChunk (provider) and
// ResponseFinishChunk (orchestrator). FinishChunk has finishReason as an object
// with unified/raw fields; ResponseFinishChunk has it as a plain string.
func unmarshalFinishChunk(data []byte) (MessageChunk, error) {
	// Peek at finishReason — if it's a string, it's ResponseFinishChunk.
	// If it's an object (or has usage), it's FinishChunk.
	var probe struct {
		FinishReason json.RawMessage `json:"finishReason"`
		Usage        json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, err
	}

	// If usage is present and non-null, it's a provider FinishChunk.
	if len(probe.Usage) > 0 && string(probe.Usage) != "null" {
		var c FinishChunk
		return c, json.Unmarshal(data, &c)
	}

	// Otherwise it's an orchestrator ResponseFinishChunk.
	var c ResponseFinishChunk
	return c, json.Unmarshal(data, &c)
}

// MarshalProviderChunk serializes a ProviderMessageChunk to JSON.
// This is a convenience wrapper around MarshalChunk that accepts the
// narrower ProviderMessageChunk type.
func MarshalProviderChunk(c ProviderMessageChunk) ([]byte, error) {
	return MarshalChunk(c)
}

// UnmarshalProviderChunk deserializes JSON into a ProviderMessageChunk.
// Returns an error if the chunk type is not a provider chunk.
func UnmarshalProviderChunk(data []byte) (ProviderMessageChunk, error) {
	chunk, err := UnmarshalChunk(data)
	if err != nil {
		return nil, err
	}
	pc, ok := chunk.(ProviderMessageChunk)
	if !ok {
		return nil, fmt.Errorf("chunk type %q is not a ProviderMessageChunk", chunk.chunkType())
	}
	return pc, nil
}
