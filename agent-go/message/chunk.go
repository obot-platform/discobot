package message

import (
	"encoding/json"
	"time"
)

// MessageChunk represents a streaming chunk. The concrete type determines
// the "type" discriminator in JSON serialization.
//
//nolint:revive
type MessageChunk interface {
	chunkType() string
}

// ProviderMessageChunk is the subset of MessageChunk that a provider
// can produce from its Complete() method. Every ProviderMessageChunk
// is automatically a MessageChunk.
type ProviderMessageChunk interface {
	MessageChunk
	providerChunk() // marker method to restrict the interface
}

// ============================================================================
// Provider chunks (implement both ProviderMessageChunk and MessageChunk)
// ============================================================================

// --- Text streaming ---

// TextStartChunk begins a new text content block.
type TextStartChunk struct {
	ID               string          `json:"id"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
}

func (TextStartChunk) chunkType() string { return "text-start" }
func (TextStartChunk) providerChunk()    {}

// TextDeltaChunk streams a text content delta.
type TextDeltaChunk struct {
	ID               string          `json:"id"`
	Delta            string          `json:"delta"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
}

func (TextDeltaChunk) chunkType() string { return "text-delta" }
func (TextDeltaChunk) providerChunk()    {}

// TextEndChunk finishes a text content block.
type TextEndChunk struct {
	ID               string          `json:"id"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
}

func (TextEndChunk) chunkType() string { return "text-end" }
func (TextEndChunk) providerChunk()    {}

// --- Reasoning streaming ---

// ReasoningStartChunk begins a new reasoning/thinking block.
type ReasoningStartChunk struct {
	ID               string          `json:"id"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
}

func (ReasoningStartChunk) chunkType() string { return "reasoning-start" }
func (ReasoningStartChunk) providerChunk()    {}

// ReasoningDeltaChunk streams a reasoning content delta.
type ReasoningDeltaChunk struct {
	ID               string          `json:"id"`
	Delta            string          `json:"delta"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
}

func (ReasoningDeltaChunk) chunkType() string { return "reasoning-delta" }
func (ReasoningDeltaChunk) providerChunk()    {}

// ReasoningEndChunk finishes a reasoning/thinking block.
type ReasoningEndChunk struct {
	ID               string          `json:"id"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
}

func (ReasoningEndChunk) chunkType() string { return "reasoning-end" }
func (ReasoningEndChunk) providerChunk()    {}

// --- Tool input streaming ---

// ToolInputStartChunk begins a streaming tool call.
type ToolInputStartChunk struct {
	ToolCallID       string          `json:"toolCallId"`
	ToolName         string          `json:"toolName"`
	ProviderExecuted *bool           `json:"providerExecuted,omitempty"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
	Dynamic          *bool           `json:"dynamic,omitempty"`
	Title            string          `json:"title,omitempty"`
}

func (ToolInputStartChunk) chunkType() string { return "tool-input-start" }
func (ToolInputStartChunk) providerChunk()    {}

// ToolInputDeltaChunk streams a partial tool call input as text.
type ToolInputDeltaChunk struct {
	ToolCallID     string `json:"toolCallId"`
	InputTextDelta string `json:"inputTextDelta"`
}

func (ToolInputDeltaChunk) chunkType() string { return "tool-input-delta" }
func (ToolInputDeltaChunk) providerChunk()    {}

// ToolInputEndChunk finishes a streaming tool call input.
// The orchestrator converts this to a ToolInputAvailableChunk after
// accumulating the full input from deltas.
type ToolInputEndChunk struct {
	ToolCallID       string          `json:"toolCallId"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
}

func (ToolInputEndChunk) chunkType() string { return "tool-input-end" }
func (ToolInputEndChunk) providerChunk()    {}

// --- Non-streaming tool call ---

// ToolCallChunk is a complete, non-streaming tool call.
// Some providers return tool calls fully formed rather than streaming the input.
type ToolCallChunk struct {
	ToolCallID       string          `json:"toolCallId"`
	ToolName         string          `json:"toolName"`
	Input            string          `json:"input"` // Raw tool input text (JSON or custom text)
	ProviderExecuted *bool           `json:"providerExecuted,omitempty"`
	Dynamic          *bool           `json:"dynamic,omitempty"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
}

func (ToolCallChunk) chunkType() string { return "tool-call" }
func (ToolCallChunk) providerChunk()    {}

// --- Tool result (provider-executed) ---

// ToolResultChunk is the result of a provider-executed tool call.
// This only appears when the provider itself executed the tool (e.g., code interpreter).
type ToolResultChunk struct {
	ToolCallID       string          `json:"toolCallId"`
	ToolName         string          `json:"toolName"`
	Result           json.RawMessage `json:"result"`
	IsError          *bool           `json:"isError,omitempty"`
	Preliminary      *bool           `json:"preliminary,omitempty"`
	Dynamic          *bool           `json:"dynamic,omitempty"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
}

func (ToolResultChunk) chunkType() string { return "tool-result" }
func (ToolResultChunk) providerChunk()    {}

// --- Tool approval request ---

// ToolApprovalRequestChunk requests user approval before executing a tool call.
type ToolApprovalRequestChunk struct {
	ApprovalID       string          `json:"approvalId"`
	ToolCallID       string          `json:"toolCallId"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
}

func (ToolApprovalRequestChunk) chunkType() string { return "tool-approval-request" }
func (ToolApprovalRequestChunk) providerChunk()    {}

// --- File ---

// FileChunk is a file produced by the model (e.g., generated image).
type FileChunk struct {
	MediaType        string          `json:"mediaType"`
	Data             string          `json:"data"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
}

func (FileChunk) chunkType() string { return "file" }
func (FileChunk) providerChunk()    {}

// --- Source ---

// SourceChunk is a source reference (URL or document) cited by the model.
// SourceType is "url" or "document".
type SourceChunk struct {
	SourceType       string          `json:"sourceType"`
	SourceID         string          `json:"sourceId"`
	URL              string          `json:"url,omitempty"`
	MediaType        string          `json:"mediaType,omitempty"`
	Title            string          `json:"title,omitempty"`
	Filename         string          `json:"filename,omitempty"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
}

func (SourceChunk) chunkType() string { return "source" }
func (SourceChunk) providerChunk()    {}

// --- Stream lifecycle (provider) ---

// StreamStartChunk signals the beginning of a stream with any warnings.
type StreamStartChunk struct {
	Warnings []Warning `json:"warnings,omitempty"`
}

func (StreamStartChunk) chunkType() string { return "stream-start" }
func (StreamStartChunk) providerChunk()    {}

// ResponseMetadataChunk provides metadata about the response.
type ResponseMetadataChunk struct {
	ID        string     `json:"id,omitempty"`
	Timestamp *time.Time `json:"timestamp,omitempty"`
	ModelID   string     `json:"modelId,omitempty"`
}

func (ResponseMetadataChunk) chunkType() string { return "response-metadata" }
func (ResponseMetadataChunk) providerChunk()    {}

// FinishChunk signals the end of the provider response with usage statistics.
type FinishChunk struct {
	FinishReason     FinishReason    `json:"finishReason"`
	Usage            Usage           `json:"usage"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
}

func (FinishChunk) chunkType() string { return "finish" }
func (FinishChunk) providerChunk()    {}

// RawChunk provides the raw, unprocessed chunk from the provider.
type RawChunk struct {
	RawValue json.RawMessage `json:"rawValue"`
}

func (RawChunk) chunkType() string { return "raw" }
func (RawChunk) providerChunk()    {}

// ErrorChunk signals a streaming error.
type ErrorChunk struct {
	ErrorText string `json:"errorText"`
	Err       error  `json:"-"` // original error, not serialized
}

func (ErrorChunk) chunkType() string { return "error" }
func (ErrorChunk) providerChunk()    {}

// ============================================================================
// Orchestrator-only chunks (implement MessageChunk but NOT ProviderMessageChunk)
// ============================================================================

// --- Tool input available (orchestrator produces after accumulating deltas) ---

// ToolInputAvailableChunk signals that full tool input is available.
type ToolInputAvailableChunk struct {
	ToolCallID       string          `json:"toolCallId"`
	ToolName         string          `json:"toolName"`
	Input            json.RawMessage `json:"input"`
	ProviderExecuted *bool           `json:"providerExecuted,omitempty"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
	Dynamic          *bool           `json:"dynamic,omitempty"`
	Title            string          `json:"title,omitempty"`
}

func (ToolInputAvailableChunk) chunkType() string { return "tool-input-available" }

// ToolInputErrorChunk signals an error during tool input parsing.
type ToolInputErrorChunk struct {
	ToolCallID       string          `json:"toolCallId"`
	ToolName         string          `json:"toolName"`
	Input            json.RawMessage `json:"input"`
	ProviderExecuted *bool           `json:"providerExecuted,omitempty"`
	ProviderMetadata json.RawMessage `json:"providerMetadata,omitempty"`
	Dynamic          *bool           `json:"dynamic,omitempty"`
	ErrorText        string          `json:"errorText"`
	Title            string          `json:"title,omitempty"`
}

func (ToolInputErrorChunk) chunkType() string { return "tool-input-error" }

// --- Tool output (orchestrator produces after tool execution) ---

// ToolOutputAvailableChunk signals that tool output is available.
type ToolOutputAvailableChunk struct {
	ToolCallID       string          `json:"toolCallId"`
	Output           json.RawMessage `json:"output"`
	ProviderExecuted *bool           `json:"providerExecuted,omitempty"`
	Dynamic          *bool           `json:"dynamic,omitempty"`
	Preliminary      *bool           `json:"preliminary,omitempty"`
}

func (ToolOutputAvailableChunk) chunkType() string { return "tool-output-available" }

// ToolOutputErrorChunk signals an error from tool execution.
type ToolOutputErrorChunk struct {
	ToolCallID       string `json:"toolCallId"`
	ErrorText        string `json:"errorText"`
	ProviderExecuted *bool  `json:"providerExecuted,omitempty"`
	Dynamic          *bool  `json:"dynamic,omitempty"`
}

func (ToolOutputErrorChunk) chunkType() string { return "tool-output-error" }

// ToolOutputDeniedChunk signals that tool execution was denied by the user.
type ToolOutputDeniedChunk struct {
	ToolCallID string `json:"toolCallId"`
}

func (ToolOutputDeniedChunk) chunkType() string { return "tool-output-denied" }

// --- Data chunks ---

// DataChunk is a custom data chunk with a type prefix of "data-".
type DataChunk struct {
	// DataType is the suffix after "data-" (e.g. "mode-change" for type "data-mode-change").
	DataType  string          `json:"-"`
	ID        string          `json:"id,omitempty"`
	Data      json.RawMessage `json:"data"`
	Transient *bool           `json:"transient,omitempty"`
}

func (c DataChunk) chunkType() string { return "data-" + c.DataType }

// ModeChangeChunk is a transient data chunk that signals a mode change.
type ModeChangeChunk struct {
	Data      ModeChangeData `json:"data"`
	Transient *bool          `json:"transient,omitempty"`
}

func (ModeChangeChunk) chunkType() string { return "data-mode-change" }

// UserMessageData is the payload for a user message stream chunk.
type UserMessageData struct {
	Message               Message `json:"message"`
	InsertBeforeMessageID string  `json:"insertBeforeMessageId,omitempty"`
}

// UserMessageChunk carries the user message that initiated the current turn.
// It is emitted before the StartChunk so consumers know which user message
// triggered the response stream.
type UserMessageChunk struct {
	Data UserMessageData `json:"data"`
}

func (UserMessageChunk) chunkType() string { return "data-user-message" }

// --- Orchestrator lifecycle ---

// StartStepChunk marks the beginning of a tool use loop iteration.
type StartStepChunk struct{}

func (StartStepChunk) chunkType() string { return "start-step" }

// FinishStepChunk marks the end of a tool use loop iteration.
type FinishStepChunk struct{}

func (FinishStepChunk) chunkType() string { return "finish-step" }

// StartChunk begins a new message response.
type StartChunk struct {
	MessageID       string          `json:"messageId,omitempty"`
	MessageMetadata json.RawMessage `json:"messageMetadata,omitempty"`
}

func (StartChunk) chunkType() string { return "start" }

// ResponseFinishChunk completes a message response.
// Named ResponseFinishChunk to avoid collision with the provider FinishChunk.
// Both serialize as "finish" in JSON but in different contexts (SSE vs step JSONL).
type ResponseFinishChunk struct {
	FinishReason    string          `json:"finishReason,omitempty"`
	MessageMetadata json.RawMessage `json:"messageMetadata,omitempty"`
}

func (ResponseFinishChunk) chunkType() string { return "finish" }

// AbortChunk signals that the stream was aborted.
type AbortChunk struct {
	Reason string `json:"reason,omitempty"`
}

func (AbortChunk) chunkType() string { return "abort" }

// MessageMetadataChunk updates the message metadata mid-stream.
//
//nolint:revive
type MessageMetadataChunk struct {
	MessageMetadata json.RawMessage `json:"messageMetadata"`
}

func (MessageMetadataChunk) chunkType() string { return "message-metadata" }
