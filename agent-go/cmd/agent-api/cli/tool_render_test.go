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

func TestRenderChunk_ToolInputDoesNotStartWithBlankLine(t *testing.T) {
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

	renderChunk(message.ToolInputAvailableChunk{
		ToolCallID: "tool-call-12345678",
		ToolName:   "Bash",
		Input:      json.RawMessage(`{"command":"pwd"}`),
	}, nil, newToolRenderState())

	if err := stderrWriter.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(stderrReader)
	if err != nil {
		t.Fatal(err)
	}

	out := string(output)
	if strings.HasPrefix(out, "\n") {
		t.Fatalf("expected tool input to avoid a leading blank line, got %q", out)
	}
	if !strings.Contains(out, "→ [Bash(12345678)]") {
		t.Fatalf("expected tool input summary in output, got %q", out)
	}
}

func TestSectionNeedsGap(t *testing.T) {
	tests := []struct {
		from sectionKind
		to   sectionKind
		want bool
	}{
		// tool → tool: never a gap
		{skTool, skTool, false},
		// same section continuation: no gap
		{skText, skText, false},
		{skReasoning, skReasoning, false},
		// start of turn: gap only for text/reasoning
		{skNone, skTool, false},
		{skNone, skText, true},
		{skNone, skReasoning, true},
		// tool ↔ text/reasoning: gap
		{skTool, skText, true},
		{skTool, skReasoning, true},
		{skReasoning, skTool, true},
		{skReasoning, skText, true},
		{skText, skTool, true},
		{skText, skReasoning, true},
	}

	for _, tt := range tests {
		got := sectionNeedsGap(tt.from, tt.to)
		if got != tt.want {
			t.Errorf("sectionNeedsGap(%v, %v) = %v, want %v", tt.from, tt.to, got, tt.want)
		}
	}
}

func TestToolInputSummary_SpecialTools(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		input    json.RawMessage
		want     string
	}{
		{
			name:     "glob shows pattern first",
			toolName: "Glob",
			input:    json.RawMessage(`{"path":"/repo","pattern":"**/*.go"}`),
			want:     "pattern: **/*.go path: /repo",
		},
		{
			name:     "grep shows pattern",
			toolName: "Grep",
			input:    json.RawMessage(`{"path":"/repo","pattern":"TODO","glob":"*.go"}`),
			want:     "pattern: TODO path: /repo glob: *.go",
		},
		{
			name:     "websearch shows query",
			toolName: "WebSearch",
			input:    json.RawMessage(`{"query":"golang context docs"}`),
			want:     "query: golang context docs",
		},
		{
			name:     "webfetch shows url",
			toolName: "WebFetch",
			input:    json.RawMessage(`{"url":"https://example.com/docs","prompt":"summarize"}`),
			want:     "url: https://example.com/docs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toolInputSummary(tt.toolName, tt.input); got != tt.want {
				t.Fatalf("toolInputSummary() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderChunk_ToolInputShowsGlobPattern(t *testing.T) {
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

	renderChunk(message.ToolInputAvailableChunk{
		ToolCallID: "tool-call-12345678",
		ToolName:   "Glob",
		Input:      json.RawMessage(`{"path":"/repo","pattern":"**/*.go"}`),
	}, nil, newToolRenderState())

	if err := stderrWriter.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(stderrReader)
	if err != nil {
		t.Fatal(err)
	}

	out := string(output)
	for _, want := range []string{
		"→ [Glob(12345678)]",
		"pattern: **/*.go",
		"path: /repo",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected output to contain %q, got %q", want, out)
		}
	}
}
