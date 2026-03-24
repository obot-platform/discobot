package message

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAccumulator_TextStreaming(t *testing.T) {
	acc := NewChunkAccumulator()
	acc.Push(TextStartChunk{ID: "t1"})
	acc.Push(TextDeltaChunk{ID: "t1", Delta: "hello "})
	acc.Push(TextDeltaChunk{ID: "t1", Delta: "world"})
	acc.Push(TextEndChunk{ID: "t1"})
	acc.Close()

	msg := acc.Message()
	if msg.Role != "assistant" {
		t.Errorf("Role: got %q", msg.Role)
	}
	if len(msg.Parts) != 1 {
		t.Fatalf("Parts: got %d, want 1", len(msg.Parts))
	}
	tp, ok := msg.Parts[0].(TextPart)
	if !ok {
		t.Fatalf("Parts[0] type: got %T", msg.Parts[0])
	}
	if tp.Text != "hello world" {
		t.Errorf("Text: got %q", tp.Text)
	}
}

func TestAccumulator_ReasoningStreaming(t *testing.T) {
	acc := NewChunkAccumulator()
	acc.Push(ReasoningStartChunk{ID: "r1"})
	acc.Push(ReasoningDeltaChunk{ID: "r1", Delta: "let me think"})
	acc.Push(ReasoningEndChunk{ID: "r1"})
	acc.Close()

	msg := acc.Message()
	if len(msg.Parts) != 1 {
		t.Fatalf("Parts: got %d, want 1", len(msg.Parts))
	}
	rp, ok := msg.Parts[0].(ReasoningPart)
	if !ok {
		t.Fatalf("type: got %T", msg.Parts[0])
	}
	if rp.Text != "let me think" {
		t.Errorf("Text: got %q", rp.Text)
	}
}

func TestAccumulator_ToolInputStreaming(t *testing.T) {
	acc := NewChunkAccumulator()
	acc.Push(ToolInputStartChunk{ToolCallID: "tc1", ToolName: "read"})
	acc.Push(ToolInputDeltaChunk{ToolCallID: "tc1", InputTextDelta: `{"path"`})
	acc.Push(ToolInputDeltaChunk{ToolCallID: "tc1", InputTextDelta: `:"foo"}`})
	acc.Push(ToolInputEndChunk{ToolCallID: "tc1"})
	acc.Close()

	msg := acc.Message()
	if len(msg.Parts) != 1 {
		t.Fatalf("Parts: got %d", len(msg.Parts))
	}
	tc, ok := msg.Parts[0].(ToolCallPart)
	if !ok {
		t.Fatalf("type: got %T", msg.Parts[0])
	}
	if tc.ToolCallID != "tc1" {
		t.Errorf("ToolCallID: got %q", tc.ToolCallID)
	}
	if string(tc.Input) != `{"path":"foo"}` {
		t.Errorf("Input: got %s", tc.Input)
	}
}

func TestAccumulator_ToolCallNonStreaming(t *testing.T) {
	acc := NewChunkAccumulator()
	acc.Push(ToolCallChunk{
		ToolCallID: "tc2",
		ToolName:   "exec",
		Input:      `{"cmd":"ls"}`,
	})
	acc.Close()

	msg := acc.Message()
	if len(msg.Parts) != 1 {
		t.Fatalf("Parts: got %d", len(msg.Parts))
	}
	tc := msg.Parts[0].(ToolCallPart)
	if string(tc.Input) != `{"cmd":"ls"}` {
		t.Errorf("Input: got %s", tc.Input)
	}
}

func TestAccumulator_ToolResult(t *testing.T) {
	acc := NewChunkAccumulator()
	acc.Push(ToolResultChunk{
		ToolCallID: "tc1",
		ToolName:   "run",
		Result:     json.RawMessage(`{"ok":true}`),
	})
	acc.Close()

	msg := acc.Message()
	tr := msg.Parts[0].(ToolResultPart)
	if _, ok := tr.Output.(JSONOutput); !ok {
		t.Errorf("Output type: got %T", tr.Output)
	}
}

func TestAccumulator_ToolResultError(t *testing.T) {
	isErr := true
	acc := NewChunkAccumulator()
	acc.Push(ToolResultChunk{
		ToolCallID: "tc1",
		ToolName:   "run",
		Result:     json.RawMessage(`"failed"`),
		IsError:    &isErr,
	})
	acc.Close()

	msg := acc.Message()
	tr := msg.Parts[0].(ToolResultPart)
	if _, ok := tr.Output.(ErrorJSONOutput); !ok {
		t.Errorf("Output type: got %T", tr.Output)
	}
}

func TestAccumulator_FinishAndMetadata(t *testing.T) {
	acc := NewChunkAccumulator()
	acc.Push(StreamStartChunk{Warnings: []Warning{{Type: "w", Message: "warn"}}})
	ts := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	acc.Push(ResponseMetadataChunk{ID: "r1", Timestamp: &ts, ModelID: "claude"})
	acc.Push(TextStartChunk{ID: "t1"})
	acc.Push(TextDeltaChunk{ID: "t1", Delta: "hi"})
	acc.Push(TextEndChunk{ID: "t1"})
	acc.Push(FinishChunk{
		FinishReason: FinishReason{Unified: "stop"},
		Usage:        Usage{InputTokens: InputTokens{Total: 10}},
	})
	acc.Close()

	if len(acc.Warnings()) != 1 {
		t.Errorf("Warnings: got %d", len(acc.Warnings()))
	}
	if acc.ResponseMeta() == nil || acc.ResponseMeta().ModelID != "claude" {
		t.Error("ResponseMeta missing or wrong")
	}
	if acc.FinishResult() == nil || acc.FinishResult().FinishReason.Unified != "stop" {
		t.Error("FinishResult missing or wrong")
	}
	msg := acc.Message()
	if msg.CreatedAt == nil || !msg.CreatedAt.Equal(ts) {
		t.Fatalf("CreatedAt: got %v, want %v", msg.CreatedAt, ts)
	}
}

func TestAccumulator_CloseWithPartialToolInput(t *testing.T) {
	acc := NewChunkAccumulator()
	acc.Push(ToolInputStartChunk{ToolCallID: "tc1", ToolName: "read"})
	acc.Push(ToolInputDeltaChunk{ToolCallID: "tc1", InputTextDelta: `{"path`})
	// No ToolInputEnd — simulates interrupted stream.
	acc.Close()

	msg := acc.Message()
	if len(msg.Parts) != 1 {
		t.Fatalf("Parts: got %d, want 1", len(msg.Parts))
	}
	tc := msg.Parts[0].(ToolCallPart)
	// Input is preserved as-is; JSON validity is enforced at tool-execution time.
	if tc.Input != `{"path` {
		t.Errorf("Input: got %q, want %q", tc.Input, `{"path`)
	}
}

func TestAccumulator_MultipleBlocks(t *testing.T) {
	acc := NewChunkAccumulator()
	acc.Push(ReasoningStartChunk{ID: "r1"})
	acc.Push(ReasoningDeltaChunk{ID: "r1", Delta: "hmm"})
	acc.Push(ReasoningEndChunk{ID: "r1"})
	acc.Push(TextStartChunk{ID: "t1"})
	acc.Push(TextDeltaChunk{ID: "t1", Delta: "answer"})
	acc.Push(TextEndChunk{ID: "t1"})
	acc.Push(ToolCallChunk{ToolCallID: "tc1", ToolName: "exec", Input: `{}`})
	acc.Close()

	msg := acc.Message()
	if len(msg.Parts) != 3 {
		t.Fatalf("Parts: got %d, want 3", len(msg.Parts))
	}
	if _, ok := msg.Parts[0].(ReasoningPart); !ok {
		t.Errorf("Parts[0]: got %T", msg.Parts[0])
	}
	if _, ok := msg.Parts[1].(TextPart); !ok {
		t.Errorf("Parts[1]: got %T", msg.Parts[1])
	}
	if _, ok := msg.Parts[2].(ToolCallPart); !ok {
		t.Errorf("Parts[2]: got %T", msg.Parts[2])
	}
}

func TestAccumulator_ReasoningProviderMetadata(t *testing.T) {
	// The nested format wraps the provider block under "openai".
	meta := json.RawMessage(`{"openai":{"id":"rs_1","type":"reasoning","encrypted_content":"gAAAA_enc","summary":[{"type":"summary_text","text":"thinking..."}]}}`)
	acc := NewChunkAccumulator()
	acc.Push(ReasoningStartChunk{ID: "rs_1"})
	acc.Push(ReasoningDeltaChunk{ID: "rs_1", Delta: "thinking..."})
	acc.Push(ReasoningEndChunk{ID: "rs_1", ProviderMetadata: meta})
	acc.Close()

	msg := acc.Message()
	if len(msg.Parts) != 1 {
		t.Fatalf("Parts: got %d, want 1", len(msg.Parts))
	}
	rp, ok := msg.Parts[0].(ReasoningPart)
	if !ok {
		t.Fatalf("type: got %T", msg.Parts[0])
	}
	if rp.Text != "thinking..." {
		t.Errorf("Text: got %q", rp.Text)
	}
	if len(rp.ProviderMetadata) == 0 {
		t.Fatal("expected ProviderMetadata to be set")
	}
	// ProviderMetadata should be in the nested format: {"openai": {...}}
	var nested map[string]any
	json.Unmarshal(rp.ProviderMetadata, &nested)
	if _, ok := nested["openai"]; !ok {
		t.Errorf("expected 'openai' key in ProviderMetadata, got keys: %v", func() []string {
			keys := make([]string, 0, len(nested))
			for k := range nested {
				keys = append(keys, k)
			}
			return keys
		}())
	}
	// MetadataType should extract "reasoning" from the nested block.
	if rp.MetadataType() != "reasoning" {
		t.Errorf("MetadataType: got %q, want %q", rp.MetadataType(), "reasoning")
	}
}
