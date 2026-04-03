package message

import (
	"encoding/json"
	"testing"
	"time"
)

func chunkRoundTrip(t *testing.T, name string, c MessageChunk) {
	t.Helper()
	data, err := MarshalChunk(c)
	if err != nil {
		t.Fatalf("%s: marshal error: %v", name, err)
	}
	got, err := UnmarshalChunk(data)
	if err != nil {
		t.Fatalf("%s: unmarshal error: %v", name, err)
	}
	data2, err := MarshalChunk(got)
	if err != nil {
		t.Fatalf("%s: re-marshal error: %v", name, err)
	}
	if string(data) != string(data2) {
		t.Errorf("%s: round-trip mismatch\n  got:  %s\n  want: %s", name, data2, data)
	}
}

func TestChunkRoundTrip_TextStreaming(t *testing.T) {
	chunkRoundTrip(t, "TextStart", TextStartChunk{ID: "t1"})
	chunkRoundTrip(t, "TextDelta", TextDeltaChunk{ID: "t1", Delta: "hello"})
	chunkRoundTrip(t, "TextEnd", TextEndChunk{ID: "t1"})
}

func TestChunkRoundTrip_ReasoningStreaming(t *testing.T) {
	chunkRoundTrip(t, "ReasoningStart", ReasoningStartChunk{ID: "r1"})
	chunkRoundTrip(t, "ReasoningDelta", ReasoningDeltaChunk{ID: "r1", Delta: "thinking"})
	chunkRoundTrip(t, "ReasoningEnd", ReasoningEndChunk{ID: "r1"})
}

func TestChunkRoundTrip_ToolInputStreaming(t *testing.T) {
	chunkRoundTrip(t, "ToolInputStart", ToolInputStartChunk{
		ToolCallID: "tc1",
		ToolName:   "read",
		Title:      "Reading file",
	})
	chunkRoundTrip(t, "ToolInputDelta", ToolInputDeltaChunk{
		ToolCallID:     "tc1",
		InputTextDelta: `{"path":`,
	})
	chunkRoundTrip(t, "ToolInputEnd", ToolInputEndChunk{ToolCallID: "tc1"})
}

func TestChunkRoundTrip_ToolCall(t *testing.T) {
	chunkRoundTrip(t, "ToolCall", ToolCallChunk{
		ToolCallID: "tc2",
		ToolName:   "exec",
		Input:      `{"cmd":"ls"}`,
	})
}

func TestChunkRoundTrip_ToolResult(t *testing.T) {
	boolTrue := true
	chunkRoundTrip(t, "ToolResult", ToolResultChunk{
		ToolCallID: "tc1",
		ToolName:   "run",
		Result:     json.RawMessage(`"ok"`),
		IsError:    &boolTrue,
	})
}

func TestChunkRoundTrip_ToolApproval(t *testing.T) {
	chunkRoundTrip(t, "ToolApprovalRequest", ToolApprovalRequestChunk{
		ApprovalID: "a1",
		ToolCallID: "tc1",
	})
	chunkRoundTrip(t, "ToolApprovalResponse", ToolApprovalResponseChunk{
		ApprovalID: "a1",
		ToolCallID: "tc1",
		Approved:   true,
		Reason:     "ok",
	})
}

func TestChunkRoundTrip_File(t *testing.T) {
	chunkRoundTrip(t, "File", FileChunk{
		MediaType: "image/png",
		Data:      "base64data",
	})
}

func TestChunkRoundTrip_Source(t *testing.T) {
	chunkRoundTrip(t, "Source", SourceChunk{
		SourceType: "url",
		SourceID:   "s1",
		URL:        "https://example.com",
		Title:      "Example",
	})
}

func TestChunkRoundTrip_StreamLifecycle(t *testing.T) {
	chunkRoundTrip(t, "StreamStart", StreamStartChunk{
		Warnings: []Warning{{Type: "test", Message: "warn"}},
	})

	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	chunkRoundTrip(t, "ResponseMetadata", ResponseMetadataChunk{
		ID:        "resp1",
		Timestamp: &ts,
		ModelID:   "claude-3",
	})

	chunkRoundTrip(t, "Raw", RawChunk{RawValue: json.RawMessage(`{"raw":true}`)})
	chunkRoundTrip(t, "Error", ErrorChunk{ErrorText: "stream failed"})
}

func TestChunkRoundTrip_OrchestratorTool(t *testing.T) {
	chunkRoundTrip(t, "ToolInputAvailable", ToolInputAvailableChunk{
		ToolCallID: "tc1",
		ToolName:   "read",
		Input:      json.RawMessage(`{"path":"foo"}`),
	})
	chunkRoundTrip(t, "ToolInputError", ToolInputErrorChunk{
		ToolCallID: "tc1",
		ToolName:   "read",
		Input:      json.RawMessage(`{"partial":true}`),
		ErrorText:  "invalid JSON",
	})
	chunkRoundTrip(t, "ToolOutputAvailable", ToolOutputAvailableChunk{
		ToolCallID: "tc1",
		Output:     json.RawMessage(`"result"`),
	})
	chunkRoundTrip(t, "ToolOutputError", ToolOutputErrorChunk{
		ToolCallID: "tc1",
		ErrorText:  "execution failed",
	})
	chunkRoundTrip(t, "ToolOutputDenied", ToolOutputDeniedChunk{
		ToolCallID: "tc1",
	})
}

func TestChunkRoundTrip_OrchestratorLifecycle(t *testing.T) {
	chunkRoundTrip(t, "StartStep", StartStepChunk{})
	chunkRoundTrip(t, "FinishStep", FinishStepChunk{})
	chunkRoundTrip(t, "Start", StartChunk{MessageID: "m1"})
	chunkRoundTrip(t, "Abort", AbortChunk{Reason: "cancelled"})
	chunkRoundTrip(t, "MessageMetadata", MessageMetadataChunk{
		MessageMetadata: json.RawMessage(`{"key":"val"}`),
	})
}

func TestChunkRoundTrip_Data(t *testing.T) {
	chunkRoundTrip(t, "DataChunk", DataChunk{
		DataType: "custom",
		ID:       "d1",
		Data:     json.RawMessage(`{"value":42}`),
	})
	chunkRoundTrip(t, "ThreadUpdate", ThreadUpdateChunk{
		Data: ThreadUpdateData{Thread: ThreadUpdateInfo{
			ID:           "thread-1",
			Name:         "Debug build failure",
			LastMessage:  "Investigate CI",
			ErrorMessage: "invalid model",
			Model:        "anthropic/claude-sonnet-4-6",
			Reasoning:    "enabled",
			Mode:         "plan",
		}},
	})
	chunkRoundTrip(t, "ThreadResume", ThreadResumeChunk{
		Data: ThreadResumeData{ThreadID: "thread-1", MessageID: "assistant-1"},
	})
	chunkRoundTrip(t, "ToolApprovalResponseData", ToolApprovalResponseDataChunk{
		Data: ToolApprovalResponseData{ApprovalID: "a1", ToolCallID: "tc1", Approved: true, Reason: "ok"},
	})
	chunkRoundTrip(t, "UserMessage", UserMessageChunk{
		Data: UserMessageData{
			Message: UIMessage{
				ID:   "u1",
				Role: "user",
				Parts: []UIPart{
					UITextPart{Type: "text", Text: "hello", State: "done"},
				},
			},
			InsertBeforeMessageID: "a1",
		},
	})
}

func TestChunkRoundTrip_FinishProvider(t *testing.T) {
	// Provider FinishChunk with usage data.
	fc := FinishChunk{
		FinishReason: FinishReason{Unified: "stop"},
		Usage: Usage{
			InputTokens:  InputTokens{Total: 100},
			OutputTokens: OutputTokens{Total: 50},
		},
	}
	data, err := MarshalChunk(fc)
	if err != nil {
		t.Fatal(err)
	}

	got, err := UnmarshalChunk(data)
	if err != nil {
		t.Fatal(err)
	}

	gotFC, ok := got.(FinishChunk)
	if !ok {
		t.Fatalf("expected FinishChunk, got %T", got)
	}
	if gotFC.FinishReason.Unified != "stop" {
		t.Errorf("FinishReason.Unified: got %q", gotFC.FinishReason.Unified)
	}
	if gotFC.Usage.InputTokens.Total != 100 {
		t.Errorf("Usage.InputTokens.Total: got %d", gotFC.Usage.InputTokens.Total)
	}
}

func TestChunkRoundTrip_ResponseFinish(t *testing.T) {
	// Orchestrator ResponseFinishChunk — no usage, string finishReason.
	rfc := ResponseFinishChunk{FinishReason: "stop", MessageMetadata: json.RawMessage(`{"finishedAt":"2025-01-02T00:00:00Z"}`)}
	data, err := MarshalChunk(rfc)
	if err != nil {
		t.Fatal(err)
	}

	got, err := UnmarshalChunk(data)
	if err != nil {
		t.Fatal(err)
	}

	gotRFC, ok := got.(ResponseFinishChunk)
	if !ok {
		t.Fatalf("expected ResponseFinishChunk, got %T", got)
	}
	if gotRFC.FinishReason != "stop" {
		t.Errorf("FinishReason: got %q", gotRFC.FinishReason)
	}
	if string(gotRFC.MessageMetadata) != `{"finishedAt":"2025-01-02T00:00:00Z"}` {
		t.Fatalf("MessageMetadata: got %s", gotRFC.MessageMetadata)
	}
}

func TestProviderChunkInterface(t *testing.T) {
	// Verify provider chunks satisfy ProviderMessageChunk.
	var providerChunks []ProviderMessageChunk
	providerChunks = append(providerChunks,
		TextStartChunk{},
		TextDeltaChunk{},
		TextEndChunk{},
		ReasoningStartChunk{},
		ReasoningDeltaChunk{},
		ReasoningEndChunk{},
		ToolInputStartChunk{},
		ToolInputDeltaChunk{},
		ToolInputEndChunk{},
		ToolCallChunk{},
		ToolResultChunk{},
		ToolApprovalRequestChunk{},
		FileChunk{},
		SourceChunk{},
		StreamStartChunk{},
		ResponseMetadataChunk{},
		FinishChunk{},
		RawChunk{},
		ErrorChunk{},
		DataChunk{},
	)

	// Verify they're all also MessageChunks.
	for _, pc := range providerChunks {
		var _ MessageChunk = pc
	}

	if len(providerChunks) != 20 {
		t.Errorf("expected 20 provider chunk types, got %d", len(providerChunks))
	}
}

func TestOrchestratorChunksNotProvider(t *testing.T) {
	// These should NOT satisfy ProviderMessageChunk.
	// We verify by checking UnmarshalProviderChunk rejects them.
	orchestratorChunks := []MessageChunk{
		ToolInputAvailableChunk{ToolCallID: "tc1", ToolName: "x", Input: json.RawMessage(`{}`)},
		ToolOutputAvailableChunk{ToolCallID: "tc1", Output: json.RawMessage(`{}`)},
		StartStepChunk{},
		FinishStepChunk{},
		StartChunk{},
		AbortChunk{},
	}

	for _, c := range orchestratorChunks {
		data, err := MarshalChunk(c)
		if err != nil {
			t.Fatalf("marshal %T: %v", c, err)
		}
		_, err = UnmarshalProviderChunk(data)
		if err == nil {
			t.Errorf("expected UnmarshalProviderChunk to reject %T", c)
		}
	}
}

func TestUnmarshalChunkUnknownType(t *testing.T) {
	_, err := UnmarshalChunk([]byte(`{"type":"totally-unknown"}`))
	if err == nil {
		t.Error("expected error for unknown type")
	}
}
