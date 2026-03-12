package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestMarkdownRenderer_StreamPlainTextImmediately(t *testing.T) {
	var out bytes.Buffer
	r := newMarkdownRenderer(&out, true, false)

	r.WriteText("hello")

	if got := out.String(); got != "hello" {
		t.Fatalf("expected immediate plain output, got %q", got)
	}
}

func TestMarkdownRenderer_BuffersFenceUntilClosed(t *testing.T) {
	var out bytes.Buffer
	r := newMarkdownRenderer(&out, true, false)

	r.WriteText("```go\nfmt.Println(1)\n")
	if got := out.String(); got != "" {
		t.Fatalf("expected no output before fence closes, got %q", got)
	}

	r.WriteText("```\n")

	want := "```go\nfmt.Println(1)\n```\n"
	if got := out.String(); got != want {
		t.Fatalf("fence output mismatch\nwant: %q\n got: %q", want, got)
	}
}

func TestMarkdownRenderer_NoColorOmitsANSI(t *testing.T) {
	var out bytes.Buffer
	r := newMarkdownRenderer(&out, true, false)

	r.WriteText("# Title\n")

	got := out.String()
	if strings.Contains(got, "\033[") {
		t.Fatalf("expected no ANSI escapes with color disabled, got %q", got)
	}
	if got != "# Title\n" {
		t.Fatalf("expected plain heading output, got %q", got)
	}
}

func TestMarkdownRenderer_ColorizesHeading(t *testing.T) {
	var out bytes.Buffer
	r := newMarkdownRenderer(&out, true, true)

	r.WriteText("# Title\n")

	got := out.String()
	if !strings.Contains(got, "\033[") {
		t.Fatalf("expected ANSI escapes with color enabled, got %q", got)
	}
	if !strings.Contains(got, "Title") {
		t.Fatalf("expected heading content in output, got %q", got)
	}
}

func TestMarkdownRenderer_FlushForBoundaryFlushesPendingTail(t *testing.T) {
	var out bytes.Buffer
	r := newMarkdownRenderer(&out, true, false)

	r.WriteText("**bold")
	if got := out.String(); got != "" {
		t.Fatalf("expected tail buffering before boundary flush, got %q", got)
	}
	if !r.AtLineStart() {
		t.Fatalf("expected buffered tail to keep renderer at line start")
	}

	r.FlushForBoundary()
	if got := out.String(); got != "**bold" {
		t.Fatalf("expected boundary flush to emit pending text, got %q", got)
	}
	if r.AtLineStart() {
		t.Fatalf("expected flushed inline text to leave renderer mid-line")
	}
}

func TestMarkdownRenderer_AtLineStartTracksNewlines(t *testing.T) {
	var out bytes.Buffer
	r := newMarkdownRenderer(&out, true, false)

	r.WriteText("hello")
	if r.AtLineStart() {
		t.Fatalf("expected plain text to leave renderer mid-line")
	}

	r.WriteText("\n")
	if !r.AtLineStart() {
		t.Fatalf("expected newline-terminated text to leave renderer at line start")
	}
}

func TestMarkdownRenderer_FinishFlushesUnclosedFence(t *testing.T) {
	var out bytes.Buffer
	r := newMarkdownRenderer(&out, true, false)

	r.WriteText("```\ncode")
	if got := out.String(); got != "" {
		t.Fatalf("expected no output while unclosed fence is buffered, got %q", got)
	}

	r.Finish()
	got := out.String()
	if !strings.Contains(got, "```\ncode") {
		t.Fatalf("expected finish to flush unclosed fence buffer, got %q", got)
	}
}

func TestMarkdownRenderer_NonFormatModePassThrough(t *testing.T) {
	var out bytes.Buffer
	r := newMarkdownRenderer(&out, false, true)

	r.WriteText("# H\n")
	r.WriteText("tail")
	r.Finish()

	got := out.String()
	if got != "# H\ntail" {
		t.Fatalf("expected passthrough output in non-format mode, got %q", got)
	}
	if strings.Contains(got, "\033[") {
		t.Fatalf("unexpected ANSI escapes in non-format mode: %q", got)
	}
}
