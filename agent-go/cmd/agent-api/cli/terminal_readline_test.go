package cli

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/term"
)

func TestReadLineReadlineTTY_SingleLinePasteInlinesContent(t *testing.T) {
	input := bytes.NewBufferString("\x1b[200~hello\x1b[201~\r")
	var output bytes.Buffer

	line, err := readLineReadlineTTY(
		"> ",
		nil,
		input,
		&output,
		120,
		40,
		func() (*term.State, error) { return nil, nil },
		func(*term.State) error { return nil },
	)
	if err != nil {
		t.Fatalf("readLineReadlineTTY() error = %v", err)
	}
	if line != "hello" {
		t.Fatalf("line = %q, want %q", line, "hello")
	}

	out := output.String()
	if strings.Contains(out, "[pasted 1 lines/5 bytes]") {
		t.Fatalf("expected small single-line paste to be inlined, got %q", out)
	}
}

func TestReadLineReadlineTTY_BracketedPasteWithTypedTextInlinesContent(t *testing.T) {
	input := bytes.NewBufferString("ab\x1b[200~hello\x1b[201~cd\r")
	var output bytes.Buffer

	line, err := readLineReadlineTTY(
		"> ",
		nil,
		input,
		&output,
		120,
		40,
		func() (*term.State, error) { return nil, nil },
		func(*term.State) error { return nil },
	)
	if err != nil {
		t.Fatalf("readLineReadlineTTY() error = %v", err)
	}
	if line != "abhellocd" {
		t.Fatalf("line = %q, want %q", line, "abhellocd")
	}

	out := output.String()
	if strings.Contains(out, "[pasted 1 lines/5 bytes]") {
		t.Fatalf("expected small single-line paste to be inlined, got %q", out)
	}
}

func TestReadLineReadlineTTY_MultilinePasteShowsCompactBlock(t *testing.T) {
	input := bytes.NewBufferString("\x1b[200~hello\nworld\x1b[201~\r")
	var output bytes.Buffer

	line, err := readLineReadlineTTY(
		"> ",
		nil,
		input,
		&output,
		120,
		40,
		func() (*term.State, error) { return nil, nil },
		func(*term.State) error { return nil },
	)
	if err != nil {
		t.Fatalf("readLineReadlineTTY() error = %v", err)
	}
	if line != "hello\nworld" {
		t.Fatalf("line = %q, want %q", line, "hello\nworld")
	}

	out := output.String()
	if !strings.Contains(out, "[pasted 2 lines/11 bytes]") {
		t.Fatalf("expected compact multiline paste summary in output, got %q", out)
	}
	if strings.Contains(out, "hello\nworld") {
		t.Fatalf("expected pasted content to be collapsed in output, got %q", out)
	}
}

func TestShouldInlinePastedContent(t *testing.T) {
	if !shouldInlinePastedContent([]byte("hello world")) {
		t.Fatal("expected short printable single-line paste to inline")
	}
	if shouldInlinePastedContent([]byte("hello\nworld")) {
		t.Fatal("expected multiline paste not to inline")
	}
	if shouldInlinePastedContent(bytes.Repeat([]byte("a"), 100)) {
		t.Fatal("expected 100-char paste not to inline")
	}
	if shouldInlinePastedContent([]byte{0xff, 0xfe, 0xfd}) {
		t.Fatal("expected invalid UTF-8 paste not to inline")
	}
	if shouldInlinePastedContent([]byte("hello\x07")) {
		t.Fatal("expected non-printable paste not to inline")
	}
}

func TestReadLineReadlineTTY_LongSingleLinePasteShowsSummary(t *testing.T) {
	payload := strings.Repeat("a", 100)
	input := bytes.NewBufferString("\x1b[200~" + payload + "\x1b[201~\r")
	var output bytes.Buffer

	line, err := readLineReadlineTTY(
		"> ",
		nil,
		input,
		&output,
		120,
		40,
		func() (*term.State, error) { return nil, nil },
		func(*term.State) error { return nil },
	)
	if err != nil {
		t.Fatalf("readLineReadlineTTY() error = %v", err)
	}
	if line != payload {
		t.Fatalf("line length = %d, want %d", len(line), len(payload))
	}
	if !strings.Contains(output.String(), "[pasted 1 lines/100 bytes]") {
		t.Fatalf("expected long single-line paste summary, got %q", output.String())
	}
}

func TestReadLineReadlineTTY_BackspaceInsidePasteBlockRemovesWholeBlock(t *testing.T) {
	payload := strings.Repeat("a", 100)
	input := bytes.NewBufferString("\x1b[200~" + payload + "\x1b[201~\x7f\r")
	var output bytes.Buffer

	line, err := readLineReadlineTTY(
		"> ",
		nil,
		input,
		&output,
		120,
		40,
		func() (*term.State, error) { return nil, nil },
		func(*term.State) error { return nil },
	)
	if err != nil {
		t.Fatalf("readLineReadlineTTY() error = %v", err)
	}
	if line != "" {
		t.Fatalf("line = %q, want empty after malformed block edit", line)
	}
}

func TestReadLineReadlineTTY_DeleteInsidePasteBlockRemovesWholeBlock(t *testing.T) {
	payload := strings.Repeat("a", 100)
	input := bytes.NewBufferString("\x1b[200~" + payload + "\x1b[201~\x1b[D\x1b[D\x1b[D\x1b[3~\r")
	var output bytes.Buffer

	line, err := readLineReadlineTTY(
		"> ",
		nil,
		input,
		&output,
		120,
		40,
		func() (*term.State, error) { return nil, nil },
		func(*term.State) error { return nil },
	)
	if err != nil {
		t.Fatalf("readLineReadlineTTY() error = %v", err)
	}
	if line != "" {
		t.Fatalf("line = %q, want empty after malformed block delete", line)
	}
}

func TestRemoveMalformedPasteBlocks(t *testing.T) {
	tests := []struct {
		name string
		in   string
		out  string
	}{
		{name: "well formed kept", in: "[pasted 1 lines/31 bytes]", out: "[pasted 1 lines/31 bytes]"},
		{name: "missing close bracket removed", in: "[pasted 1 lines/31 bytes", out: ""},
		{name: "broken bracketed prefix removed", in: "[asted 1 lines/31 bytes]", out: ""},
		{name: "missing open bracket removed", in: "pasted 1 lines/31 bytes]", out: ""},
		{name: "missing byte character removed", in: "[pasted 1 lines/31 byts]", out: ""},
		{name: "unbracketed missing byte character removed", in: "pasted 1 lines/31 byts]", out: ""},
		{name: "inline malformed removed", in: "abc [pasted 1 lines/31 byte] xyz", out: "abc  xyz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := removeMalformedPasteBlocks(tt.in)
			if got != tt.out {
				t.Fatalf("removeMalformedPasteBlocks(%q) = %q, want %q", tt.in, got, tt.out)
			}
		})
	}
}

func TestSanitizeMalformedPasteBlocksWithCursor(t *testing.T) {
	cleaned, newPos, changed := sanitizeMalformedPasteBlocksWithCursor("[pasted 1 lines/31 bytes", 10)
	if !changed {
		t.Fatal("expected malformed paste block to be removed")
	}
	if cleaned != "" {
		t.Fatalf("cleaned = %q, want empty", cleaned)
	}
	if newPos != 0 {
		t.Fatalf("newPos = %d, want 0", newPos)
	}
}

func TestInputTrackingReader_AddsSanitizeTriggerAfterEditKeys(t *testing.T) {
	r := &inputTrackingReader{}
	r.appendOutput([]byte{0x7f})
	if !bytes.Equal(r.pendingOut, []byte{0x7f, sanitizeTriggerKey}) {
		t.Fatalf("pendingOut = %v, want backspace+trigger", r.pendingOut)
	}

	r.pendingOut = nil
	r.editSeqState = 0
	r.appendOutput([]byte{0x1b, '[', '3', '~'})
	if !bytes.Equal(r.pendingOut, []byte{0x1b, '[', '3', '~', sanitizeTriggerKey}) {
		t.Fatalf("pendingOut = %v, want delete sequence + trigger", r.pendingOut)
	}
}

func TestCompleteSlashCommand_SingleMatch(t *testing.T) {
	line, pos, matches, ok := completeSlashCommand("/his", len("/his"), []string{"/history", "/help", "/models"})
	if !ok {
		t.Fatal("expected completion match")
	}
	if line != "/history " {
		t.Fatalf("line = %q, want %q", line, "/history ")
	}
	if pos != len("/history ") {
		t.Fatalf("pos = %d, want %d", pos, len("/history "))
	}
	if len(matches) != 1 || matches[0] != "/history" {
		t.Fatalf("unexpected matches: %v", matches)
	}
}

func TestCompleteSlashCommand_FirstTabDoesNotShowChoices(t *testing.T) {
	var output bytes.Buffer
	input := bytes.NewBufferString("/\t\r")
	opts := &readLineOptions{slashCommands: []string{"/clear", "/compact", "/history"}}

	line, err := readLineReadlineTTYWithOptions(
		"> ",
		nil,
		input,
		&output,
		120,
		40,
		opts,
		func() (*term.State, error) { return nil, nil },
		func(*term.State) error { return nil },
	)
	if err != nil {
		t.Fatalf("readLineReadlineTTYWithOptions() error = %v", err)
	}
	if line != "/" {
		t.Fatalf("line = %q, want %q", line, "/")
	}
	out := output.String()
	if strings.Contains(out, "/clear") || strings.Contains(out, "/compact") {
		t.Fatalf("expected first tab not to print choices, got %q", out)
	}
}

func TestCompleteSlashCommand_SecondTabShowsChoices(t *testing.T) {
	var output bytes.Buffer
	input := bytes.NewBufferString("/\t\t\r")
	opts := &readLineOptions{slashCommands: []string{"/clear", "/compact", "/history"}}

	line, err := readLineReadlineTTYWithOptions(
		"> ",
		nil,
		input,
		&output,
		120,
		40,
		opts,
		func() (*term.State, error) { return nil, nil },
		func(*term.State) error { return nil },
	)
	if err != nil {
		t.Fatalf("readLineReadlineTTYWithOptions() error = %v", err)
	}
	if line != "/" {
		t.Fatalf("line = %q, want %q", line, "/")
	}
	out := output.String()
	if !strings.Contains(out, "/clear") || !strings.Contains(out, "/compact") {
		t.Fatalf("expected second tab to print choices, got %q", out)
	}
	if !strings.Contains(out, "> /") {
		t.Fatalf("expected prompt and buffer redraw after list, got %q", out)
	}
}

func TestCompleteSlashCommand_RepeatedTabDoesNotReprintChoices(t *testing.T) {
	var output bytes.Buffer
	input := bytes.NewBufferString("/\t\t\t\r")
	opts := &readLineOptions{slashCommands: []string{"/clear", "/compact", "/history"}}

	line, err := readLineReadlineTTYWithOptions(
		"> ",
		nil,
		input,
		&output,
		120,
		40,
		opts,
		func() (*term.State, error) { return nil, nil },
		func(*term.State) error { return nil },
	)
	if err != nil {
		t.Fatalf("readLineReadlineTTYWithOptions() error = %v", err)
	}
	if line != "/" {
		t.Fatalf("line = %q, want %q", line, "/")
	}
	out := output.String()
	if strings.Count(out, "/clear") != 1 {
		t.Fatalf("expected choices to print once, got %q", out)
	}
}

func TestTruncateCompletionLine_OneLineWithEllipsis(t *testing.T) {
	line := "/clear  /compact  /history"
	got := truncateCompletionLine(line, 12)
	if len([]rune(got)) > 12 {
		t.Fatalf("truncated line too long: %q", got)
	}
	if !strings.HasSuffix(got, " ...") {
		t.Fatalf("expected ellipsis suffix, got %q", got)
	}
}

func TestCompleteSlashCommand_CompletionsTruncateToWidth(t *testing.T) {
	var output bytes.Buffer
	input := bytes.NewBufferString("/\t\t\r")
	opts := &readLineOptions{slashCommands: []string{"/clear", "/compact", "/history", "/multiline"}}

	line, err := readLineReadlineTTYWithOptions(
		"> ",
		nil,
		input,
		&output,
		16,
		40,
		opts,
		func() (*term.State, error) { return nil, nil },
		func(*term.State) error { return nil },
	)
	if err != nil {
		t.Fatalf("readLineReadlineTTYWithOptions() error = %v", err)
	}
	if line != "/" {
		t.Fatalf("line = %q, want %q", line, "/")
	}
	out := output.String()
	if !strings.Contains(out, " ...") {
		t.Fatalf("expected truncated completion line with ellipsis, got %q", out)
	}
}

func TestReadLineReadlineTTY_CtrlCMapsToInterrupt(t *testing.T) {
	input := bytes.NewBuffer([]byte{0x03})
	var output bytes.Buffer

	line, err := readLineReadlineTTY(
		"> ",
		nil,
		input,
		&output,
		120,
		40,
		func() (*term.State, error) { return nil, nil },
		func(*term.State) error { return nil },
	)
	if err != errInterrupt {
		t.Fatalf("err = %v, want %v", err, errInterrupt)
	}
	if line != "" {
		t.Fatalf("line = %q, want empty", line)
	}
	if !strings.Contains(output.String(), "^C") {
		t.Fatalf("expected ^C marker in output, got %q", output.String())
	}
}
