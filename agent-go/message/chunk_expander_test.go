package message

import (
	"encoding/json"
	"testing"
)

func TestExpander_TextPassthrough(t *testing.T) {
	exp := NewChunkExpander(false)
	chunks := exp.Expand(TextStartChunk{ID: "t1"})
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks", len(chunks))
	}
	if _, ok := chunks[0].(TextStartChunk); !ok {
		t.Errorf("type: got %T", chunks[0])
	}

	chunks = exp.Expand(TextDeltaChunk{ID: "t1", Delta: "hi"})
	if _, ok := chunks[0].(TextDeltaChunk); !ok {
		t.Errorf("type: got %T", chunks[0])
	}

	chunks = exp.Expand(TextEndChunk{ID: "t1"})
	if _, ok := chunks[0].(TextEndChunk); !ok {
		t.Errorf("type: got %T", chunks[0])
	}
}

func TestExpander_ToolInputStreamingToAvailable(t *testing.T) {
	exp := NewChunkExpander(false)

	// Start
	chunks := exp.Expand(ToolInputStartChunk{
		ToolCallID: "tc1",
		ToolName:   "read",
		Title:      "Reading",
	})
	if len(chunks) != 1 {
		t.Fatalf("start: got %d chunks", len(chunks))
	}
	if _, ok := chunks[0].(ToolInputStartChunk); !ok {
		t.Errorf("start type: got %T", chunks[0])
	}

	// Deltas
	chunks = exp.Expand(ToolInputDeltaChunk{ToolCallID: "tc1", InputTextDelta: `{"path"`})
	if len(chunks) != 1 {
		t.Fatalf("delta: got %d chunks", len(chunks))
	}

	chunks = exp.Expand(ToolInputDeltaChunk{ToolCallID: "tc1", InputTextDelta: `:"foo"}`})
	if len(chunks) != 1 {
		t.Fatalf("delta2: got %d chunks", len(chunks))
	}

	// End → ToolInputAvailableChunk
	chunks = exp.Expand(ToolInputEndChunk{ToolCallID: "tc1"})
	if len(chunks) != 1 {
		t.Fatalf("end: got %d chunks", len(chunks))
	}
	avail, ok := chunks[0].(ToolInputAvailableChunk)
	if !ok {
		t.Fatalf("end type: got %T", chunks[0])
	}
	if avail.ToolCallID != "tc1" {
		t.Errorf("ToolCallID: got %q", avail.ToolCallID)
	}
	if avail.ToolName != "read" {
		t.Errorf("ToolName: got %q", avail.ToolName)
	}
	if avail.Title != "Reading" {
		t.Errorf("Title: got %q", avail.Title)
	}
	if string(avail.Input) != `{"path":"foo"}` {
		t.Errorf("Input: got %s", avail.Input)
	}
}

func TestExpander_ToolCallNonStreaming(t *testing.T) {
	exp := NewChunkExpander(false)
	chunks := exp.Expand(ToolCallChunk{
		ToolCallID: "tc2",
		ToolName:   "exec",
		Input:      `{"cmd":"ls"}`,
	})
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	if _, ok := chunks[0].(ToolInputStartChunk); !ok {
		t.Errorf("chunks[0] type: got %T", chunks[0])
	}
	avail, ok := chunks[1].(ToolInputAvailableChunk)
	if !ok {
		t.Fatalf("chunks[1] type: got %T", chunks[1])
	}
	if string(avail.Input) != `{"cmd":"ls"}` {
		t.Errorf("Input: got %s", avail.Input)
	}
}

func TestExpander_ToolCallNonStreamingRawInput(t *testing.T) {
	exp := NewChunkExpander(false)
	patch := "*** Begin Patch\n*** End Patch"
	chunks := exp.Expand(ToolCallChunk{
		ToolCallID: "tc2",
		ToolName:   "apply_patch",
		Input:      patch,
	})
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	avail, ok := chunks[1].(ToolInputAvailableChunk)
	if !ok {
		t.Fatalf("chunks[1] type: got %T", chunks[1])
	}
	var got struct {
		Raw string `json:"raw"`
	}
	if err := json.Unmarshal(avail.Input, &got); err != nil {
		t.Fatalf("input should be wrapped in an object: %v", err)
	}
	if got.Raw != patch {
		t.Fatalf("Input.raw: got %q, want %q", got.Raw, patch)
	}
}

func TestExpander_ToolResultSuccess(t *testing.T) {
	exp := NewChunkExpander(false)
	chunks := exp.Expand(ToolResultChunk{
		ToolCallID: "tc1",
		ToolName:   "run",
		Result:     json.RawMessage(`{"ok":true}`),
	})
	if len(chunks) != 1 {
		t.Fatalf("got %d", len(chunks))
	}
	oa, ok := chunks[0].(ToolOutputAvailableChunk)
	if !ok {
		t.Fatalf("type: got %T", chunks[0])
	}
	if string(oa.Output) != `{"ok":true}` {
		t.Errorf("Output: got %s", oa.Output)
	}
}

func TestExpander_ToolResultError(t *testing.T) {
	isErr := true
	exp := NewChunkExpander(false)
	chunks := exp.Expand(ToolResultChunk{
		ToolCallID: "tc1",
		ToolName:   "run",
		Result:     json.RawMessage(`"failed"`),
		IsError:    &isErr,
	})
	if len(chunks) != 1 {
		t.Fatalf("got %d", len(chunks))
	}
	oe, ok := chunks[0].(ToolOutputErrorChunk)
	if !ok {
		t.Fatalf("type: got %T", chunks[0])
	}
	if oe.ErrorText != "failed" {
		t.Errorf("ErrorText: got %q", oe.ErrorText)
	}
}

func TestExpander_StreamStartToStartStep(t *testing.T) {
	// Non-first step: StreamStartChunk → StartStepChunk.
	exp := NewChunkExpander(false)
	chunks := exp.Expand(StreamStartChunk{})
	if len(chunks) != 1 {
		t.Fatalf("got %d", len(chunks))
	}
	if _, ok := chunks[0].(StartStepChunk); !ok {
		t.Errorf("type: got %T", chunks[0])
	}

	// First step (isFirstStep=true): StreamStartChunk is suppressed.
	expFirst := NewChunkExpander(true)
	if got := expFirst.Expand(StreamStartChunk{}); len(got) != 0 {
		t.Errorf("first step start: expected no chunks, got %d", len(got))
	}
}

func TestExpander_FinishDropped(t *testing.T) {
	exp := NewChunkExpander(false)
	if chunks := exp.Expand(FinishChunk{
		FinishReason: FinishReason{Unified: "stop"},
	}); len(chunks) != 0 {
		t.Fatalf("expected finish chunk to be dropped, got %d chunks", len(chunks))
	}
}

func TestExpander_MetadataDropped(t *testing.T) {
	exp := NewChunkExpander(false)
	if chunks := exp.Expand(ResponseMetadataChunk{ID: "r1"}); chunks != nil {
		t.Errorf("ResponseMetadata should be dropped, got %d chunks", len(chunks))
	}
	if chunks := exp.Expand(RawChunk{RawValue: json.RawMessage(`{}`)}); chunks != nil {
		t.Errorf("Raw should be dropped, got %d chunks", len(chunks))
	}
}

func TestExpander_FileConvertsToDataURI(t *testing.T) {
	exp := NewChunkExpander(false)
	chunks := exp.Expand(FileChunk{
		MediaType: "image/png",
		Data:      "abc123",
	})
	if len(chunks) != 1 {
		t.Fatalf("got %d", len(chunks))
	}
	fc := chunks[0].(FileChunk)
	if fc.Data != "data:image/png;base64,abc123" {
		t.Errorf("Data: got %q", fc.Data)
	}
}

func TestToolResultToChunks_Text(t *testing.T) {
	chunks := ToolResultToChunks(ToolResultPart{
		ToolCallID: "tc1",
		ToolName:   "read",
		Output:     TextOutput{Value: "file contents"},
	})
	if len(chunks) != 1 {
		t.Fatalf("got %d", len(chunks))
	}
	oa, ok := chunks[0].(ToolOutputAvailableChunk)
	if !ok {
		t.Fatalf("type: got %T", chunks[0])
	}
	if oa.ToolCallID != "tc1" {
		t.Errorf("ToolCallID: got %q", oa.ToolCallID)
	}
}

func TestToolResultToChunks_ErrorText(t *testing.T) {
	chunks := ToolResultToChunks(ToolResultPart{
		ToolCallID: "tc1",
		ToolName:   "exec",
		Output:     ErrorTextOutput{Value: "command failed"},
	})
	oe := chunks[0].(ToolOutputErrorChunk)
	if oe.ErrorText != "command failed" {
		t.Errorf("ErrorText: got %q", oe.ErrorText)
	}
}

func TestToolResultToChunks_Denied(t *testing.T) {
	chunks := ToolResultToChunks(ToolResultPart{
		ToolCallID: "tc1",
		Output:     ExecutionDeniedOutput{Reason: "unsafe"},
	})
	if _, ok := chunks[0].(ToolOutputDeniedChunk); !ok {
		t.Errorf("type: got %T", chunks[0])
	}
}

func TestToolResultToChunks_NilOutput(t *testing.T) {
	chunks := ToolResultToChunks(ToolResultPart{ToolCallID: "tc1"})
	if chunks != nil {
		t.Errorf("expected nil, got %d chunks", len(chunks))
	}
}
