package tools

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestBuildRgArgs_UsesRegexpFlagForPattern(t *testing.T) {
	got := buildRgArgs(grepInput{
		Pattern:    "--model|--plan|--max-turns",
		OutputMode: "content",
	}, "/tmp/search")

	want := []string{"-n", "--regexp", "--model|--plan|--max-turns", "/tmp/search"}
	if !slices.Equal(got, want) {
		t.Fatalf("buildRgArgs() = %#v, want %#v", got, want)
	}
}

func TestGrep_PatternStartingWithDashWorksWithRipgrep(t *testing.T) {
	if !isRgAvailable() {
		t.Skip("rg not available")
	}

	cwd := t.TempDir()
	content := strings.Join([]string{
		"--model",
		"--plan",
		"--max-turns",
		"--subagent",
		"--new-thread",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(cwd, "flags.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	e := New(cwd, t.TempDir(), t.Name())
	out, ok := runTool(t, e, "Grep", map[string]any{
		"pattern":     "--model|--plan|--max-turns|--subagent|--new-thread",
		"path":        cwd,
		"output_mode": "content",
	})
	if !ok {
		t.Fatalf("expected successful grep result, got error: %s", out)
	}
	if !strings.Contains(out, "flags.txt:1:--model") {
		t.Fatalf("expected first matched flag in output, got: %q", out)
	}
	if !strings.Contains(out, "flags.txt:5:--new-thread") {
		t.Fatalf("expected last matched flag in output, got: %q", out)
	}
}
