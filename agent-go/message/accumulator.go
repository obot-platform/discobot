package message

import (
	"strings"
	"time"
)

// ChunkAccumulator consolidates streaming ProviderMessageChunks from a single
// provider Complete() call into a Message.
//
// Usage:
//
//	acc := NewChunkAccumulator()
//	for chunk, err := range provider.Complete(ctx, req) {
//	    if err != nil { ... }
//	    acc.Push(chunk)
//	}
//	acc.Close()
//	msg := acc.Message()
type ChunkAccumulator struct {
	parts []Part

	// Active streaming blocks: ID → index in parts.
	activeText      map[string]int
	activeReasoning map[string]int
	activeToolInput map[string]*activeToolInput

	// Metadata collected from the stream.
	sources  []SourceChunk
	finish   *FinishChunk
	respMeta *ResponseMetadataChunk
	warnings []Warning
	errors   []ErrorChunk

	closed bool
}

type activeToolInput struct {
	index            int
	toolName         string
	inputBuf         string
	providerExecuted *bool
}

// NewChunkAccumulator creates a new empty accumulator.
func NewChunkAccumulator() *ChunkAccumulator {
	return &ChunkAccumulator{
		activeText:      make(map[string]int),
		activeReasoning: make(map[string]int),
		activeToolInput: make(map[string]*activeToolInput),
	}
}

// Push processes a single ProviderMessageChunk and updates the accumulated state.
func (a *ChunkAccumulator) Push(chunk ProviderMessageChunk) {
	if a.closed {
		return
	}

	switch c := chunk.(type) {
	// --- Text ---

	case TextStartChunk:
		idx := len(a.parts)
		a.parts = append(a.parts, TextPart{ID: c.ID})
		a.activeText[c.ID] = idx

	case TextDeltaChunk:
		if idx, ok := a.activeText[c.ID]; ok {
			p := a.parts[idx].(TextPart)
			p.Text += c.Delta
			a.parts[idx] = p
		}

	case TextEndChunk:
		delete(a.activeText, c.ID)

	// --- Reasoning ---

	case ReasoningStartChunk:
		idx := len(a.parts)
		a.parts = append(a.parts, ReasoningPart{ID: c.ID})
		a.activeReasoning[c.ID] = idx

	case ReasoningDeltaChunk:
		if idx, ok := a.activeReasoning[c.ID]; ok {
			p := a.parts[idx].(ReasoningPart)
			p.Text += c.Delta
			a.parts[idx] = p
		}

	case ReasoningEndChunk:
		if idx, ok := a.activeReasoning[c.ID]; ok {
			if len(c.ProviderMetadata) > 0 {
				p := a.parts[idx].(ReasoningPart)
				p.ProviderMetadata = c.ProviderMetadata
				a.parts[idx] = p
			}
		}
		delete(a.activeReasoning, c.ID)

	// --- Tool input (streaming) ---

	case ToolInputStartChunk:
		idx := len(a.parts)
		a.parts = append(a.parts, ToolCallPart{
			ToolCallID:       c.ToolCallID,
			ToolName:         c.ToolName,
			ProviderExecuted: c.ProviderExecuted,
		})
		a.activeToolInput[c.ToolCallID] = &activeToolInput{
			index:            idx,
			toolName:         c.ToolName,
			providerExecuted: c.ProviderExecuted,
		}

	case ToolInputDeltaChunk:
		if active, ok := a.activeToolInput[c.ToolCallID]; ok {
			active.inputBuf += c.InputTextDelta
		}

	case ToolInputEndChunk:
		if active, ok := a.activeToolInput[c.ToolCallID]; ok {
			p := a.parts[active.index].(ToolCallPart)
			p.Input = strings.TrimSpace(active.inputBuf)
			a.parts[active.index] = p
			delete(a.activeToolInput, c.ToolCallID)
		}

	// --- Tool call (non-streaming) ---

	case ToolCallChunk:
		a.parts = append(a.parts, ToolCallPart{
			ToolCallID:       c.ToolCallID,
			ToolName:         c.ToolName,
			Input:            c.Input,
			ProviderExecuted: c.ProviderExecuted,
		})

	// --- Tool result (provider-executed) ---

	case ToolResultChunk:
		var output ToolResultOutput
		if c.IsError != nil && *c.IsError {
			output = ErrorJSONOutput{Value: c.Result}
		} else {
			output = JSONOutput{Value: c.Result}
		}
		a.parts = append(a.parts, ToolResultPart{
			ToolCallID: c.ToolCallID,
			ToolName:   c.ToolName,
			Output:     output,
		})

	// --- Tool approval ---

	case ToolApprovalRequestChunk:
		a.parts = append(a.parts, ToolApprovalRequest{
			ApprovalID: c.ApprovalID,
			ToolCallID: c.ToolCallID,
		})

	// --- File ---

	case FileChunk:
		a.parts = append(a.parts, FilePart{
			Data:      c.Data,
			MediaType: c.MediaType,
		})

	// --- Source ---

	case SourceChunk:
		a.sources = append(a.sources, c)

	// --- Stream lifecycle ---

	case StreamStartChunk:
		a.warnings = c.Warnings

	case FinishChunk:
		a.finish = &c

	case ResponseMetadataChunk:
		a.respMeta = &c

	case ErrorChunk:
		a.errors = append(a.errors, c)

	case RawChunk:
		// Not accumulated into the message.
	}
}

// Close finalizes all in-progress streaming state. Active text and reasoning
// blocks are kept as-is (their accumulated text is already in the parts slice).
// Active tool inputs are finalized with whatever JSON has accumulated so far.
//
// This is safe to call multiple times; subsequent calls are no-ops.
func (a *ChunkAccumulator) Close() {
	if a.closed {
		return
	}
	a.closed = true

	// Active text/reasoning: text is already accumulated in the parts slice.
	clear(a.activeText)
	clear(a.activeReasoning)

	// Finalize active tool inputs with whatever JSON has accumulated so far.
	// Validity is not checked here — tool execution will surface any parse error.
	for id, active := range a.activeToolInput {
		p := a.parts[active.index].(ToolCallPart)
		p.Input = strings.TrimSpace(active.inputBuf)
		a.parts[active.index] = p
		delete(a.activeToolInput, id)
	}
}

// Message returns the accumulated assistant Message.
// If the provider supplied a response ID via ResponseMetadataChunk, it is used
// as the message ID. Otherwise, the caller is expected to assign one.
func (a *ChunkAccumulator) Message() Message {
	parts := make([]Part, len(a.parts))
	copy(parts, a.parts)
	msg := Message{Role: "assistant", Parts: parts}
	if a.respMeta != nil && a.respMeta.ID != "" {
		msg.ID = a.respMeta.ID
		msg.ProviderResponseID = a.respMeta.ID
	}
	if a.respMeta != nil && a.respMeta.Timestamp != nil {
		ts := a.respMeta.Timestamp.UTC()
		msg.CreatedAt = &ts
	} else {
		now := time.Now().UTC()
		msg.CreatedAt = &now
	}
	return msg
}

// FinishResult returns the FinishChunk received from the provider, or nil
// if the stream hasn't finished yet.
func (a *ChunkAccumulator) FinishResult() *FinishChunk {
	return a.finish
}

// ResponseMeta returns the ResponseMetadataChunk, or nil if none was received.
func (a *ChunkAccumulator) ResponseMeta() *ResponseMetadataChunk {
	return a.respMeta
}

// Sources returns all source references received during the stream.
func (a *ChunkAccumulator) Sources() []SourceChunk {
	return a.sources
}

// Warnings returns any warnings from the StreamStartChunk.
func (a *ChunkAccumulator) Warnings() []Warning {
	return a.warnings
}

// Errors returns any provider error chunks received during the stream.
func (a *ChunkAccumulator) Errors() []ErrorChunk {
	return a.errors
}
