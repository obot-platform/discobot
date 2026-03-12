package cli

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/obot-platform/discobot/agent-go/message"
)

func TestToolOutputDetail_DefaultTool(t *testing.T) {
	detail := toolOutputDetail("Bash", json.RawMessage(`{"command":"pwd"}`))
	if detail != "" {
		t.Fatalf("expected no detail for non-special tool, got %q", detail)
	}
}

func TestToolOutputDetail_Write(t *testing.T) {
	detail := toolOutputDetail("Write", json.RawMessage(`{"file_path":"/tmp/file.txt","content":"line1\nline2\n"}`))
	if !strings.HasPrefix(detail, "written content:\n") {
		t.Fatalf("expected written content header, got %q", detail)
	}
	if !strings.Contains(detail, "line1") || !strings.Contains(detail, "line2") {
		t.Fatalf("expected written content lines in detail, got %q", detail)
	}
}

func TestToolOutputDetail_Edit(t *testing.T) {
	detail := toolOutputDetail("Edit", json.RawMessage(`{"file_path":"/tmp/file.txt","old_string":"line one\nline two","new_string":"line one\nline 2\nline three"}`))
	if !strings.HasPrefix(detail, "applied diff:\n") {
		t.Fatalf("expected applied diff header, got %q", detail)
	}
	for _, want := range []string{"--- old", "+++ new", " line one", "-line two", "+line 2", "+line three"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("expected detail to contain %q, got %q", want, detail)
		}
	}
}

func TestToolErrorDetail(t *testing.T) {
	detail := toolErrorDetail("line1\r\nline2\n")
	if detail != "tool output:\nline1\nline2" {
		t.Fatalf("unexpected tool error detail: %q", detail)
	}
}

func TestRenderChunk_ToolOutputErrorPrintsToolOutput(t *testing.T) {
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stderrReader.Close()

	oldStderr := os.Stderr
	oldNoColor := noColor
	os.Stderr = stderrWriter
	noColor = true
	defer func() {
		os.Stderr = oldStderr
		noColor = oldNoColor
	}()

	renderChunk(message.ToolOutputErrorChunk{
		ToolCallID: "tool-call-12345678",
		ErrorText:  "line1\nline2",
	}, nil, nil)

	if err := stderrWriter.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(stderrReader)
	if err != nil {
		t.Fatal(err)
	}

	out := string(output)
	for _, want := range []string{
		"[tool(12345678)] error: 2 lines",
		"tool output:",
		"line1",
		"line2",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}
