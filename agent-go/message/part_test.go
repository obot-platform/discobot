package message

import (
	"encoding/json"
	"testing"
)

func partRoundTrip(t *testing.T, name string, p Part) {
	t.Helper()
	data, err := MarshalPart(p)
	if err != nil {
		t.Fatalf("%s: marshal error: %v", name, err)
	}
	got, err := UnmarshalPart(data)
	if err != nil {
		t.Fatalf("%s: unmarshal error: %v", name, err)
	}
	data2, err := MarshalPart(got)
	if err != nil {
		t.Fatalf("%s: re-marshal error: %v", name, err)
	}
	if string(data) != string(data2) {
		t.Errorf("%s: round-trip mismatch\n  got:  %s\n  want: %s", name, data2, data)
	}
}

func TestPartRoundTrip_Text(t *testing.T) {
	partRoundTrip(t, "TextPart", TextPart{
		Text:             "hello",
		State:            "done",
		ProviderMetadata: json.RawMessage(`{"k":"v"}`),
		ProviderOptions:  json.RawMessage(`{"o":"p"}`),
	})
}

func TestPartRoundTrip_Reasoning(t *testing.T) {
	partRoundTrip(t, "ReasoningPart", ReasoningPart{
		Text:  "thinking...",
		State: "streaming",
	})
}

func TestPartRoundTrip_Image(t *testing.T) {
	partRoundTrip(t, "ImagePart", ImagePart{
		Image:     "base64data",
		MediaType: "image/png",
	})
}

func TestPartRoundTrip_File(t *testing.T) {
	partRoundTrip(t, "FilePart", FilePart{
		Data:      "data:image/png;base64,abc",
		MediaType: "image/png",
		Filename:  "test.png",
	})
}

// TestPartUnmarshal_ToolCallLegacy verifies backward compatibility with step
// result files written before ToolCallPart.Input was changed from
// json.RawMessage to string. The old code embedded ToolCallPart directly in
// the MarshalPart anonymous struct, so the JSON stored on disk looks exactly
// like the bytes below. The new UnmarshalPart must read the "input" field
// (a JSON object) and surface it as a plain Go string.
func TestPartUnmarshal_ToolCallLegacy(t *testing.T) {
	// Simulate a persisted tool-call part written by the old json.RawMessage code.
	legacy := []byte(`{"type":"tool-call","toolCallId":"tc1","toolName":"edit","input":{"path":"/foo.go","old_string":"a","new_string":"b"}}`)

	part, err := UnmarshalPart(legacy)
	if err != nil {
		t.Fatalf("unmarshal legacy tool-call: %v", err)
	}
	tc, ok := part.(ToolCallPart)
	if !ok {
		t.Fatalf("got %T, want ToolCallPart", part)
	}
	if tc.ToolCallID != "tc1" {
		t.Errorf("ToolCallID: got %q, want %q", tc.ToolCallID, "tc1")
	}
	// Input must be the JSON object as a string, ready for json.Unmarshal.
	wantInput := `{"path":"/foo.go","old_string":"a","new_string":"b"}`
	if tc.Input != wantInput {
		t.Errorf("Input: got %q, want %q", tc.Input, wantInput)
	}
	// Confirm the string parses correctly — matching what tool execution does.
	var dst struct {
		Path      string `json:"path"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	if err := json.Unmarshal([]byte(tc.Input), &dst); err != nil {
		t.Fatalf("unmarshal Input at execution time: %v", err)
	}
	if dst.Path != "/foo.go" || dst.OldString != "a" || dst.NewString != "b" {
		t.Errorf("parsed fields: got %+v", dst)
	}
}

func TestPartRoundTrip_ToolCall(t *testing.T) {
	boolTrue := true
	partRoundTrip(t, "ToolCallPart", ToolCallPart{
		ToolCallID:       "tc1",
		ToolName:         "read",
		Input:            `{"path":"foo"}`,
		ProviderExecuted: &boolTrue,
	})
}

func TestPartRoundTrip_ToolCallRawInput(t *testing.T) {
	patch := "*** Begin Patch\n*** End Patch"

	data, err := MarshalPart(ToolCallPart{
		ToolCallID: "tc1",
		ToolName:   "apply_patch",
		Input:      patch,
	})
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var raw struct {
		Input     json.RawMessage `json:"input"`
		InputText string          `json:"inputText"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw data: %v", err)
	}
	if raw.InputText != patch {
		t.Fatalf("InputText: got %q, want %q", raw.InputText, patch)
	}
	if len(raw.Input) != 0 {
		t.Fatalf("expected raw JSON input to be omitted, got %s", raw.Input)
	}

	part, err := UnmarshalPart(data)
	if err != nil {
		t.Fatalf("unmarshal part: %v", err)
	}
	toolCall, ok := part.(ToolCallPart)
	if !ok {
		t.Fatalf("got %T, want ToolCallPart", part)
	}
	if toolCall.Input != patch {
		t.Fatalf("Input: got %q, want %q", toolCall.Input, patch)
	}
}

func TestPartRoundTrip_ToolResult(t *testing.T) {
	partRoundTrip(t, "ToolResultPart/text", ToolResultPart{
		ToolCallID: "tc1",
		ToolName:   "read",
		Output:     TextOutput{Value: "file contents"},
	})
	partRoundTrip(t, "ToolResultPart/json", ToolResultPart{
		ToolCallID: "tc2",
		ToolName:   "search",
		Output:     JSONOutput{Value: json.RawMessage(`[1,2,3]`)},
	})
	partRoundTrip(t, "ToolResultPart/error-text", ToolResultPart{
		ToolCallID: "tc3",
		ToolName:   "exec",
		Output:     ErrorTextOutput{Value: "command failed"},
	})
	partRoundTrip(t, "ToolResultPart/denied", ToolResultPart{
		ToolCallID: "tc4",
		ToolName:   "rm",
		Output:     ExecutionDeniedOutput{Reason: "too dangerous"},
	})
}

func TestPartRoundTrip_ToolApproval(t *testing.T) {
	partRoundTrip(t, "ToolApprovalRequest", ToolApprovalRequest{
		ApprovalID: "a1",
		ToolCallID: "tc1",
	})
	partRoundTrip(t, "ToolApprovalResponse", ToolApprovalResponse{
		ApprovalID: "a1",
		Approved:   true,
		Reason:     "looks safe",
	})
}

func TestPartRoundTrip_Source(t *testing.T) {
	partRoundTrip(t, "SourceURLPart", SourceURLPart{
		SourceID: "s1",
		URL:      "https://example.com",
		Title:    "Example",
	})
	partRoundTrip(t, "SourceDocumentPart", SourceDocumentPart{
		SourceID:  "s2",
		MediaType: "application/pdf",
		Title:     "Doc",
		Filename:  "test.pdf",
	})
}

func TestPartRoundTrip_StepStart(t *testing.T) {
	partRoundTrip(t, "StepStartPart", StepStartPart{})
}

func TestPartRoundTrip_Data(t *testing.T) {
	partRoundTrip(t, "DataPart", DataPart{
		DataType: "custom",
		ID:       "d1",
		Data:     json.RawMessage(`{"value":42}`),
	})
}

func TestPartTypeField(t *testing.T) {
	data, err := MarshalPart(TextPart{Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)
	if raw["type"] != "text" {
		t.Errorf("expected type=text, got %v", raw["type"])
	}
}

func TestUnmarshalPartUnknownType(t *testing.T) {
	_, err := UnmarshalPart([]byte(`{"type":"unknown-type"}`))
	if err == nil {
		t.Error("expected error for unknown type")
	}
}
