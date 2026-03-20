package message

import (
	"encoding/json"
	"testing"
)

// uiPartRoundTrip verifies that a UIPart survives a full marshal → unmarshal →
// remarshal cycle with identical JSON, and that the "type" discriminator in the
// output always matches uiPartType().
//
// When adding a new field to a UIPart type, add it (with a distinctive non-zero
// value) to the corresponding call below in TestUIPartRoundTrip so that the
// round-trip catches any marshal/unmarshal gap.
func uiPartRoundTrip(t *testing.T, name string, p UIPart) {
	t.Helper()

	data, err := MarshalUIPart(p)
	if err != nil {
		t.Fatalf("%s: marshal: %v", name, err)
	}

	var disc struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &disc); err != nil {
		t.Fatalf("%s: read type discriminator: %v", name, err)
	}
	if disc.Type != p.uiPartType() {
		t.Errorf("%s: type field: got %q, want %q", name, disc.Type, p.uiPartType())
	}

	got, err := UnmarshalUIPart(data)
	if err != nil {
		t.Fatalf("%s: unmarshal: %v", name, err)
	}
	data2, err := MarshalUIPart(got)
	if err != nil {
		t.Fatalf("%s: remarshal: %v", name, err)
	}
	if string(data) != string(data2) {
		t.Errorf("%s: round-trip mismatch\n  got:  %s\n  want: %s", name, data2, data)
	}
}

// TestUIPartRoundTrip exercises every UIPart concrete type with all fields
// populated. A field left at its zero value here will not be detected if the
// marshal/unmarshal code silently drops it.
//
// MAINTENANCE: update this test when adding new fields to any UIPart type.
func TestUIPartRoundTrip(t *testing.T) {
	uiPartRoundTrip(t, "UITextPart", UITextPart{
		Type:             "text",
		Text:             "hello world",
		State:            "done",
		ProviderMetadata: json.RawMessage(`{"k":"v"}`),
	})
	uiPartRoundTrip(t, "UIReasoningPart", UIReasoningPart{
		Type:             "reasoning",
		Text:             "thinking...",
		State:            "done",
		ProviderMetadata: json.RawMessage(`{"m":"n"}`),
	})
	uiPartRoundTrip(t, "UIFilePart", UIFilePart{
		Type:             "file",
		URL:              "data:image/png;base64,abc",
		MediaType:        "image/png",
		Filename:         "test.png",
		ProviderMetadata: json.RawMessage(`{"f":"g"}`),
	})
	uiPartRoundTrip(t, "UIStepStartPart", UIStepStartPart{Type: "step-start"})
	uiPartRoundTrip(t, "UISourceURLPart", UISourceURLPart{
		Type:             "source-url",
		SourceID:         "s1",
		URL:              "https://example.com",
		Title:            "Example",
		ProviderMetadata: json.RawMessage(`{"p":"q"}`),
	})
	uiPartRoundTrip(t, "UISourceDocumentPart", UISourceDocumentPart{
		Type:             "source-document",
		SourceID:         "s2",
		MediaType:        "application/pdf",
		Title:            "Doc",
		Filename:         "doc.pdf",
		ProviderMetadata: json.RawMessage(`{"r":"s"}`),
	})
	uiPartRoundTrip(t, "UIDataPart", UIDataPart{
		Type: "data-custom",
		ID:   "d1",
		Data: json.RawMessage(`{"value":42}`),
	})
	uiPartRoundTrip(t, "DynamicToolPart", DynamicToolPart{
		Type:             "dynamic-tool",
		ToolName:         "read",
		ToolCallID:       "tc1",
		State:            "output-available",
		Title:            "Reading file",
		ProviderExecuted: new(bool),
		Input:            json.RawMessage(`{"path":"foo"}`),
		Output:           json.RawMessage(`"contents"`),
		Approval: &ToolApproval{
			ID:       "a1",
			Approved: new(bool),
			Reason:   "looks safe",
		},
		CallProviderMetadata: json.RawMessage(`{"x":"y"}`),
	})
}

// TestMarshalUIPartTypeAlwaysFromMethod verifies that MarshalUIPart always
// serializes the "type" field from uiPartType(), even when the struct's own
// Type field is empty or contains an incorrect value.
func TestMarshalUIPartTypeAlwaysFromMethod(t *testing.T) {
	cases := []struct {
		name     string
		part     UIPart
		wantType string
	}{
		{"UITextPart/unset", UITextPart{}, "text"},
		{"UITextPart/wrong", UITextPart{Type: "wrong"}, "text"},
		{"UIReasoningPart/unset", UIReasoningPart{}, "reasoning"},
		{"UIFilePart/unset", UIFilePart{}, "file"},
		{"UIStepStartPart/unset", UIStepStartPart{}, "step-start"},
		{"UISourceURLPart/unset", UISourceURLPart{}, "source-url"},
		{"UISourceDocumentPart/unset", UISourceDocumentPart{}, "source-document"},
		// UIDataPart.uiPartType() returns the Type field, so the field is the source of truth.
		{"UIDataPart", UIDataPart{Type: "data-foo"}, "data-foo"},
	}

	for _, tc := range cases {
		data, err := MarshalUIPart(tc.part)
		if err != nil {
			t.Errorf("%s: marshal: %v", tc.name, err)
			continue
		}
		var disc struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &disc); err != nil {
			t.Errorf("%s: read type: %v", tc.name, err)
			continue
		}
		if disc.Type != tc.wantType {
			t.Errorf("%s: type: got %q, want %q", tc.name, disc.Type, tc.wantType)
		}
	}
}

// TestMarshalUIPartUnknownType verifies that MarshalUIPart returns an error for
// an unrecognized UIPart implementation, rather than silently producing bad JSON.
func TestMarshalUIPartUnknownType(t *testing.T) {
	_, err := MarshalUIPart(testAlienUIPart{})
	if err == nil {
		t.Error("expected error for unknown UIPart type")
	}
}

// testAlienUIPart is a package-level UIPart implementation that is not handled
// by MarshalUIPart's type switch, used to exercise the default error branch.
type testAlienUIPart struct{}

func (testAlienUIPart) uiPartType() string { return "alien" }
