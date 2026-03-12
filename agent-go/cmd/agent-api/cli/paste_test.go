package cli

import "testing"

func TestPastedSummary_FormatsLinesAndChars(t *testing.T) {
	summary := pastedSummary([]byte("hello\nworld\n"))
	if summary != "[pasted 2 lines/12 chars]" {
		t.Fatalf("unexpected summary: %q", summary)
	}
	if summary := pastedSummary([]byte("hello\r\nworld\r\n")); summary != "[pasted 2 lines/12 chars]" {
		t.Fatalf("unexpected CRLF summary: %q", summary)
	}
	if pastedLineCount([]byte("single")) != 1 {
		t.Fatalf("expected single line count")
	}
	if pastedLineCount([]byte("hello\rworld")) != 2 {
		t.Fatalf("expected CR-delimited paste to count as two lines")
	}
	if pastedCharCount([]byte("hello\r\nworld")) != 11 {
		t.Fatalf("expected normalized character count")
	}
	if pastedLineCount(nil) != 0 {
		t.Fatalf("expected empty paste line count")
	}
}

func TestNormalizePastedChunks_TrimsInvalidTail(t *testing.T) {
	chunks := []pastedChunk{
		{end: 5, rawLen: 3, dispLen: 8},
		{end: 10, rawLen: 4, dispLen: 9},
	}

	normalized := normalizePastedChunks(chunks, 6)
	if len(normalized) != 1 {
		t.Fatalf("expected one valid chunk, got %d", len(normalized))
	}
	if normalized[0].end != 5 {
		t.Fatalf("unexpected remaining chunk end: %d", normalized[0].end)
	}
}

func TestNormalizePastedChunks_DropsAllInvalidChunks(t *testing.T) {
	chunks := []pastedChunk{{end: 12, rawLen: 6, dispLen: 20}}
	normalized := normalizePastedChunks(chunks, 4)
	if len(normalized) != 0 {
		t.Fatalf("expected no valid chunks, got %d", len(normalized))
	}
}
