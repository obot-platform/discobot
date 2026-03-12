package message

import (
	"encoding/json"
	"fmt"
)

// ChunkExpander maps provider ProviderMessageChunks to MessageChunks for
// SSE delivery to the frontend. It is stateful because tool input streaming
// requires buffering deltas to produce a ToolInputAvailableChunk with the
// full input on ToolInputEnd.
//
// Usage:
//
//	exp := NewChunkExpander(stepIndex == 0)
//	for chunk, err := range provider.Complete(ctx, req) {
//	    for _, mc := range exp.Expand(chunk) {
//	        // send mc to frontend via SSE
//	    }
//	}
type ChunkExpander struct {
	activeToolInputs  map[string]*expanderToolInput
	suppressStartStep bool // suppress the StartStepChunk for the first step
}

type expanderToolInput struct {
	toolName         string
	inputBuf         string
	providerExecuted *bool
	dynamic          *bool
	title            string
}

// NewChunkExpander creates a new stateful chunk expander.
// isFirstStep should be true for step index 0: it suppresses the StartStepChunk
// that would otherwise be emitted for the leading StreamStartChunk, matching the
// ui_projection behaviour of omitting step-start before the first step.
func NewChunkExpander(isFirstStep bool) *ChunkExpander {
	return &ChunkExpander{
		activeToolInputs:  make(map[string]*expanderToolInput),
		suppressStartStep: isFirstStep,
	}
}

// Expand maps a single ProviderMessageChunk to zero or more MessageChunks.
// Most chunks map 1:1. ToolCallChunk (non-streaming) produces two chunks.
// Lifecycle/metadata chunks that have no direct frontend equivalent return nil.
// FinishStepChunk is emitted by the turn loop once the step's tool results have
// been streamed, so provider FinishChunk is dropped here.
func (e *ChunkExpander) Expand(chunk ProviderMessageChunk) []MessageChunk {
	switch v := chunk.(type) {
	// --- Text (1:1 passthrough) ---

	case TextStartChunk:
		return []MessageChunk{v}
	case TextDeltaChunk:
		return []MessageChunk{v}
	case TextEndChunk:
		return []MessageChunk{v}

	// --- Reasoning (1:1 passthrough) ---

	case ReasoningStartChunk:
		return []MessageChunk{v}
	case ReasoningDeltaChunk:
		return []MessageChunk{v}
	case ReasoningEndChunk:
		return []MessageChunk{v}

	// --- Tool input (streaming) ---

	case ToolInputStartChunk:
		e.activeToolInputs[v.ToolCallID] = &expanderToolInput{
			toolName:         v.ToolName,
			providerExecuted: v.ProviderExecuted,
			dynamic:          new(true),
			title:            v.Title,
		}
		v.Dynamic = new(true)
		return []MessageChunk{v}

	case ToolInputDeltaChunk:
		if active, ok := e.activeToolInputs[v.ToolCallID]; ok {
			active.inputBuf += v.InputTextDelta
		}
		return []MessageChunk{v}

	case ToolInputEndChunk:
		active, ok := e.activeToolInputs[v.ToolCallID]
		if !ok {
			return nil
		}
		delete(e.activeToolInputs, v.ToolCallID)
		return []MessageChunk{ToolInputAvailableChunk{
			ToolCallID:       v.ToolCallID,
			ToolName:         active.toolName,
			Input:            toolInputJSONValue(active.inputBuf),
			ProviderExecuted: active.providerExecuted,
			Dynamic:          new(true),
			Title:            active.title,
		}}

	// --- Tool call (non-streaming → Start + Available) ---

	case ToolCallChunk:
		return []MessageChunk{
			ToolInputStartChunk{
				ToolCallID:       v.ToolCallID,
				ToolName:         v.ToolName,
				ProviderExecuted: v.ProviderExecuted,
				Dynamic:          new(true),
			},
			ToolInputAvailableChunk{
				ToolCallID:       v.ToolCallID,
				ToolName:         v.ToolName,
				Input:            toolInputJSONValue(v.Input),
				ProviderExecuted: v.ProviderExecuted,
				Dynamic:          new(true),
			},
		}

	// --- Tool result (provider-executed → Output Available/Error) ---

	case ToolResultChunk:
		if v.IsError != nil && *v.IsError {
			var errText string
			if err := json.Unmarshal(v.Result, &errText); err != nil {
				errText = string(v.Result)
			}
			return []MessageChunk{ToolOutputErrorChunk{
				ToolCallID: v.ToolCallID,
				ErrorText:  errText,
			}}
		}
		return []MessageChunk{ToolOutputAvailableChunk{
			ToolCallID:  v.ToolCallID,
			Output:      v.Result,
			Preliminary: v.Preliminary,
		}}

	// --- Tool approval (1:1 passthrough) ---

	case ToolApprovalRequestChunk:
		return []MessageChunk{v}

	// --- File (convert base64 data to data-URI) ---

	case FileChunk:
		url := fmt.Sprintf("data:%s;base64,%s", v.MediaType, v.Data)
		return []MessageChunk{FileChunk{
			MediaType:        v.MediaType,
			Data:             url,
			ProviderMetadata: v.ProviderMetadata,
		}}

	// --- Source (1:1 passthrough) ---

	case SourceChunk:
		return []MessageChunk{v}

	// --- Stream lifecycle ---

	case StreamStartChunk:
		if e.suppressStartStep {
			e.suppressStartStep = false
			return nil
		}
		return []MessageChunk{StartStepChunk{}}

	case FinishChunk:
		return nil

	case ErrorChunk:
		return []MessageChunk{ErrorChunk{
			ErrorText: v.ErrorText,
		}}

	case ResponseMetadataChunk, RawChunk:
		// No frontend equivalent.
		return nil

	default:
		return nil
	}
}

// ToolResultToChunks converts a ToolResultPart to MessageChunks for SSE delivery.
func ToolResultToChunks(result ToolResultPart) []MessageChunk {
	if result.Output == nil {
		return nil
	}
	switch o := result.Output.(type) {
	case ErrorTextOutput:
		return []MessageChunk{ToolOutputErrorChunk{
			ToolCallID: result.ToolCallID,
			ErrorText:  o.Value,
		}}
	case ErrorJSONOutput:
		return []MessageChunk{ToolOutputErrorChunk{
			ToolCallID: result.ToolCallID,
			ErrorText:  string(o.Value),
		}}
	case JSONOutput:
		return []MessageChunk{ToolOutputAvailableChunk{
			ToolCallID: result.ToolCallID,
			Output:     o.Value,
		}}
	case TextOutput:
		data, _ := json.Marshal(o.Value)
		return []MessageChunk{ToolOutputAvailableChunk{
			ToolCallID: result.ToolCallID,
			Output:     data,
		}}
	case ExecutionDeniedOutput:
		return []MessageChunk{ToolOutputDeniedChunk{
			ToolCallID: result.ToolCallID,
		}}
	default:
		data, _ := MarshalToolResultOutput(result.Output)
		return []MessageChunk{ToolOutputAvailableChunk{
			ToolCallID: result.ToolCallID,
			Output:     data,
		}}
	}
}
