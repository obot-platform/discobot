package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/obot-platform/discobot/agent-go/message"
)

// runTool is a generic test helper that executes any named tool and returns
// (output text, success). Mirrors the pattern used by runBash / runEdit.
func runTool(t *testing.T, e *Executor, toolName string, input map[string]any) (string, bool) {
	t.Helper()
	raw, _ := json.Marshal(input)
	result, err := e.Execute(context.Background(), nil, message.ToolCallPart{
		ToolCallID: t.Name(),
		ToolName:   toolName,
		Input:      string(raw),
	})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	switch v := result.Result.Output.(type) {
	case message.TextOutput:
		return v.Value, true
	case message.ErrorTextOutput:
		return v.Value, false
	}
	return "", false
}

// bigFile writes n lines of 30 chars each to path and returns the full content.
func bigFile(t *testing.T, path string, lines int) string {
	t.Helper()
	var b strings.Builder
	for range lines {
		b.WriteString(strings.Repeat("x", 30) + "\n")
	}
	content := b.String()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return content
}

// TestLimitOutput_ShortOutput verifies output within the limit passes through unchanged.
func TestLimitOutput_ShortOutput(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "file.txt"), []byte("hello world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := New(cwd, t.TempDir(), t.Name())
	out, ok := runTool(t, e, "Read", map[string]any{"file_path": "file.txt"})
	if !ok {
		t.Fatalf("unexpected error: %s", out)
	}
	if strings.Contains(out, "Output too long") {
		t.Errorf("short output should not be truncated, got: %q", out)
	}
	if !strings.Contains(out, "hello world") {
		t.Errorf("expected file content in output, got: %q", out)
	}
}

// TestLimitOutput_LongOutputTruncated verifies output exceeding maxOutputLen is
// truncated and the full content is written to a file.
func TestLimitOutput_LongOutputTruncated(t *testing.T) {
	cwd := t.TempDir()
	// 3_000 lines × ~37 chars with line numbers > maxOutputLen (30_000).
	bigFile(t, filepath.Join(cwd, "big.txt"), 3_000)

	e := New(cwd, t.TempDir(), t.Name())
	out, ok := runTool(t, e, "Read", map[string]any{"file_path": "big.txt"})
	if !ok {
		t.Fatalf("unexpected error: %s", out)
	}
	if !strings.Contains(out, "Output too long") {
		t.Error("expected truncation notice in output")
	}
	if !strings.Contains(out, "Full output written to") {
		t.Error("expected file path in truncation notice")
	}
	if !strings.Contains(out, "truncated") {
		t.Error("expected truncation footer in output")
	}
	if len(out) >= maxOutputLen {
		t.Errorf("truncated output length %d should be < maxOutputLen %d", len(out), maxOutputLen)
	}
}

// TestLimitOutput_SpillFileContainsFullOutput verifies the spill file holds the
// complete original content, including content beyond the inline preview.
func TestLimitOutput_SpillFileContainsFullOutput(t *testing.T) {
	cwd := t.TempDir()
	needle := "UNIQUE_NEEDLE_CONTENT"

	// Put the needle past the previewLen so it won't appear inline.
	var b strings.Builder
	for range 2_900 {
		b.WriteString(strings.Repeat("y", 30) + "\n")
	}
	b.WriteString(needle + "\n")
	if err := os.WriteFile(filepath.Join(cwd, "big.txt"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	e := New(cwd, t.TempDir(), t.Name())
	out, ok := runTool(t, e, "Read", map[string]any{"file_path": "big.txt"})
	if !ok {
		t.Fatalf("unexpected error: %s", out)
	}
	if strings.Contains(out, needle) {
		t.Error("needle should not appear in the truncated inline preview")
	}

	// Extract the spill file path from the message.
	const marker = "Full output written to: "
	idx := strings.Index(out, marker)
	if idx < 0 {
		t.Fatalf("could not find spill file path in output: %s", out)
	}
	spillPath := strings.TrimSpace(strings.SplitN(out[idx+len(marker):], "]", 2)[0])

	spillData, err := os.ReadFile(spillPath)
	if err != nil {
		t.Fatalf("could not read spill file %s: %v", spillPath, err)
	}
	if !strings.Contains(string(spillData), needle) {
		t.Error("spill file must contain the complete original content including the needle")
	}
}

// TestLimitOutput_SpillPathUnderDataDir verifies spill files land in
// threads/{threadID}/output/ under the configured data directory.
func TestLimitOutput_SpillPathUnderDataDir(t *testing.T) {
	cwd := t.TempDir()
	bigFile(t, filepath.Join(cwd, "big.txt"), 3_000)

	threadID := "my-thread"
	dataDir := t.TempDir()
	e := New(cwd, dataDir, threadID)
	out, ok := runTool(t, e, "Read", map[string]any{"file_path": "big.txt"})
	if !ok {
		t.Fatalf("unexpected error: %s", out)
	}

	expectedDir := filepath.Join(dataDir, "threads", threadID, "output")
	if !strings.Contains(out, expectedDir) {
		t.Errorf("spill path should be under %s; got:\n%s", expectedDir, out)
	}
}

// TestLimitOutput_AppliesToBash verifies that long Bash output is also truncated.
func TestLimitOutput_AppliesToBash(t *testing.T) {
	e := New(t.TempDir(), t.TempDir(), t.Name())
	// Generate > maxOutputLen chars via bash (printf repeats a char N times).
	out, ok := runTool(t, e, "Bash", map[string]any{
		"command": "python3 -c \"print('a' * 35000)\"",
	})
	if !ok {
		t.Fatalf("unexpected error: %s", out)
	}
	if !strings.Contains(out, "Output too long") {
		t.Error("expected truncation notice for long Bash output")
	}
}
