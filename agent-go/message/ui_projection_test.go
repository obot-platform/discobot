package message

import (
	"encoding/json"
	"testing"
)

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

func TestProjectUIMessages_System(t *testing.T) {
	msgs := []Message{
		{ID: "m1", Role: "system", Parts: []Part{TextPart{Text: "Be helpful."}}},
	}

	result, err := ProjectUIMessages(msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("got %d messages", len(result))
	}

	var ui struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	}
	json.Unmarshal(mustMarshal(t, result[0]), &ui)
	if ui.Role != "system" {
		t.Errorf("Role: got %q", ui.Role)
	}
}

func TestProjectUIMessages_User(t *testing.T) {
	metadata := mustMarshal(t, map[string]string{"originalText": "/commit fix the bug"})
	msgs := []Message{
		{ID: "m1", Role: "user", Metadata: metadata, Parts: []Part{
			TextPart{Text: "hello"},
			FilePart{Data: "data:text/plain;base64,aGk=", MediaType: "text/plain"},
		}},
	}

	result, err := ProjectUIMessages(msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("got %d messages", len(result))
	}
	if string(result[0].Metadata) != string(metadata) {
		t.Fatalf("metadata: got %s want %s", result[0].Metadata, metadata)
	}

	var ui struct {
		Parts []json.RawMessage `json:"parts"`
	}
	json.Unmarshal(mustMarshal(t, result[0]), &ui)
	if len(ui.Parts) != 2 {
		t.Fatalf("parts: got %d", len(ui.Parts))
	}
}

func TestProjectUIMessages_AssistantToolPair(t *testing.T) {
	msgs := []Message{
		{ID: "m1", Role: "assistant", Parts: []Part{
			TextPart{Text: "Let me read that."},
			ToolCallPart{ToolCallID: "tc1", ToolName: "read", Input: `{"path":"foo"}`},
		}},
		{Role: "tool", Parts: []Part{
			ToolResultPart{ToolCallID: "tc1", ToolName: "read", Output: TextOutput{Value: "contents"}},
		}},
	}

	result, err := ProjectUIMessages(msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("got %d messages, want 1 (merged)", len(result))
	}

	var ui struct {
		Role  string            `json:"role"`
		Parts []json.RawMessage `json:"parts"`
	}
	json.Unmarshal(mustMarshal(t, result[0]), &ui)
	if ui.Role != "assistant" {
		t.Errorf("Role: got %q", ui.Role)
	}

	// Parts: text, dynamic-tool (no leading step-start for first step)
	if len(ui.Parts) != 2 {
		t.Fatalf("parts: got %d, want 2", len(ui.Parts))
	}

	var types []string
	for _, p := range ui.Parts {
		var disc struct {
			Type string `json:"type"`
		}
		json.Unmarshal(p, &disc)
		types = append(types, disc.Type)
	}
	if types[0] != "text" {
		t.Errorf("part[0] type: got %q", types[0])
	}
	if types[1] != "dynamic-tool" {
		t.Errorf("part[1] type: got %q", types[1])
	}

	// Check dynamic-tool has output-available state.
	var dp struct {
		State      string          `json:"state"`
		ToolCallID string          `json:"toolCallId"`
		Output     json.RawMessage `json:"output"`
	}
	json.Unmarshal(ui.Parts[1], &dp)
	if dp.State != "output-available" {
		t.Errorf("DynamicToolPart state: got %q", dp.State)
	}
	if dp.ToolCallID != "tc1" {
		t.Errorf("ToolCallID: got %q", dp.ToolCallID)
	}
}

func TestProjectUIMessages_RawToolInput(t *testing.T) {
	patch := "*** Begin Patch\n*** End Patch"
	msgs := []Message{{
		ID:   "m1",
		Role: "assistant",
		Parts: []Part{
			ToolCallPart{ToolCallID: "tc1", ToolName: "apply_patch", Input: patch},
		},
	}}

	result, err := ProjectUIMessages(msgs)
	if err != nil {
		t.Fatal(err)
	}

	var ui struct {
		Parts []json.RawMessage `json:"parts"`
	}
	if err := json.Unmarshal(mustMarshal(t, result[0]), &ui); err != nil {
		t.Fatalf("unmarshal ui message: %v", err)
	}
	if len(ui.Parts) != 1 {
		t.Fatalf("parts: got %d, want 1", len(ui.Parts))
	}

	var dp struct {
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(ui.Parts[0], &dp); err != nil {
		t.Fatalf("unmarshal dynamic tool: %v", err)
	}
	var got struct {
		Raw string `json:"raw"`
	}
	if err := json.Unmarshal(dp.Input, &got); err != nil {
		t.Fatalf("input should be wrapped in an object: %v", err)
	}
	if got.Raw != patch {
		t.Fatalf("Input.raw: got %q, want %q", got.Raw, patch)
	}
}

func TestProjectUIMessages_MultiStep(t *testing.T) {
	// Two assistant+tool steps merged into one UIMessage.
	msgs := []Message{
		{Role: "assistant", Parts: []Part{
			ToolCallPart{ToolCallID: "tc1", ToolName: "read", Input: `{}`},
		}},
		{Role: "tool", Parts: []Part{
			ToolResultPart{ToolCallID: "tc1", ToolName: "read", Output: TextOutput{Value: "a"}},
		}},
		{Role: "assistant", Parts: []Part{
			TextPart{Text: "Done."},
		}},
	}

	result, err := ProjectUIMessages(msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("got %d messages, want 1", len(result))
	}

	var ui struct {
		Parts []json.RawMessage `json:"parts"`
	}
	json.Unmarshal(mustMarshal(t, result[0]), &ui)

	// Two steps: dynamic-tool, step-start + text (no leading step-start for first step)
	if len(ui.Parts) != 3 {
		t.Fatalf("parts: got %d, want 3", len(ui.Parts))
	}

	var types []string
	for _, p := range ui.Parts {
		var disc struct {
			Type string `json:"type"`
		}
		json.Unmarshal(p, &disc)
		types = append(types, disc.Type)
	}
	if types[0] != "dynamic-tool" || types[1] != "step-start" || types[2] != "text" {
		t.Errorf("types: got %v", types)
	}
}

func TestProjectUIMessages_ToolError(t *testing.T) {
	msgs := []Message{
		{Role: "assistant", Parts: []Part{
			ToolCallPart{ToolCallID: "tc1", ToolName: "exec", Input: `{}`},
		}},
		{Role: "tool", Parts: []Part{
			ToolResultPart{ToolCallID: "tc1", ToolName: "exec", Output: ErrorTextOutput{Value: "cmd failed"}},
		}},
	}

	result, err := ProjectUIMessages(msgs)
	if err != nil {
		t.Fatal(err)
	}

	var ui struct {
		Parts []json.RawMessage `json:"parts"`
	}
	json.Unmarshal(mustMarshal(t, result[0]), &ui)

	var dp struct {
		State     string `json:"state"`
		ErrorText string `json:"errorText"`
	}
	json.Unmarshal(ui.Parts[0], &dp) // [0]=dynamic-tool (no leading step-start)
	if dp.State != "output-error" {
		t.Errorf("state: got %q", dp.State)
	}
	if dp.ErrorText != "cmd failed" {
		t.Errorf("errorText: got %q", dp.ErrorText)
	}
}

func TestProjectUIMessages_ProviderExecutedResult(t *testing.T) {
	boolTrue := true
	msgs := []Message{
		{Role: "assistant", Parts: []Part{
			ToolCallPart{
				ToolCallID:       "tc1",
				ToolName:         "code_interpreter",
				Input:            `{}`,
				ProviderExecuted: &boolTrue,
			},
			ToolResultPart{
				ToolCallID: "tc1",
				ToolName:   "code_interpreter",
				Output:     JSONOutput{Value: json.RawMessage(`{"result":42}`)},
			},
		}},
	}

	result, err := ProjectUIMessages(msgs)
	if err != nil {
		t.Fatal(err)
	}

	var ui struct {
		Parts []json.RawMessage `json:"parts"`
	}
	json.Unmarshal(mustMarshal(t, result[0]), &ui)

	// dynamic-tool only (no leading step-start for first step)
	if len(ui.Parts) != 1 {
		t.Fatalf("parts: got %d", len(ui.Parts))
	}

	var dp struct {
		State  string          `json:"state"`
		Output json.RawMessage `json:"output"`
	}
	json.Unmarshal(ui.Parts[0], &dp)
	if dp.State != "output-available" {
		t.Errorf("state: got %q", dp.State)
	}
}

func TestProjectUIMessages_Approval(t *testing.T) {
	boolTrue := true
	msgs := []Message{
		{Role: "assistant", Parts: []Part{
			ToolCallPart{ToolCallID: "tc1", ToolName: "rm", Input: `{}`},
			ToolApprovalRequest{ApprovalID: "a1", ToolCallID: "tc1"},
		}},
		{Role: "tool", Parts: []Part{
			ToolApprovalResponse{ApprovalID: "a1", Approved: true, Reason: "ok"},
			ToolResultPart{ToolCallID: "tc1", ToolName: "rm", Output: TextOutput{Value: "deleted"}},
		}},
	}

	result, err := ProjectUIMessages(msgs)
	if err != nil {
		t.Fatal(err)
	}

	var ui struct {
		Parts []json.RawMessage `json:"parts"`
	}
	json.Unmarshal(mustMarshal(t, result[0]), &ui)

	var dp struct {
		State    string `json:"state"`
		Approval *struct {
			ID       string `json:"id"`
			Approved *bool  `json:"approved"`
		} `json:"approval"`
	}
	json.Unmarshal(ui.Parts[0], &dp) // [0]=dynamic-tool (no leading step-start)

	if dp.Approval == nil {
		t.Fatal("expected approval")
	}
	if dp.Approval.ID != "a1" {
		t.Errorf("approval ID: got %q", dp.Approval.ID)
	}
	if dp.Approval.Approved == nil || !*dp.Approval.Approved {
		t.Error("expected approved=true")
	}
	_ = boolTrue
}

func TestProjectUIMessages_ApprovalAcrossToolMessages(t *testing.T) {
	boolTrue := true
	msgs := []Message{
		{Role: "assistant", Parts: []Part{
			ToolCallPart{ToolCallID: "tc1", ToolName: "rm", Input: `{}`},
		}},
		{Role: "tool", Parts: []Part{
			ToolApprovalRequest{ApprovalID: "a1", ToolCallID: "tc1"},
		}},
		{Role: "tool", Parts: []Part{
			ToolApprovalResponse{ApprovalID: "a1", ToolCallID: "tc1", Approved: true, Reason: "ok"},
		}},
		{Role: "tool", Parts: []Part{
			ToolResultPart{ToolCallID: "tc1", ToolName: "rm", Output: TextOutput{Value: "deleted"}},
		}},
	}

	result, err := ProjectUIMessages(msgs)
	if err != nil {
		t.Fatal(err)
	}

	var ui struct {
		Parts []json.RawMessage `json:"parts"`
	}
	json.Unmarshal(mustMarshal(t, result[0]), &ui)

	var dp struct {
		State    string `json:"state"`
		Approval *struct {
			ID       string `json:"id"`
			Approved *bool  `json:"approved"`
		} `json:"approval"`
	}
	json.Unmarshal(ui.Parts[0], &dp)

	if dp.State != "output-available" {
		t.Fatalf("state: got %q", dp.State)
	}
	if dp.Approval == nil || dp.Approval.ID != "a1" {
		t.Fatalf("approval: got %+v", dp.Approval)
	}
	if dp.Approval.Approved == nil || !*dp.Approval.Approved {
		t.Fatal("expected approved=true")
	}
	_ = boolTrue
}

// --- Projection field-coverage tests ---
//
// Each test below creates a source Part with every projectable field set to a
// distinctive non-zero value, projects it, and checks the corresponding UIPart
// field-by-field. If a new field is added to a Part and its UIPart mapping, add
// it here so that the test catches any missing mapping in the projection code.

func projectAssistantPart(t *testing.T, p Part) UIPart {
	t.Helper()
	parts, err := convertAssistantToolStepToUI(Message{Role: "assistant", Parts: []Part{p}}, nil)
	if err != nil {
		t.Fatalf("convertAssistantToolStepToUI: %v", err)
	}
	if len(parts) != 1 {
		t.Fatalf("expected 1 UIPart, got %d", len(parts))
	}
	return parts[0]
}

func projectUserPart(t *testing.T, p Part) UIPart {
	t.Helper()
	msg := Message{Role: "user", Parts: []Part{p}}
	uiMsg := buildUIUserMessage(msg)
	var ui struct {
		Parts []json.RawMessage `json:"parts"`
	}
	if err := json.Unmarshal(mustMarshal(t, uiMsg), &ui); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(ui.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(ui.Parts))
	}
	got, err := UnmarshalUIPart(ui.Parts[0])
	if err != nil {
		t.Fatalf("UnmarshalUIPart: %v", err)
	}
	return got
}

// MAINTENANCE: update when adding fields to TextPart or UITextPart.
func TestProjectionFieldCoverage_TextPart_Assistant(t *testing.T) {
	src := TextPart{
		Text:             "hello projection",
		ProviderMetadata: json.RawMessage(`{"k":"v"}`),
		// ID and ProviderOptions are intentionally not projected to UI.
	}
	got, ok := projectAssistantPart(t, src).(UITextPart)
	if !ok {
		t.Fatalf("expected UITextPart, got %T", projectAssistantPart(t, src))
	}
	if got.Text != src.Text {
		t.Errorf("Text: got %q, want %q", got.Text, src.Text)
	}
	if string(got.ProviderMetadata) != string(src.ProviderMetadata) {
		t.Errorf("ProviderMetadata: got %s, want %s", got.ProviderMetadata, src.ProviderMetadata)
	}
	if got.State != "done" {
		t.Errorf("State: got %q, want %q", got.State, "done")
	}
}

// MAINTENANCE: update when adding fields to TextPart or UITextPart.
func TestProjectionFieldCoverage_TextPart_User(t *testing.T) {
	src := TextPart{
		Text:             "user hello",
		ProviderMetadata: json.RawMessage(`{"u":"w"}`),
	}
	got, ok := projectUserPart(t, src).(UITextPart)
	if !ok {
		t.Fatalf("expected UITextPart, got %T", projectUserPart(t, src))
	}
	if got.Text != src.Text {
		t.Errorf("Text: got %q, want %q", got.Text, src.Text)
	}
	if string(got.ProviderMetadata) != string(src.ProviderMetadata) {
		t.Errorf("ProviderMetadata: got %s, want %s", got.ProviderMetadata, src.ProviderMetadata)
	}
}

// MAINTENANCE: update when adding fields to ReasoningPart or UIReasoningPart.
func TestProjectionFieldCoverage_ReasoningPart(t *testing.T) {
	src := ReasoningPart{
		Text:             "let me think",
		ProviderMetadata: json.RawMessage(`{"a":"b"}`),
	}
	got, ok := projectAssistantPart(t, src).(UIReasoningPart)
	if !ok {
		t.Fatalf("expected UIReasoningPart, got %T", projectAssistantPart(t, src))
	}
	if got.Text != src.Text {
		t.Errorf("Text: got %q, want %q", got.Text, src.Text)
	}
	if string(got.ProviderMetadata) != string(src.ProviderMetadata) {
		t.Errorf("ProviderMetadata: got %s, want %s", got.ProviderMetadata, src.ProviderMetadata)
	}
	if got.State != "done" {
		t.Errorf("State: got %q, want %q", got.State, "done")
	}
}

// MAINTENANCE: update when adding fields to FilePart or UIFilePart.
func TestProjectionFieldCoverage_FilePart_User(t *testing.T) {
	src := FilePart{
		Data:             "data:image/png;base64,abc123",
		MediaType:        "image/png",
		Filename:         "photo.png",
		ProviderMetadata: json.RawMessage(`{"c":"d"}`),
	}
	got, ok := projectUserPart(t, src).(UIFilePart)
	if !ok {
		t.Fatalf("expected UIFilePart, got %T", projectUserPart(t, src))
	}
	if got.URL != src.Data {
		t.Errorf("URL (from Data): got %q, want %q", got.URL, src.Data)
	}
	if got.MediaType != src.MediaType {
		t.Errorf("MediaType: got %q, want %q", got.MediaType, src.MediaType)
	}
	if got.Filename != src.Filename {
		t.Errorf("Filename: got %q, want %q", got.Filename, src.Filename)
	}
	if string(got.ProviderMetadata) != string(src.ProviderMetadata) {
		t.Errorf("ProviderMetadata: got %s, want %s", got.ProviderMetadata, src.ProviderMetadata)
	}
}

// MAINTENANCE: update when adding fields to FilePart or UIFilePart.
func TestProjectionFieldCoverage_FilePart_Assistant(t *testing.T) {
	src := FilePart{
		Data:             "data:text/plain;base64,aGVsbG8=",
		MediaType:        "text/plain",
		Filename:         "note.txt",
		ProviderMetadata: json.RawMessage(`{"e":"f"}`),
	}
	got, ok := projectAssistantPart(t, src).(UIFilePart)
	if !ok {
		t.Fatalf("expected UIFilePart, got %T", projectAssistantPart(t, src))
	}
	if got.URL != src.Data {
		t.Errorf("URL (from Data): got %q, want %q", got.URL, src.Data)
	}
	if got.MediaType != src.MediaType {
		t.Errorf("MediaType: got %q, want %q", got.MediaType, src.MediaType)
	}
	if got.Filename != src.Filename {
		t.Errorf("Filename: got %q, want %q", got.Filename, src.Filename)
	}
	if string(got.ProviderMetadata) != string(src.ProviderMetadata) {
		t.Errorf("ProviderMetadata: got %s, want %s", got.ProviderMetadata, src.ProviderMetadata)
	}
}

// MAINTENANCE: update when adding fields to SourceURLPart or UISourceURLPart.
func TestProjectionFieldCoverage_SourceURLPart(t *testing.T) {
	src := SourceURLPart{
		SourceID:         "src-1",
		URL:              "https://example.com/page",
		Title:            "Example Page",
		ProviderMetadata: json.RawMessage(`{"g":"h"}`),
	}
	got, ok := projectAssistantPart(t, src).(UISourceURLPart)
	if !ok {
		t.Fatalf("expected UISourceURLPart, got %T", projectAssistantPart(t, src))
	}
	if got.SourceID != src.SourceID {
		t.Errorf("SourceID: got %q, want %q", got.SourceID, src.SourceID)
	}
	if got.URL != src.URL {
		t.Errorf("URL: got %q, want %q", got.URL, src.URL)
	}
	if got.Title != src.Title {
		t.Errorf("Title: got %q, want %q", got.Title, src.Title)
	}
	if string(got.ProviderMetadata) != string(src.ProviderMetadata) {
		t.Errorf("ProviderMetadata: got %s, want %s", got.ProviderMetadata, src.ProviderMetadata)
	}
}

// MAINTENANCE: update when adding fields to SourceDocumentPart or UISourceDocumentPart.
func TestProjectionFieldCoverage_SourceDocumentPart(t *testing.T) {
	src := SourceDocumentPart{
		SourceID:         "doc-1",
		MediaType:        "application/pdf",
		Title:            "Report",
		Filename:         "report.pdf",
		ProviderMetadata: json.RawMessage(`{"i":"j"}`),
	}
	got, ok := projectAssistantPart(t, src).(UISourceDocumentPart)
	if !ok {
		t.Fatalf("expected UISourceDocumentPart, got %T", projectAssistantPart(t, src))
	}
	if got.SourceID != src.SourceID {
		t.Errorf("SourceID: got %q, want %q", got.SourceID, src.SourceID)
	}
	if got.MediaType != src.MediaType {
		t.Errorf("MediaType: got %q, want %q", got.MediaType, src.MediaType)
	}
	if got.Title != src.Title {
		t.Errorf("Title: got %q, want %q", got.Title, src.Title)
	}
	if got.Filename != src.Filename {
		t.Errorf("Filename: got %q, want %q", got.Filename, src.Filename)
	}
	if string(got.ProviderMetadata) != string(src.ProviderMetadata) {
		t.Errorf("ProviderMetadata: got %s, want %s", got.ProviderMetadata, src.ProviderMetadata)
	}
}

// MAINTENANCE: update when adding fields to DataPart or UIDataPart.
func TestProjectionFieldCoverage_DataPart(t *testing.T) {
	src := DataPart{
		DataType: "mode-change",
		ID:       "dp-1",
		Data:     json.RawMessage(`{"mode":"edit"}`),
	}
	got, ok := projectAssistantPart(t, src).(UIDataPart)
	if !ok {
		t.Fatalf("expected UIDataPart, got %T", projectAssistantPart(t, src))
	}
	if got.Type != "data-"+src.DataType {
		t.Errorf("Type: got %q, want %q", got.Type, "data-"+src.DataType)
	}
	if got.ID != src.ID {
		t.Errorf("ID: got %q, want %q", got.ID, src.ID)
	}
	if string(got.Data) != string(src.Data) {
		t.Errorf("Data: got %s, want %s", got.Data, src.Data)
	}
}
