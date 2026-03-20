package gogrep

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupTestDir creates a temporary directory with test files.
func setupTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	writeFile(t, dir, "hello.go", `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
	fmt.Println("hello, world!")
	fmt.Println("goodbye")
}
`)

	writeFile(t, dir, "math.go", `package main

func add(a, b int) int {
	return a + b
}

func subtract(a, b int) int {
	return a - b
}

func multiply(a, b int) int {
	return a * b
}
`)

	writeFile(t, dir, "data.json", `{
  "name": "test",
  "value": 42,
  "items": ["foo", "bar", "baz"]
}
`)

	writeFile(t, dir, "readme.md", `# Test Project

This is a test project for gogrep.
It contains multiple files for testing.
`)

	// Create a subdirectory
	subdir := filepath.Join(dir, "sub")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, subdir, "nested.go", `package sub

func Nested() string {
	return "nested function"
}
`)

	// Create a binary file (should be skipped)
	binPath := filepath.Join(dir, "binary.dat")
	if err := os.WriteFile(binPath, []byte("hello\x00world"), 0o644); err != nil {
		t.Fatal(err)
	}

	return dir
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGrepLiteral(t *testing.T) {
	dir := setupTestDir(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "Hello",
		Path:    dir,
	})
	if err != nil {
		t.Fatal(err)
	}

	if results.TotalCount == 0 {
		t.Fatal("expected at least one match for 'Hello'")
	}

	found := false
	for _, fm := range results.Files {
		for _, m := range fm.Matches {
			if m.Line == `	fmt.Println("Hello, World!")` {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected to find Hello, World! line")
	}
}

func TestGrepCaseInsensitive(t *testing.T) {
	dir := setupTestDir(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern:         "hello",
		Path:            dir,
		CaseInsensitive: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Should match both "Hello" and "hello" lines
	if results.TotalCount < 2 {
		t.Fatalf("expected at least 2 matches for case-insensitive 'hello', got %d", results.TotalCount)
	}
}

func TestGrepRegex(t *testing.T) {
	dir := setupTestDir(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: `func \w+\(`,
		Path:    dir,
	})
	if err != nil {
		t.Fatal(err)
	}

	if results.TotalCount < 4 {
		t.Fatalf("expected at least 4 function definitions, got %d", results.TotalCount)
	}
}

func TestGrepFilesWithMatches(t *testing.T) {
	dir := setupTestDir(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern:    "func",
		Path:       dir,
		OutputMode: "files_with_matches",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(results.Files) < 2 {
		t.Fatalf("expected at least 2 files with 'func', got %d", len(results.Files))
	}
}

func TestGrepCount(t *testing.T) {
	dir := setupTestDir(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern:    "fmt",
		Path:       dir,
		OutputMode: "count",
	})
	if err != nil {
		t.Fatal(err)
	}

	if results.TotalCount < 2 {
		t.Fatalf("expected at least 2 matches for 'fmt', got %d", results.TotalCount)
	}
}

func TestGrepFileType(t *testing.T) {
	dir := setupTestDir(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "test",
		Path:    dir,
		Type:    "json",
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, fm := range results.Files {
		if filepath.Ext(fm.Path) != ".json" {
			t.Errorf("expected only .json files, got %s", fm.Path)
		}
	}
}

func TestGrepGlob(t *testing.T) {
	dir := setupTestDir(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "func",
		Path:    dir,
		Glob:    "*.go",
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, fm := range results.Files {
		if filepath.Ext(fm.Path) != ".go" {
			t.Errorf("expected only .go files, got %s", fm.Path)
		}
	}
}

func TestGrepContextLines(t *testing.T) {
	dir := setupTestDir(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "subtract",
		Path:    filepath.Join(dir, "math.go"),
		Before:  1,
		After:   1,
	})
	if err != nil {
		t.Fatal(err)
	}

	if results.TotalCount != 1 {
		t.Fatalf("expected 1 match, got %d", results.TotalCount)
	}

	m := results.Files[0].Matches[0]
	if len(m.Before) == 0 {
		t.Error("expected before context lines")
	}
	if len(m.After) == 0 {
		t.Error("expected after context lines")
	}
}

func TestGrepMultiline(t *testing.T) {
	dir := setupTestDir(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern:   `func add.*?\n.*?return`,
		Path:      filepath.Join(dir, "math.go"),
		Multiline: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if results.TotalCount == 0 {
		t.Fatal("expected multiline match")
	}
}

func TestGrepSingleFile(t *testing.T) {
	dir := setupTestDir(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "Hello",
		Path:    filepath.Join(dir, "hello.go"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if results.TotalCount != 1 {
		t.Fatalf("expected 1 match, got %d", results.TotalCount)
	}
}

func TestGrepBinaryFileSkipped(t *testing.T) {
	dir := setupTestDir(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "hello",
		Path:    filepath.Join(dir, "binary.dat"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if results.TotalCount != 0 {
		t.Fatalf("expected 0 matches in binary file, got %d", results.TotalCount)
	}
}

func TestGrepHeadLimit(t *testing.T) {
	dir := setupTestDir(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern:   "func",
		Path:      dir,
		HeadLimit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	if results.TotalCount > 2 {
		t.Fatalf("expected at most 2 matches with HeadLimit=2, got %d", results.TotalCount)
	}
}

func TestGrepOffset(t *testing.T) {
	dir := setupTestDir(t)

	// Get all results first
	all, err := Grep(context.Background(), GrepOptions{
		Pattern: "func",
		Path:    dir,
	})
	if err != nil {
		t.Fatal(err)
	}

	if all.TotalCount < 3 {
		t.Skip("need at least 3 matches to test offset")
	}

	// Get results with offset
	offset, err := Grep(context.Background(), GrepOptions{
		Pattern: "func",
		Path:    dir,
		Offset:  1,
	})
	if err != nil {
		t.Fatal(err)
	}

	if offset.TotalCount >= all.TotalCount {
		t.Fatalf("expected fewer matches with offset, got %d vs %d", offset.TotalCount, all.TotalCount)
	}
}

func TestGrepAlternation(t *testing.T) {
	dir := setupTestDir(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "add|subtract|multiply",
		Path:    dir,
	})
	if err != nil {
		t.Fatal(err)
	}

	if results.TotalCount < 3 {
		t.Fatalf("expected at least 3 matches for alternation, got %d", results.TotalCount)
	}
}

func TestGrepRecursiveGlob(t *testing.T) {
	dir := setupTestDir(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "func",
		Path:    dir,
		Glob:    "**/*.go",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Should find matches in both top-level and sub/ directory
	foundNested := false
	for _, fm := range results.Files {
		if filepath.Base(fm.Path) == "nested.go" {
			foundNested = true
		}
	}
	if !foundNested {
		t.Error("expected to find matches in nested.go with **/*.go glob")
	}
}

func TestGrepGitignore(t *testing.T) {
	dir := t.TempDir()

	// Initialize a git repo
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)

	// Create .gitignore that ignores .log files and build/ dir
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\nbuild/\n"), 0o644)

	// Create files
	writeFile(t, dir, "main.go", "func main() { return }")
	writeFile(t, dir, "debug.log", "func debug() { return }")

	os.MkdirAll(filepath.Join(dir, "build"), 0o755)
	writeFile(t, filepath.Join(dir, "build"), "output.go", "func output() { return }")

	os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	writeFile(t, filepath.Join(dir, "src"), "lib.go", "func lib() { return }")

	boolTrue := true
	results, err := Grep(context.Background(), GrepOptions{
		Pattern:          "func",
		Path:             dir,
		RespectGitignore: &boolTrue,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Should find matches in main.go and src/lib.go, but NOT debug.log or build/output.go
	for _, fm := range results.Files {
		base := filepath.Base(fm.Path)
		if base == "debug.log" {
			t.Error("should not match files ignored by .gitignore (debug.log)")
		}
		if base == "output.go" {
			t.Error("should not match files in ignored directory (build/)")
		}
	}

	if results.TotalCount != 2 {
		t.Fatalf("expected 2 matches (main.go + lib.go), got %d", results.TotalCount)
	}
}

func TestGrepNoMatches(t *testing.T) {
	dir := setupTestDir(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "zzz_nonexistent_pattern_zzz",
		Path:    dir,
	})
	if err != nil {
		t.Fatal(err)
	}

	if results.TotalCount != 0 {
		t.Fatalf("expected 0 matches, got %d", results.TotalCount)
	}
}

// ============================================================
// Basic Search Edge Cases (ripgrep: misc.rs, glue.rs)
// ============================================================

func TestGrepEmptyFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "empty.go", "")

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "anything",
		Path:    filepath.Join(dir, "empty.go"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if results.TotalCount != 0 {
		t.Fatalf("expected 0 matches in empty file, got %d", results.TotalCount)
	}
}

func TestGrepFileWithoutTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "noeol.go", "func main() {\n\treturn\n}") // no trailing \n on last line

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "}",
		Path:    filepath.Join(dir, "noeol.go"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if results.TotalCount != 1 {
		t.Fatalf("expected 1 match on last line without newline, got %d", results.TotalCount)
	}
	if results.Files[0].Matches[0].LineNumber != 3 {
		t.Fatalf("expected match on line 3, got line %d", results.Files[0].Matches[0].LineNumber)
	}
}

func TestGrepPatternAtStartAndEnd(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "bounds.txt", "START of file\nmiddle\nend of END\n")

	r1, _ := Grep(context.Background(), GrepOptions{
		Pattern: "START",
		Path:    filepath.Join(dir, "bounds.txt"),
	})
	if r1.TotalCount != 1 || r1.Files[0].Matches[0].LineNumber != 1 {
		t.Error("expected match at start of file, line 1")
	}

	r2, _ := Grep(context.Background(), GrepOptions{
		Pattern: "END",
		Path:    filepath.Join(dir, "bounds.txt"),
	})
	if r2.TotalCount != 1 || r2.Files[0].Matches[0].LineNumber != 3 {
		t.Error("expected match at end of file, line 3")
	}
}

func TestGrepVeryLongLine(t *testing.T) {
	dir := t.TempDir()
	longLine := strings.Repeat("x", 100000) + "NEEDLE" + strings.Repeat("y", 100000)
	writeFile(t, dir, "long.txt", longLine+"\n")

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "NEEDLE",
		Path:    filepath.Join(dir, "long.txt"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if results.TotalCount != 1 {
		t.Fatalf("expected 1 match in very long line, got %d", results.TotalCount)
	}
}

func TestGrepUnicodePattern(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "unicode.txt", "café\nnaïve\nrésumé\nplain\n")

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "é",
		Path:    filepath.Join(dir, "unicode.txt"),
	})
	if err != nil {
		t.Fatal(err)
	}
	// café and résumé contain é, naïve has ï not é
	if results.TotalCount != 2 {
		t.Fatalf("expected 2 matches for Unicode é, got %d", results.TotalCount)
	}
}

// ============================================================
// Regex Edge Cases (ripgrep: regression.rs)
// ============================================================

func TestGrepRegexAnchors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "anchors.txt", "hello world\nworld hello\nhello\n")

	// ^ anchor
	r1, _ := Grep(context.Background(), GrepOptions{
		Pattern: "^hello",
		Path:    filepath.Join(dir, "anchors.txt"),
	})
	if r1.TotalCount != 2 {
		t.Fatalf("expected 2 matches for ^hello, got %d", r1.TotalCount)
	}

	// $ anchor
	r2, _ := Grep(context.Background(), GrepOptions{
		Pattern: "hello$",
		Path:    filepath.Join(dir, "anchors.txt"),
	})
	if r2.TotalCount != 2 {
		t.Fatalf("expected 2 matches for hello$, got %d", r2.TotalCount)
	}
}

func TestGrepRegexCharacterClass(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "charclass.txt", "abc123\ndef456\nghi\n789\n")

	results, _ := Grep(context.Background(), GrepOptions{
		Pattern: "[0-9]+",
		Path:    filepath.Join(dir, "charclass.txt"),
	})
	if results.TotalCount != 3 {
		t.Fatalf("expected 3 matches for [0-9]+, got %d", results.TotalCount)
	}
}

func TestGrepRegexWordBoundary(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "boundary.txt", "cat\ncatalog\nthe cat sat\nconcatenate\n")

	results, _ := Grep(context.Background(), GrepOptions{
		Pattern: `\bcat\b`,
		Path:    filepath.Join(dir, "boundary.txt"),
	})
	if results.TotalCount != 2 {
		t.Fatalf("expected 2 matches for \\bcat\\b, got %d", results.TotalCount)
	}
}

func TestGrepAlternationMixedExact(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "mixed.txt", "foo bar\nbaz123\nqux\nfoo123\n")

	// Pattern with literal prefix + regex
	results, _ := Grep(context.Background(), GrepOptions{
		Pattern: `foo|baz\d+`,
		Path:    filepath.Join(dir, "mixed.txt"),
	})
	if results.TotalCount != 3 {
		t.Fatalf("expected 3 matches for foo|baz\\d+, got %d", results.TotalCount)
	}
}

func TestGrepRegexPatternStartingWithHyphen(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "hyphen.txt", "hello\n-flag\n--verbose\nnormal\n")

	results, _ := Grep(context.Background(), GrepOptions{
		Pattern: "-flag",
		Path:    filepath.Join(dir, "hyphen.txt"),
	})
	if results.TotalCount != 1 {
		t.Fatalf("expected 1 match for -flag, got %d", results.TotalCount)
	}
}

// ============================================================
// Case-Insensitive Edge Cases (ripgrep: feature.rs)
// ============================================================

func TestGrepCaseInsensitiveWithRegex(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "caserx.txt", "Error found\nerror FOUND\nERROR Found\nno match\n")

	results, _ := Grep(context.Background(), GrepOptions{
		Pattern:         `error\s+found`,
		Path:            filepath.Join(dir, "caserx.txt"),
		CaseInsensitive: true,
	})
	if results.TotalCount != 3 {
		t.Fatalf("expected 3 case-insensitive regex matches, got %d", results.TotalCount)
	}
}

func TestGrepCaseInsensitiveUnicode(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "caseu.txt", "Straße\nSTRASSE\nstraße\n")

	// Basic ASCII case folding test
	results, _ := Grep(context.Background(), GrepOptions{
		Pattern:         "stra",
		Path:            filepath.Join(dir, "caseu.txt"),
		CaseInsensitive: true,
	})
	if results.TotalCount < 2 {
		t.Fatalf("expected at least 2 case-insensitive matches, got %d", results.TotalCount)
	}
}

// ============================================================
// Count Mode Edge Cases (ripgrep: misc.rs)
// ============================================================

func TestGrepCountMultipleMatchesSameLine(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "multi.txt", "aaa aaa aaa\nbbb\naaa\n")

	// Count mode counts matching LINES, not occurrences
	results, _ := Grep(context.Background(), GrepOptions{
		Pattern:    "aaa",
		Path:       filepath.Join(dir, "multi.txt"),
		OutputMode: "count",
	})
	if results.TotalCount != 2 {
		t.Fatalf("expected 2 matching lines, got %d", results.TotalCount)
	}
}

func TestGrepCountWithTypeFilter(t *testing.T) {
	dir := setupTestDir(t)

	results, _ := Grep(context.Background(), GrepOptions{
		Pattern:    "func",
		Path:       dir,
		OutputMode: "count",
		Type:       "go",
	})
	if results.TotalCount == 0 {
		t.Fatal("expected matches with type filter")
	}
	for _, fm := range results.Files {
		if filepath.Ext(fm.Path) != ".go" {
			t.Errorf("count mode with type filter returned non-.go file: %s", fm.Path)
		}
	}
}

func TestGrepCountZeroMatchFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "nomatch.txt", "nothing here\n")

	results, _ := Grep(context.Background(), GrepOptions{
		Pattern:    "zzz",
		Path:       filepath.Join(dir, "nomatch.txt"),
		OutputMode: "count",
	})
	if results.TotalCount != 0 {
		t.Fatalf("expected 0, got %d", results.TotalCount)
	}
	if len(results.Files) != 0 {
		t.Fatalf("expected no files in result, got %d", len(results.Files))
	}
}

// ============================================================
// Context Lines Edge Cases (ripgrep: misc.rs, glue.rs)
// ============================================================

func TestGrepContextOverlapping(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "overlap.txt", "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n")

	// Match lines 3 and 5 with context 1 — should not duplicate line 4
	results, _ := Grep(context.Background(), GrepOptions{
		Pattern: "^[35]$",
		Path:    filepath.Join(dir, "overlap.txt"),
		Before:  1,
		After:   1,
	})
	if results.TotalCount != 2 {
		t.Fatalf("expected 2 matches, got %d", results.TotalCount)
	}
	// Collect all context lines to check for duplicates
	var allContext []string
	for _, fm := range results.Files {
		for _, m := range fm.Matches {
			allContext = append(allContext, m.Before...)
			allContext = append(allContext, m.After...)
		}
	}
	seen := map[string]int{}
	for _, line := range allContext {
		seen[line]++
	}
	for line, count := range seen {
		if count > 1 {
			t.Errorf("context line %q appeared %d times (should not duplicate)", line, count)
		}
	}
}

func TestGrepContextAtFileBoundaries(t *testing.T) {
	dir := t.TempDir()
	// No trailing newline to avoid empty last line
	writeFile(t, dir, "boundary.txt", "first\nsecond\nthird")

	// Match first line with before=2 — should truncate gracefully
	r1, _ := Grep(context.Background(), GrepOptions{
		Pattern: "first",
		Path:    filepath.Join(dir, "boundary.txt"),
		Before:  2,
	})
	if r1.TotalCount != 1 {
		t.Fatal("expected 1 match")
	}
	if len(r1.Files[0].Matches[0].Before) != 0 {
		t.Error("before context at start of file should be empty")
	}

	// Match last line with after=2 — should truncate gracefully
	r2, _ := Grep(context.Background(), GrepOptions{
		Pattern: "third",
		Path:    filepath.Join(dir, "boundary.txt"),
		After:   2,
	})
	if r2.TotalCount != 1 {
		t.Fatal("expected 1 match")
	}
	if len(r2.Files[0].Matches[0].After) != 0 {
		t.Error("after context at end of file should be empty")
	}
}

func TestGrepBeforeOnlyContext(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "before.txt", "aaa\nbbb\nccc\nddd\n")

	results, _ := Grep(context.Background(), GrepOptions{
		Pattern: "ccc",
		Path:    filepath.Join(dir, "before.txt"),
		Before:  2,
	})
	if results.TotalCount != 1 {
		t.Fatal("expected 1 match")
	}
	m := results.Files[0].Matches[0]
	if len(m.Before) != 2 {
		t.Fatalf("expected 2 before context lines, got %d", len(m.Before))
	}
	if len(m.After) != 0 {
		t.Fatalf("expected 0 after context lines, got %d", len(m.After))
	}
}

func TestGrepAfterOnlyContext(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "after.txt", "aaa\nbbb\nccc\nddd\n")

	results, _ := Grep(context.Background(), GrepOptions{
		Pattern: "bbb",
		Path:    filepath.Join(dir, "after.txt"),
		After:   2,
	})
	if results.TotalCount != 1 {
		t.Fatal("expected 1 match")
	}
	m := results.Files[0].Matches[0]
	if len(m.Before) != 0 {
		t.Fatalf("expected 0 before context lines, got %d", len(m.Before))
	}
	if len(m.After) != 2 {
		t.Fatalf("expected 2 after context lines, got %d", len(m.After))
	}
}

func TestGrepLargeContext(t *testing.T) {
	dir := t.TempDir()
	// No trailing newline to avoid empty last line
	writeFile(t, dir, "small.txt", "a\nb\nc")

	results, _ := Grep(context.Background(), GrepOptions{
		Pattern: "b",
		Path:    filepath.Join(dir, "small.txt"),
		Before:  100,
		After:   100,
	})
	if results.TotalCount != 1 {
		t.Fatal("expected 1 match")
	}
	m := results.Files[0].Matches[0]
	if len(m.Before) != 1 {
		t.Fatalf("expected 1 before line (file boundary), got %d", len(m.Before))
	}
	if len(m.After) != 1 {
		t.Fatalf("expected 1 after line (file boundary), got %d", len(m.After))
	}
}

// ============================================================
// Multiline Edge Cases (ripgrep: multiline.rs)
// ============================================================

func TestGrepMultilineWithCount(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "ml.txt", "foo\nbar\nfoo\nbar\nbaz\n")

	results, _ := Grep(context.Background(), GrepOptions{
		Pattern:    `foo\nbar`,
		Path:       filepath.Join(dir, "ml.txt"),
		Multiline:  true,
		OutputMode: "count",
	})
	if results.TotalCount != 2 {
		t.Fatalf("expected 2 multiline matches in count mode, got %d", results.TotalCount)
	}
}

func TestGrepMultilineFilesWithMatches(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "foo\nbar\n")
	writeFile(t, dir, "b.txt", "baz\nqux\n")

	results, _ := Grep(context.Background(), GrepOptions{
		Pattern:    `foo\nbar`,
		Path:       dir,
		Multiline:  true,
		OutputMode: "files_with_matches",
	})
	if len(results.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(results.Files))
	}
}

func TestGrepMultilineAnchors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "anchor.txt", "start\nmiddle\nend\n")

	// Pattern that crosses lines using \n explicitly
	results, _ := Grep(context.Background(), GrepOptions{
		Pattern:   `start\n.*\nend`,
		Path:      filepath.Join(dir, "anchor.txt"),
		Multiline: true,
	})
	if results.TotalCount != 1 {
		t.Fatalf("expected 1 multiline match, got %d", results.TotalCount)
	}
}

// ============================================================
// Glob Edge Cases (ripgrep: misc.rs, glob.rs)
// ============================================================

func TestGrepGlobBasenameOnly(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	writeFile(t, dir, "test.go", "func main() {}")
	writeFile(t, filepath.Join(dir, "sub"), "test.go", "func sub() {}")
	writeFile(t, dir, "test.py", "def main(): pass")

	results, _ := Grep(context.Background(), GrepOptions{
		Pattern: "func",
		Path:    dir,
		Glob:    "*.go",
	})
	for _, fm := range results.Files {
		if !strings.HasSuffix(fm.Path, ".go") {
			t.Errorf("glob *.go matched non-.go file: %s", fm.Path)
		}
	}
}

func TestGrepGlobNoMatches(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "test.go", "func main() {}")

	results, _ := Grep(context.Background(), GrepOptions{
		Pattern: "func",
		Path:    dir,
		Glob:    "*.xyz",
	})
	if results.TotalCount != 0 {
		t.Fatalf("expected 0 matches with non-matching glob, got %d", results.TotalCount)
	}
}

func TestGrepGlobBraceExpansion(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	os.MkdirAll(sub, 0o755)
	writeFile(t, dir, "app.ts", "const x = 1")
	writeFile(t, sub, "component.tsx", "const y = 2")
	writeFile(t, dir, "style.css", "body {}")
	writeFile(t, dir, "script.js", "var z = 3")

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: ".",
		Path:    dir,
		Glob:    "**/*.{ts,tsx}",
	})
	if err != nil {
		t.Fatal(err)
	}
	if results.TotalCount == 0 {
		t.Fatal("expected matches for **/*.{ts,tsx} brace expansion, got none")
	}
	for _, fm := range results.Files {
		ext := filepath.Ext(fm.Path)
		if ext != ".ts" && ext != ".tsx" {
			t.Errorf("brace glob matched unexpected file: %s", fm.Path)
		}
	}
}

// ============================================================
// Offset/Limit Combinations (ripgrep: feature.rs)
// ============================================================

func TestGrepOffsetPlusLimit(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "lines.txt", "a1\na2\na3\na4\na5\n")

	results, _ := Grep(context.Background(), GrepOptions{
		Pattern:   "a",
		Path:      filepath.Join(dir, "lines.txt"),
		Offset:    1,
		HeadLimit: 2,
	})
	if results.TotalCount != 2 {
		t.Fatalf("expected 2 matches with offset=1 limit=2, got %d", results.TotalCount)
	}
	if results.Files[0].Matches[0].LineNumber != 2 {
		t.Fatalf("expected first match on line 2 (offset=1), got line %d", results.Files[0].Matches[0].LineNumber)
	}
}

func TestGrepOffsetBeyondTotal(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "small.txt", "match\n")

	results, _ := Grep(context.Background(), GrepOptions{
		Pattern: "match",
		Path:    filepath.Join(dir, "small.txt"),
		Offset:  100,
	})
	if results.TotalCount != 0 {
		t.Fatalf("expected 0 matches with offset beyond total, got %d", results.TotalCount)
	}
}

func TestGrepLimitInCountMode(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "a"), 0o755)
	os.MkdirAll(filepath.Join(dir, "b"), 0o755)
	os.MkdirAll(filepath.Join(dir, "c"), 0o755)
	writeFile(t, filepath.Join(dir, "a"), "f.txt", "match")
	writeFile(t, filepath.Join(dir, "b"), "f.txt", "match")
	writeFile(t, filepath.Join(dir, "c"), "f.txt", "match")

	results, _ := Grep(context.Background(), GrepOptions{
		Pattern:    "match",
		Path:       dir,
		OutputMode: "count",
		HeadLimit:  1,
	})
	if len(results.Files) != 1 {
		t.Fatalf("expected 1 file with limit=1 in count mode, got %d", len(results.Files))
	}
}

func TestGrepLimitInFilesWithMatchesMode(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "a"), 0o755)
	os.MkdirAll(filepath.Join(dir, "b"), 0o755)
	os.MkdirAll(filepath.Join(dir, "c"), 0o755)
	writeFile(t, filepath.Join(dir, "a"), "f.txt", "match")
	writeFile(t, filepath.Join(dir, "b"), "f.txt", "match")
	writeFile(t, filepath.Join(dir, "c"), "f.txt", "match")

	results, _ := Grep(context.Background(), GrepOptions{
		Pattern:    "match",
		Path:       dir,
		OutputMode: "files_with_matches",
		HeadLimit:  2,
	})
	if len(results.Files) > 2 {
		t.Fatalf("expected at most 2 files with limit=2, got %d", len(results.Files))
	}
}

// ============================================================
// Binary Detection Edge Cases (ripgrep: binary.rs)
// ============================================================

func TestGrepBinaryNulAtStart(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "nulstart.txt"), []byte("\x00hello world\n"), 0o644)

	results, _ := Grep(context.Background(), GrepOptions{
		Pattern: "hello",
		Path:    filepath.Join(dir, "nulstart.txt"),
	})
	if results.TotalCount != 0 {
		t.Fatal("expected binary file with NUL at start to be skipped")
	}
}

func TestGrepBinaryNulAt511(t *testing.T) {
	dir := t.TempDir()
	data := make([]byte, 512)
	for i := range data {
		data[i] = 'x'
	}
	data[511] = 0
	data = append(data, []byte("\nhello world\n")...)
	os.WriteFile(filepath.Join(dir, "nul511.txt"), data, 0o644)

	results, _ := Grep(context.Background(), GrepOptions{
		Pattern: "hello",
		Path:    filepath.Join(dir, "nul511.txt"),
	})
	if results.TotalCount != 0 {
		t.Fatal("expected binary file with NUL at pos 511 to be skipped")
	}
}

func TestGrepLargeNonBinaryFile(t *testing.T) {
	dir := t.TempDir()
	// File larger than 512 bytes, no NUL — should not be detected as binary
	data := make([]byte, 1024)
	for i := range data {
		data[i] = 'a'
	}
	copy(data[500:], []byte("NEEDLE"))
	os.WriteFile(filepath.Join(dir, "large.txt"), data, 0o644)

	results, _ := Grep(context.Background(), GrepOptions{
		Pattern: "NEEDLE",
		Path:    filepath.Join(dir, "large.txt"),
	})
	if results.TotalCount != 1 {
		t.Fatalf("expected 1 match in large non-binary file, got %d", results.TotalCount)
	}
}

// ============================================================
// CRLF Handling (ripgrep: feature.rs)
// ============================================================

func TestGrepCRLFLineEndings(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "crlf.txt"), []byte("hello\r\nworld\r\nfoo\r\n"), 0o644)

	results, _ := Grep(context.Background(), GrepOptions{
		Pattern: "world",
		Path:    filepath.Join(dir, "crlf.txt"),
	})
	if results.TotalCount != 1 {
		t.Fatalf("expected 1 match with CRLF, got %d", results.TotalCount)
	}
	// Line content should not include \r
	if strings.Contains(results.Files[0].Matches[0].Line, "\r") {
		t.Error("match line should not contain \\r")
	}
}

func TestGrepMixedLineEndings(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "mixed.txt"), []byte("line1\nline2\r\nline3\nline4\r\n"), 0o644)

	results, _ := Grep(context.Background(), GrepOptions{
		Pattern: "line",
		Path:    filepath.Join(dir, "mixed.txt"),
	})
	if results.TotalCount != 4 {
		t.Fatalf("expected 4 matches with mixed line endings, got %d", results.TotalCount)
	}
	for _, fm := range results.Files {
		for _, m := range fm.Matches {
			if strings.Contains(m.Line, "\r") {
				t.Errorf("line %d contains \\r: %q", m.LineNumber, m.Line)
			}
		}
	}
}

// ============================================================
// Cancellation (context.Context)
// ============================================================

func TestGrepCancellation(t *testing.T) {
	dir := t.TempDir()
	// Create many files
	for i := range 100 {
		writeFile(t, dir, strings.Repeat("file", 1)+strings.Repeat("x", i)+".txt", "match\n")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	results, err := Grep(ctx, GrepOptions{
		Pattern: "match",
		Path:    dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Should get fewer results than total files due to cancellation
	if results.TotalCount >= 100 {
		t.Fatal("expected cancellation to stop search early")
	}
}

// ============================================================
// Column position accuracy
// ============================================================

func TestGrepColumnPosition(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "col.txt", "   NEEDLE   \n")

	results, _ := Grep(context.Background(), GrepOptions{
		Pattern: "NEEDLE",
		Path:    filepath.Join(dir, "col.txt"),
	})
	if results.TotalCount != 1 {
		t.Fatal("expected 1 match")
	}
	if results.Files[0].Matches[0].Column != 3 {
		t.Fatalf("expected column 3 (0-indexed), got %d", results.Files[0].Matches[0].Column)
	}
}

// ============================================================
// Files with matches: early termination
// ============================================================

func TestGrepFilesWithMatchesEarlyTermination(t *testing.T) {
	dir := t.TempDir()
	// File with many matches — should still report exactly 1 count
	var content strings.Builder
	for range 1000 {
		content.WriteString("matchline\n")
	}
	writeFile(t, dir, "many.txt", content.String())

	results, _ := Grep(context.Background(), GrepOptions{
		Pattern:    "matchline",
		Path:       filepath.Join(dir, "many.txt"),
		OutputMode: "files_with_matches",
	})
	if len(results.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(results.Files))
	}
	if results.Files[0].Count != 1 {
		t.Fatalf("expected count 1 for files_with_matches, got %d", results.Files[0].Count)
	}
}

// ============================================================
// Ripgrep SHERLOCK Fixture (ported from tests/hay.rs)
// ============================================================

const SHERLOCK = "For the Doctor Watsons of this world, as opposed to the Sherlock\n" +
	"Holmeses, success in the province of detective work must always\n" +
	"be, to a very large extent, the result of luck. Sherlock Holmes\n" +
	"can extract a clew from a wisp of straw or a flake of cigar ash;\n" +
	"but Doctor Watson has to have it taken out for him and dusted,\n" +
	"and exhibited clearly, with a label attached.\n"

const sherlockCRLF = "For the Doctor Watsons of this world, as opposed to the Sherlock\r\n" +
	"Holmeses, success in the province of detective work must always\r\n" +
	"be, to a very large extent, the result of luck. Sherlock Holmes\r\n" +
	"can extract a clew from a wisp of straw or a flake of cigar ash;\r\n" +
	"but Doctor Watson has to have it taken out for him and dusted,\r\n" +
	"and exhibited clearly, with a label attached.\r\n"

func setupSherlock(t *testing.T) (dir string, filePath string) {
	t.Helper()
	dir = t.TempDir()
	filePath = filepath.Join(dir, "sherlock")
	os.WriteFile(filePath, []byte(SHERLOCK), 0o644)
	return dir, filePath
}

// ============================================================
// Ported from tests/misc.rs: single_file
// ============================================================

func TestSherlockSingleFile(t *testing.T) {
	_, fp := setupSherlock(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "Sherlock",
		Path:    fp,
	})
	if err != nil {
		t.Fatal(err)
	}
	if results.TotalCount != 2 {
		t.Fatalf("expected 2 matches for 'Sherlock', got %d", results.TotalCount)
	}
}

// ============================================================
// Ported from tests/misc.rs: line_numbers
// ============================================================

func TestSherlockLineNumbers(t *testing.T) {
	_, fp := setupSherlock(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "Sherlock",
		Path:    fp,
	})
	if err != nil {
		t.Fatal(err)
	}
	if results.TotalCount != 2 {
		t.Fatalf("expected 2, got %d", results.TotalCount)
	}
	if results.Files[0].Matches[0].LineNumber != 1 {
		t.Errorf("first match should be on line 1, got %d", results.Files[0].Matches[0].LineNumber)
	}
	if results.Files[0].Matches[1].LineNumber != 3 {
		t.Errorf("second match should be on line 3, got %d", results.Files[0].Matches[1].LineNumber)
	}
}

// ============================================================
// Ported from tests/misc.rs: columns
// ============================================================

func TestSherlockColumns(t *testing.T) {
	_, fp := setupSherlock(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "Sherlock",
		Path:    fp,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Line 1: "For the Doctor Watsons of this world, as opposed to the Sherlock"
	// "Sherlock" starts at 0-based column 56
	if results.Files[0].Matches[0].Column != 56 {
		t.Errorf("line 1 column should be 56, got %d", results.Files[0].Matches[0].Column)
	}
	// Line 3: "be, to a very large extent, the result of luck. Sherlock Holmes"
	// "Sherlock" starts at 0-based column 48
	if results.Files[0].Matches[1].Column != 48 {
		t.Errorf("line 3 column should be 48, got %d", results.Files[0].Matches[1].Column)
	}
}

// ============================================================
// Ported from tests/misc.rs: case_insensitive
// ============================================================

func TestSherlockCaseInsensitive(t *testing.T) {
	_, fp := setupSherlock(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern:         "sherlock",
		Path:            fp,
		CaseInsensitive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if results.TotalCount != 2 {
		t.Fatalf("expected 2 case-insensitive matches for 'sherlock', got %d", results.TotalCount)
	}
}

// ============================================================
// Ported from tests/misc.rs: word (via \b)
// ============================================================

func TestSherlockWordBoundary(t *testing.T) {
	_, fp := setupSherlock(t)

	// ripgrep -w "as" -> \bas\b -> matches "as" in "as opposed" but not in "Watsons"
	results, err := Grep(context.Background(), GrepOptions{
		Pattern: `\bas\b`,
		Path:    fp,
	})
	if err != nil {
		t.Fatal(err)
	}
	if results.TotalCount != 1 {
		t.Fatalf("expected 1 match for \\bas\\b, got %d", results.TotalCount)
	}
	if results.Files[0].Matches[0].LineNumber != 1 {
		t.Errorf("expected match on line 1, got %d", results.Files[0].Matches[0].LineNumber)
	}
}

// ============================================================
// Ported from tests/misc.rs: count
// ============================================================

func TestSherlockCount(t *testing.T) {
	_, fp := setupSherlock(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern:    "Sherlock",
		Path:       fp,
		OutputMode: "count",
	})
	if err != nil {
		t.Fatal(err)
	}
	if results.TotalCount != 2 {
		t.Fatalf("expected count 2, got %d", results.TotalCount)
	}
}

// ============================================================
// Ported from tests/misc.rs: count_matches ("the" -> lines)
// Note: ripgrep --count-matches counts occurrences; our count counts lines.
// "the" appears on lines 1, 2, 3 (3 lines).
// ============================================================

func TestSherlockCountThe(t *testing.T) {
	_, fp := setupSherlock(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern:    "the",
		Path:       fp,
		OutputMode: "count",
	})
	if err != nil {
		t.Fatal(err)
	}
	if results.TotalCount != 3 {
		t.Fatalf("expected 3 lines matching 'the', got %d", results.TotalCount)
	}
}

// ============================================================
// Ported from tests/misc.rs: files_with_matches
// ============================================================

func TestSherlockFilesWithMatches(t *testing.T) {
	dir, _ := setupSherlock(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern:    "Sherlock",
		Path:       dir,
		OutputMode: "files_with_matches",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(results.Files))
	}
}

// ============================================================
// Ported from tests/misc.rs: after_context
// ============================================================

func TestSherlockAfterContext(t *testing.T) {
	_, fp := setupSherlock(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "Sherlock",
		Path:    fp,
		After:   1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if results.TotalCount != 2 {
		t.Fatalf("expected 2 matches, got %d", results.TotalCount)
	}
	m0 := results.Files[0].Matches[0]
	m1 := results.Files[0].Matches[1]

	// Match at line 1: after should include line 2
	if len(m0.After) != 1 {
		t.Fatalf("expected 1 after context for match 1, got %d", len(m0.After))
	}
	if !strings.Contains(m0.After[0], "Holmeses") {
		t.Errorf("after context should be line 2, got %q", m0.After[0])
	}

	// Match at line 3: after should include line 4
	if len(m1.After) != 1 {
		t.Fatalf("expected 1 after context for match 2, got %d", len(m1.After))
	}
	if !strings.Contains(m1.After[0], "can extract") {
		t.Errorf("after context should be line 4, got %q", m1.After[0])
	}
}

// ============================================================
// Ported from tests/misc.rs: before_context
// ============================================================

func TestSherlockBeforeContext(t *testing.T) {
	_, fp := setupSherlock(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "Sherlock",
		Path:    fp,
		Before:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if results.TotalCount != 2 {
		t.Fatalf("expected 2 matches, got %d", results.TotalCount)
	}
	m0 := results.Files[0].Matches[0]
	m1 := results.Files[0].Matches[1]

	// Match at line 1: no before context (first line)
	if len(m0.Before) != 0 {
		t.Fatalf("expected 0 before context for match 1 (first line), got %d", len(m0.Before))
	}

	// Match at line 3: before should include line 2
	if len(m1.Before) != 1 {
		t.Fatalf("expected 1 before context for match 2, got %d", len(m1.Before))
	}
	if !strings.Contains(m1.Before[0], "Holmeses") {
		t.Errorf("before context should be line 2, got %q", m1.Before[0])
	}
}

// ============================================================
// Ported from tests/misc.rs: context (C=1 with two non-adjacent matches)
// ============================================================

func TestSherlockContext(t *testing.T) {
	_, fp := setupSherlock(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "world|attached",
		Path:    fp,
		Before:  1,
		After:   1,
	})
	if err != nil {
		t.Fatal(err)
	}
	// "world" on line 1, "attached" on line 6
	if results.TotalCount != 2 {
		t.Fatalf("expected 2 matches, got %d", results.TotalCount)
	}
	m0 := results.Files[0].Matches[0]
	m1 := results.Files[0].Matches[1]

	// Match "world" at line 1: no before, after=line 2
	if len(m0.Before) != 0 {
		t.Errorf("world match: expected 0 before, got %d", len(m0.Before))
	}
	if len(m0.After) != 1 || !strings.Contains(m0.After[0], "Holmeses") {
		t.Errorf("world match: expected after=line 2 (Holmeses), got %v", m0.After)
	}

	// Match "attached" at line 6: before=line 5
	if len(m1.Before) != 1 || !strings.Contains(m1.Before[0], "Doctor Watson") {
		t.Errorf("attached match: expected before=line 5 (Doctor Watson), got %v", m1.Before)
	}
}

// ============================================================
// Ported from tests/misc.rs: file_types
// ============================================================

func TestSherlockFileTypes(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "sherlock"), []byte(SHERLOCK), 0o644)
	os.WriteFile(filepath.Join(dir, "file.py"), []byte("Sherlock\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "file.rs"), []byte("Sherlock\n"), 0o644)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "Sherlock",
		Path:    dir,
		Type:    "rust",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results.Files) != 1 {
		t.Fatalf("expected 1 file with rust type filter, got %d", len(results.Files))
	}
	if filepath.Ext(results.Files[0].Path) != ".rs" {
		t.Errorf("expected .rs file, got %s", results.Files[0].Path)
	}
}

// ============================================================
// Ported from tests/misc.rs: glob
// ============================================================

func TestSherlockGlob(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "file.py"), []byte("Sherlock\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "file.rs"), []byte("Sherlock\n"), 0o644)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "Sherlock",
		Path:    dir,
		Glob:    "*.rs",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results.Files) != 1 {
		t.Fatalf("expected 1 file with glob *.rs, got %d", len(results.Files))
	}
	if filepath.Ext(results.Files[0].Path) != ".rs" {
		t.Errorf("expected .rs file, got %s", results.Files[0].Path)
	}
}

// ============================================================
// Ported from tests/misc.rs: literal (special chars in pattern)
// ============================================================

func TestSherlockLiteral(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "file"), []byte("blib\n()\nblab\n"), 0o644)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: `\(\)`,
		Path:    filepath.Join(dir, "file"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if results.TotalCount != 1 {
		t.Fatalf("expected 1 match for literal (), got %d", results.TotalCount)
	}
	if results.Files[0].Matches[0].Line != "()" {
		t.Errorf("expected line '()', got %q", results.Files[0].Matches[0].Line)
	}
}

// ============================================================
// Ported from tests/misc.rs: ignore_git
// ============================================================

func TestSherlockIgnoreGit(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("sherlock\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "sherlock"), []byte(SHERLOCK), 0o644)

	boolTrue := true
	results, err := Grep(context.Background(), GrepOptions{
		Pattern:          "Sherlock",
		Path:             dir,
		RespectGitignore: &boolTrue,
	})
	if err != nil {
		t.Fatal(err)
	}
	if results.TotalCount != 0 {
		t.Fatalf("expected 0 matches (file ignored by .gitignore), got %d", results.TotalCount)
	}
}

// ============================================================
// Ported from tests/misc.rs: binary_convert (binary detection)
// ============================================================

func TestSherlockBinaryConvert(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "file"), []byte("foo\x00bar\nfoo\x00baz\n"), 0o644)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "foo",
		Path:    filepath.Join(dir, "file"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if results.TotalCount != 0 {
		t.Fatalf("expected 0 matches in binary file, got %d", results.TotalCount)
	}
}

// ============================================================
// Ported from tests/misc.rs: SHERLOCK with CRLF
// ============================================================

func TestSherlockCRLF(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "sherlock")
	os.WriteFile(fp, []byte(sherlockCRLF), 0o644)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "Sherlock",
		Path:    fp,
	})
	if err != nil {
		t.Fatal(err)
	}
	if results.TotalCount != 2 {
		t.Fatalf("expected 2 matches with CRLF, got %d", results.TotalCount)
	}
	for _, m := range results.Files[0].Matches {
		if strings.Contains(m.Line, "\r") {
			t.Errorf("line %d should not contain \\r: %q", m.LineNumber, m.Line)
		}
	}
}

// ============================================================
// Ported from tests/misc.rs: dir (directory search)
// ============================================================

func TestSherlockDirSearch(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "sherlock"), []byte(SHERLOCK), 0o644)
	os.WriteFile(filepath.Join(dir, "other.txt"), []byte("no match here\n"), 0o644)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "Sherlock",
		Path:    dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if results.TotalCount != 2 {
		t.Fatalf("expected 2 matches in dir search, got %d", results.TotalCount)
	}
	if len(results.Files) != 1 {
		t.Fatalf("expected 1 file with matches, got %d", len(results.Files))
	}
}

// ============================================================
// Ported from tests/misc.rs: match line content verification
// ============================================================

func TestSherlockMatchContent(t *testing.T) {
	_, fp := setupSherlock(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "Sherlock",
		Path:    fp,
	})
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{
		"For the Doctor Watsons of this world, as opposed to the Sherlock",
		"be, to a very large extent, the result of luck. Sherlock Holmes",
	}
	for i, m := range results.Files[0].Matches {
		if m.Line != expected[i] {
			t.Errorf("match %d: expected %q, got %q", i, expected[i], m.Line)
		}
	}
}

// ============================================================
// Ported from tests/multiline.rs: overlap1
// ============================================================

func TestMultilineOverlap1(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test"), []byte("xxx\nabc\ndefxxxabc\ndefxxx\nxxx"), 0o644)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern:   `abc\ndef`,
		Path:      filepath.Join(dir, "test"),
		Multiline: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if results.TotalCount != 2 {
		t.Fatalf("overlap1: expected 2 multiline matches, got %d", results.TotalCount)
	}
}

// ============================================================
// Ported from tests/multiline.rs: overlap2
// ============================================================

func TestMultilineOverlap2(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test"), []byte("xxx\nabc\ndefabc\ndefxxx\nxxx"), 0o644)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern:   `abc\ndef`,
		Path:      filepath.Join(dir, "test"),
		Multiline: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if results.TotalCount != 2 {
		t.Fatalf("overlap2: expected 2 multiline matches, got %d", results.TotalCount)
	}
}

// ============================================================
// Ported from tests/multiline.rs: dot_all
// Our multiline mode enables (?s), so . matches newlines.
// ============================================================

func TestMultilineDotAll(t *testing.T) {
	_, fp := setupSherlock(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern:   "of this world.+detective work",
		Path:      fp,
		Multiline: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if results.TotalCount != 1 {
		t.Fatalf("dot_all: expected 1 multiline match spanning lines, got %d", results.TotalCount)
	}
	if results.Files[0].Matches[0].LineNumber != 1 {
		t.Errorf("expected match to start at line 1, got %d", results.Files[0].Matches[0].LineNumber)
	}
}

// ============================================================
// Ported from tests/multiline.rs: multiline count mode
// ============================================================

func TestMultilineCountSherlock(t *testing.T) {
	_, fp := setupSherlock(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern:    "of this world.+detective work",
		Path:       fp,
		Multiline:  true,
		OutputMode: "count",
	})
	if err != nil {
		t.Fatal(err)
	}
	if results.TotalCount != 1 {
		t.Fatalf("expected 1 multiline match in count mode, got %d", results.TotalCount)
	}
}

// ============================================================
// Ported from tests/multiline.rs: multiline files_with_matches
// ============================================================

func TestMultilineFilesWithMatchesSherlock(t *testing.T) {
	dir, _ := setupSherlock(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern:    "of this world.+detective work",
		Path:       dir,
		Multiline:  true,
		OutputMode: "files_with_matches",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(results.Files))
	}
}

// ============================================================
// Ported from tests/glue.rs: empty line matching (^$)
// ============================================================

func TestGrepEmptyLinePattern(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("foo\n\nbar\n\nbaz\n"), 0o644)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "^$",
		Path:    filepath.Join(dir, "test.txt"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if results.TotalCount != 2 {
		t.Fatalf("expected 2 empty lines, got %d", results.TotalCount)
	}
}

// ============================================================
// Ported from tests/binary.rs: binary NUL in second chunk
// ============================================================

func TestBinaryNulInSecondChunk(t *testing.T) {
	data := make([]byte, 600)
	for i := range data {
		data[i] = 'a'
	}
	data[100] = '\n'
	data[200] = '\n'
	data[300] = '\n'
	data[400] = '\n'
	data[500] = 0 // NUL within first 512-byte check region
	data = append(data, []byte("\nmatch here\n")...)

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.dat"), data, 0o644)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "match",
		Path:    filepath.Join(dir, "test.dat"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if results.TotalCount != 0 {
		t.Fatalf("expected 0 matches in binary file (NUL at 500), got %d", results.TotalCount)
	}
}

// ============================================================
// Ported from tests/misc.rs: Doctor Watson
// ============================================================

func TestSherlockDoctorWatson(t *testing.T) {
	_, fp := setupSherlock(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "Doctor Watson",
		Path:    fp,
	})
	if err != nil {
		t.Fatal(err)
	}
	// "Doctor Watson" matches "Doctor Watsons" (line 1) and "Doctor Watson" (line 5)
	if results.TotalCount != 2 {
		t.Fatalf("expected 2 matches for 'Doctor Watson', got %d", results.TotalCount)
	}
}

// ============================================================
// Ported from tests/misc.rs: "Doctor" matches
// ============================================================

func TestSherlockDoctor(t *testing.T) {
	_, fp := setupSherlock(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern: "Doctor",
		Path:    fp,
	})
	if err != nil {
		t.Fatal(err)
	}
	if results.TotalCount != 2 {
		t.Fatalf("expected 2 matches for 'Doctor', got %d", results.TotalCount)
	}
}

// ============================================================
// Ported from tests/misc.rs: "Holmes" count
// ============================================================

func TestSherlockHolmes(t *testing.T) {
	_, fp := setupSherlock(t)

	results, err := Grep(context.Background(), GrepOptions{
		Pattern:    "Holmes",
		Path:       fp,
		OutputMode: "count",
	})
	if err != nil {
		t.Fatal(err)
	}
	// "Holmes" matches "Holmeses" (line 2) and "Sherlock Holmes" (line 3)
	if results.TotalCount != 2 {
		t.Fatalf("expected 2 lines matching 'Holmes', got %d", results.TotalCount)
	}
}
