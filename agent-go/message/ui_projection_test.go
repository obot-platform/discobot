package message

import (
	"encoding/json"
	"testing"
)

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
	json.Unmarshal(result[0], &ui)
	if ui.Role != "system" {
		t.Errorf("Role: got %q", ui.Role)
	}
}

func TestProjectUIMessages_User(t *testing.T) {
	msgs := []Message{
		{ID: "m1", Role: "user", Parts: []Part{
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

	var ui struct {
		Parts []json.RawMessage `json:"parts"`
	}
	json.Unmarshal(result[0], &ui)
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
	json.Unmarshal(result[0], &ui)
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
	if err := json.Unmarshal(result[0], &ui); err != nil {
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
	json.Unmarshal(result[0], &ui)

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
	json.Unmarshal(result[0], &ui)

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
	json.Unmarshal(result[0], &ui)

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
	json.Unmarshal(result[0], &ui)

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
