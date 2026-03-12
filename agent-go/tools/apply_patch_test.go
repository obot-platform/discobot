package tools

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/obot-platform/discobot/agent-go/message"
)

func runApplyPatch(t *testing.T, e *Executor, patch string) (string, bool) {
	t.Helper()
	return runTool(t, e, "apply_patch", map[string]any{"input": patch})
}

func runApplyPatchRaw(t *testing.T, e *Executor, rawInput string) (string, bool) {
	t.Helper()
	result, err := e.Execute(context.Background(), nil, message.ToolCallPart{
		ToolCallID: t.Name(),
		ToolName:   "apply_patch",
		Input:      rawInput,
	})
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	switch v := result.Result.Output.(type) {
	case message.TextOutput:
		return v.Value, true
	case message.ErrorTextOutput:
		return v.Value, false
	default:
		return "", false
	}
}

func TestApplyPatch_MissingInput(t *testing.T) {
	e := New(t.TempDir(), t.TempDir(), t.Name())
	out, ok := runTool(t, e, "apply_patch", map[string]any{})
	if ok {
		t.Fatal("expected error for missing input")
	}
	if !strings.Contains(out, "input is required") {
		t.Fatalf("expected missing input error, got: %q", out)
	}
}

func TestApplyPatch_InvalidPatchBoundaries(t *testing.T) {
	e := New(t.TempDir(), t.TempDir(), t.Name())
	out, ok := runApplyPatch(t, e, "*** Add File: a.txt\n+hello\n")
	if ok {
		t.Fatal("expected parse error for invalid boundaries")
	}
	if !strings.Contains(out, "the first line of the patch must be '*** Begin Patch'") {
		t.Fatalf("unexpected error output: %q", out)
	}
}

func TestApplyPatch_RawInput(t *testing.T) {
	cwd := t.TempDir()
	e := New(cwd, t.TempDir(), t.Name())
	patch := "*** Begin Patch\n*** Add File: raw.txt\n+hello\n*** End Patch"

	out, ok := runApplyPatchRaw(t, e, patch)
	if !ok {
		t.Fatalf("unexpected error: %q", out)
	}
	if !strings.Contains(out, "A raw.txt") {
		t.Fatalf("expected add summary, got: %q", out)
	}
	data, err := os.ReadFile(filepath.Join(cwd, "raw.txt"))
	if err != nil {
		t.Fatalf("failed reading raw file: %v", err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("unexpected raw file content: %q", string(data))
	}
}

func TestApplyPatch_JSONStringInput(t *testing.T) {
	cwd := t.TempDir()
	e := New(cwd, t.TempDir(), t.Name())
	patch := "*** Begin Patch\n*** Add File: quoted.txt\n+hello\n*** End Patch"

	out, ok := runApplyPatchRaw(t, e, strconv.Quote(patch))
	if !ok {
		t.Fatalf("unexpected error: %q", out)
	}
	if !strings.Contains(out, "A quoted.txt") {
		t.Fatalf("expected add summary, got: %q", out)
	}
	data, err := os.ReadFile(filepath.Join(cwd, "quoted.txt"))
	if err != nil {
		t.Fatalf("failed reading quoted file: %v", err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("unexpected quoted file content: %q", string(data))
	}
}

func TestApplyPatch_AllowsAbsolutePaths(t *testing.T) {
	cwd := t.TempDir()
	e := New(cwd, t.TempDir(), t.Name())
	absPath := filepath.Join(cwd, "abs.txt")
	patch := "*** Begin Patch\n*** Add File: " + absPath + "\n+hello\n*** End Patch"

	out, ok := runApplyPatch(t, e, patch)
	if !ok {
		t.Fatalf("unexpected error: %q", out)
	}
	if !strings.Contains(out, "A "+absPath) {
		t.Fatalf("expected add summary, got: %q", out)
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("failed reading file: %v", err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("unexpected content: %q", string(data))
	}
}

func TestApplyPatch_EmptyPatchHasNoChanges(t *testing.T) {
	e := New(t.TempDir(), t.TempDir(), t.Name())
	out, ok := runApplyPatch(t, e, "*** Begin Patch\n*** End Patch")
	if ok {
		t.Fatal("expected no-change patch to fail")
	}
	if !strings.Contains(out, "No files were modified.") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestApplyPatch_AddFile(t *testing.T) {
	cwd := t.TempDir()
	e := New(cwd, t.TempDir(), t.Name())

	patch := `*** Begin Patch
*** Add File: added.txt
+hello
+world
*** End Patch`

	out, ok := runApplyPatch(t, e, patch)
	if !ok {
		t.Fatalf("unexpected error: %s", out)
	}
	if !strings.Contains(out, "A added.txt") {
		t.Fatalf("expected add summary, got: %q", out)
	}

	data, err := os.ReadFile(filepath.Join(cwd, "added.txt"))
	if err != nil {
		t.Fatalf("failed reading added file: %v", err)
	}
	if string(data) != "hello\nworld\n" {
		t.Fatalf("unexpected file contents: %q", string(data))
	}
}

func TestApplyPatch_AddFileCanOverwriteExistingWithRead(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(cwd, "added.txt")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := New(cwd, t.TempDir(), t.Name())
	primeRead(t, e, "added.txt")

	patch := `*** Begin Patch
*** Add File: added.txt
+new
*** End Patch`

	out, ok := runApplyPatch(t, e, patch)
	if !ok {
		t.Fatalf("unexpected error: %s", out)
	}
	if !strings.Contains(out, "A added.txt") {
		t.Fatalf("expected add summary, got: %q", out)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new\n" {
		t.Fatalf("unexpected content: %q", string(data))
	}
}

func TestApplyPatch_AddFileOverwriteStillRequiresRead(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(cwd, "added.txt")
	if err := os.WriteFile(path, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := New(cwd, t.TempDir(), t.Name())

	patch := `*** Begin Patch
*** Add File: added.txt
+new
*** End Patch`

	out, ok := runApplyPatch(t, e, patch)
	if ok {
		t.Fatal("expected read-before-write error")
	}
	if !strings.Contains(out, "read") {
		t.Fatalf("expected read hint in output, got: %q", out)
	}
}

func TestApplyPatch_DeleteFile(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(cwd, "remove.txt")
	if err := os.WriteFile(path, []byte("to be removed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := New(cwd, t.TempDir(), t.Name())
	primeRead(t, e, "remove.txt")

	patch := `*** Begin Patch
*** Delete File: remove.txt
*** End Patch`

	out, ok := runApplyPatch(t, e, patch)
	if !ok {
		t.Fatalf("unexpected error: %s", out)
	}
	if !strings.Contains(out, "D remove.txt") {
		t.Fatalf("expected delete summary, got: %q", out)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file to be deleted, stat err=%v", err)
	}
}

func TestApplyPatch_UpdateFileWithChunks(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(cwd, "file.txt")
	if err := os.WriteFile(path, []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := New(cwd, t.TempDir(), t.Name())
	primeRead(t, e, "file.txt")

	patch := `*** Begin Patch
*** Update File: file.txt
@@
 alpha
-beta
+BETA
@@
 gamma
+delta
*** End of File
*** End Patch`

	out, ok := runApplyPatch(t, e, patch)
	if !ok {
		t.Fatalf("unexpected error: %s", out)
	}
	if !strings.Contains(out, "M file.txt") {
		t.Fatalf("expected modify summary, got: %q", out)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed reading updated file: %v", err)
	}
	if string(data) != "alpha\nBETA\ngamma\ndelta\n" {
		t.Fatalf("unexpected file contents: %q", string(data))
	}
}

func TestApplyPatch_MoveFile(t *testing.T) {
	cwd := t.TempDir()
	src := filepath.Join(cwd, "old.txt")
	if err := os.WriteFile(src, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := New(cwd, t.TempDir(), t.Name())
	primeRead(t, e, "old.txt")

	patch := `*** Begin Patch
*** Update File: old.txt
*** Move to: new/renamed.txt
@@
-before
+after
*** End Patch`

	out, ok := runApplyPatch(t, e, patch)
	if !ok {
		t.Fatalf("unexpected error: %s", out)
	}
	if !strings.Contains(out, "M new/renamed.txt") {
		t.Fatalf("expected moved-file summary, got: %q", out)
	}

	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("expected source file removed, stat err=%v", err)
	}

	dst := filepath.Join(cwd, "new", "renamed.txt")
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("failed reading moved file: %v", err)
	}
	if string(data) != "after\n" {
		t.Fatalf("unexpected moved file contents: %q", string(data))
	}
}

func TestApplyPatch_ContextNotFound(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "file.txt"), []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := New(cwd, t.TempDir(), t.Name())
	primeRead(t, e, "file.txt")

	patch := `*** Begin Patch
*** Update File: file.txt
@@
-not-there
+replacement
*** End Patch`

	out, ok := runApplyPatch(t, e, patch)
	if ok {
		t.Fatal("expected failure when old lines are missing")
	}
	if !strings.Contains(out, "failed to find expected lines") {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestApplyPatch_UnicodeNormalizedMatching(t *testing.T) {
	cwd := t.TempDir()
	line := "import asyncio  # local import – avoids top‑level dep\n"
	if err := os.WriteFile(filepath.Join(cwd, "unicode.py"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	e := New(cwd, t.TempDir(), t.Name())
	primeRead(t, e, "unicode.py")

	patch := `*** Begin Patch
*** Update File: unicode.py
@@
-import asyncio  # local import - avoids top-level dep
+import asyncio  # HELLO
*** End Patch`

	out, ok := runApplyPatch(t, e, patch)
	if !ok {
		t.Fatalf("unexpected error: %s", out)
	}
	if !strings.Contains(out, "M unicode.py") {
		t.Fatalf("expected modify summary, got: %q", out)
	}

	data, err := os.ReadFile(filepath.Join(cwd, "unicode.py"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "import asyncio  # HELLO\n" {
		t.Fatalf("unexpected content: %q", string(data))
	}
}

func TestApplyPatch_ReadBeforeWriteEnforced(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "file.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := New(cwd, t.TempDir(), t.Name())
	patch := `*** Begin Patch
*** Update File: file.txt
@@
-hello
+HELLO
*** End Patch`

	out, ok := runApplyPatch(t, e, patch)
	if ok {
		t.Fatal("expected read-before-write error")
	}
	if !strings.Contains(out, "read") {
		t.Fatalf("expected read hint in output, got: %q", out)
	}
}

func strPtr(s string) *string {
	return &s
}

func TestSeekPatchSequence_ConformanceCases(t *testing.T) {
	t.Run("exact match", func(t *testing.T) {
		lines := []string{"foo", "bar", "baz"}
		pattern := []string{"bar", "baz"}
		if got := seekPatchSequence(lines, pattern, 0, false); got != 1 {
			t.Fatalf("expected 1, got %d", got)
		}
	})

	t.Run("rstrip match", func(t *testing.T) {
		lines := []string{"foo   ", "bar\t\t"}
		pattern := []string{"foo", "bar"}
		if got := seekPatchSequence(lines, pattern, 0, false); got != 0 {
			t.Fatalf("expected 0, got %d", got)
		}
	})

	t.Run("trim match", func(t *testing.T) {
		lines := []string{"    foo   ", "   bar\t"}
		pattern := []string{"foo", "bar"}
		if got := seekPatchSequence(lines, pattern, 0, false); got != 0 {
			t.Fatalf("expected 0, got %d", got)
		}
	})

	t.Run("pattern longer than input", func(t *testing.T) {
		lines := []string{"just one line"}
		pattern := []string{"too", "many", "lines"}
		if got := seekPatchSequence(lines, pattern, 0, false); got != -1 {
			t.Fatalf("expected -1, got %d", got)
		}
	})
}

func TestParseApplyPatch_ConformanceCases(t *testing.T) {
	t.Run("invalid boundaries", func(t *testing.T) {
		_, err := parseApplyPatch("bad")
		if err == nil || !strings.Contains(err.Error(), "the first line of the patch must be '") {
			t.Fatalf("unexpected error: %v", err)
		}

		_, err = parseApplyPatch("*** Begin Patch\nbad")
		if err == nil || !strings.Contains(err.Error(), "the last line of the patch must be '") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("lenient heredoc wrappers", func(t *testing.T) {
		patchText := "*** Begin Patch\n*** Update File: file2.py\n import foo\n+bar\n*** End Patch"
		expected := []patchOperation{{
			kind: patchUpdateFile,
			path: "file2.py",
			chunks: []patchChunk{{
				oldLines: []string{"import foo"},
				newLines: []string{"import foo", "bar"},
			}},
		}}

		for _, wrapped := range []string{
			"<<EOF\n" + patchText + "\nEOF\n",
			"<<'EOF'\n" + patchText + "\nEOF\n",
			"<<\"EOF\"\n" + patchText + "\nEOF\n",
		} {
			ops, err := parseApplyPatch(wrapped)
			if err != nil {
				t.Fatalf("parseApplyPatch returned error for wrapped patch: %v", err)
			}
			if !reflect.DeepEqual(ops, expected) {
				t.Fatalf("unexpected ops: %#v", ops)
			}
		}

		_, err := parseApplyPatch("<<\"EOF'\n" + patchText + "\nEOF\n")
		if err == nil || !strings.Contains(err.Error(), "the first line of the patch must be '") {
			t.Fatalf("unexpected mismatched-quote error: %v", err)
		}

		_, err = parseApplyPatch("<<EOF\n*** Begin Patch\n*** Update File: file2.py\nEOF\n")
		if err == nil || !strings.Contains(err.Error(), "the last line of the patch must be '") {
			t.Fatalf("unexpected missing-closing-marker error: %v", err)
		}
	})

	t.Run("whitespace around boundary markers", func(t *testing.T) {
		ops, err := parseApplyPatch("*** Begin Patch \n*** Add File: foo\n+hi\n *** End Patch")
		if err != nil {
			t.Fatalf("parseApplyPatch returned error: %v", err)
		}
		expected := []patchOperation{{kind: patchAddFile, path: "foo", addLines: []string{"hi"}}}
		if !reflect.DeepEqual(ops, expected) {
			t.Fatalf("unexpected ops: %#v", ops)
		}
	})

	t.Run("update hunk empty", func(t *testing.T) {
		_, err := parseApplyPatch("*** Begin Patch\n*** Update File: test.py\n*** End Patch")
		if err == nil || !strings.Contains(err.Error(), "update file hunk for path 'test.py' is empty") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("empty patch body", func(t *testing.T) {
		ops, err := parseApplyPatch("*** Begin Patch\n*** End Patch")
		if err != nil {
			t.Fatalf("parseApplyPatch returned error: %v", err)
		}
		if len(ops) != 0 {
			t.Fatalf("expected zero operations, got %d", len(ops))
		}
	})

	t.Run("add delete update with move", func(t *testing.T) {
		ops, err := parseApplyPatch("*** Begin Patch\n*** Add File: path/add.py\n+abc\n+def\n*** Delete File: path/delete.py\n*** Update File: path/update.py\n*** Move to: path/update2.py\n@@ def f():\n-    pass\n+    return 123\n*** End Patch")
		if err != nil {
			t.Fatalf("parseApplyPatch returned error: %v", err)
		}

		expected := []patchOperation{
			{kind: patchAddFile, path: "path/add.py", addLines: []string{"abc", "def"}},
			{kind: patchDeleteFile, path: "path/delete.py"},
			{kind: patchUpdateFile, path: "path/update.py", movePath: "path/update2.py", chunks: []patchChunk{{
				changeContext: strPtr("def f():"),
				oldLines:      []string{"    pass"},
				newLines:      []string{"    return 123"},
			}}},
		}

		if !reflect.DeepEqual(ops, expected) {
			t.Fatalf("unexpected ops: %#v", ops)
		}
	})

	t.Run("update followed by add", func(t *testing.T) {
		ops, err := parseApplyPatch("*** Begin Patch\n*** Update File: file.py\n@@\n+line\n*** Add File: other.py\n+content\n*** End Patch")
		if err != nil {
			t.Fatalf("parseApplyPatch returned error: %v", err)
		}

		expected := []patchOperation{
			{kind: patchUpdateFile, path: "file.py", chunks: []patchChunk{{newLines: []string{"line"}}}},
			{kind: patchAddFile, path: "other.py", addLines: []string{"content"}},
		}
		if !reflect.DeepEqual(ops, expected) {
			t.Fatalf("unexpected ops: %#v", ops)
		}
	})

	t.Run("first update chunk can omit @@", func(t *testing.T) {
		ops, err := parseApplyPatch("*** Begin Patch\n*** Update File: file2.py\n import foo\n+bar\n*** End Patch")
		if err != nil {
			t.Fatalf("parseApplyPatch returned error: %v", err)
		}
		expected := []patchOperation{{
			kind: patchUpdateFile,
			path: "file2.py",
			chunks: []patchChunk{{
				oldLines: []string{"import foo"},
				newLines: []string{"import foo", "bar"},
			}},
		}}
		if !reflect.DeepEqual(ops, expected) {
			t.Fatalf("unexpected ops: %#v", ops)
		}
	})
}

func TestParsePatchChunk_ConformanceCases(t *testing.T) {
	_, _, err := parsePatchChunk([]string{"bad"}, false)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "expected update hunk to start") {
		t.Fatalf("unexpected error: %v", err)
	}

	_, _, err = parsePatchChunk([]string{"@@"}, false)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "does not contain any lines") {
		t.Fatalf("unexpected error: %v", err)
	}

	_, _, err = parsePatchChunk([]string{"@@", "bad"}, false)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "unexpected line found in update hunk") {
		t.Fatalf("unexpected error: %v", err)
	}

	_, _, err = parsePatchChunk([]string{"@@", "*** End of File"}, false)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "does not contain any lines") {
		t.Fatalf("unexpected error: %v", err)
	}

	chunk, consumed, err := parsePatchChunk([]string{"@@ change_context", "", " context", "-remove", "+add", " context2", "*** End Patch"}, false)
	if err != nil {
		t.Fatalf("parsePatchChunk returned error: %v", err)
	}
	expectedChunk := patchChunk{
		changeContext: strPtr("change_context"),
		oldLines:      []string{"", "context", "remove", "context2"},
		newLines:      []string{"", "context", "add", "context2"},
	}
	if !reflect.DeepEqual(chunk, expectedChunk) {
		t.Fatalf("unexpected chunk: %#v", chunk)
	}
	if consumed != 6 {
		t.Fatalf("expected consumed=6, got %d", consumed)
	}

	chunk, consumed, err = parsePatchChunk([]string{"@@", "+line", "*** End of File"}, false)
	if err != nil {
		t.Fatalf("parsePatchChunk returned error: %v", err)
	}
	expectedChunk = patchChunk{newLines: []string{"line"}, isEndOfFile: true}
	if !reflect.DeepEqual(chunk, expectedChunk) {
		t.Fatalf("unexpected chunk: %#v", chunk)
	}
	if consumed != 3 {
		t.Fatalf("expected consumed=3, got %d", consumed)
	}
}

func TestApplyPatch_MultipleUpdateChunksApplyToSingleFile(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(cwd, "multi.txt")
	if err := os.WriteFile(path, []byte("foo\nbar\nbaz\nqux\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := New(cwd, t.TempDir(), t.Name())
	primeRead(t, e, "multi.txt")

	patch := `*** Begin Patch
*** Update File: multi.txt
@@
 foo
-bar
+BAR
@@
 baz
-qux
+QUX
*** End Patch`

	out, ok := runApplyPatch(t, e, patch)
	if !ok {
		t.Fatalf("unexpected error: %s", out)
	}
	if !strings.Contains(out, "M multi.txt") {
		t.Fatalf("expected modify summary, got: %q", out)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "foo\nBAR\nbaz\nQUX\n" {
		t.Fatalf("unexpected content: %q", string(data))
	}
}

func TestApplyPatch_UpdateFileHunkInterleavedChanges(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(cwd, "interleaved.txt")
	if err := os.WriteFile(path, []byte("a\nb\nc\nd\ne\nf\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := New(cwd, t.TempDir(), t.Name())
	primeRead(t, e, "interleaved.txt")

	patch := `*** Begin Patch
*** Update File: interleaved.txt
@@
 a
-b
+B
@@
 c
 d
-e
+E
@@
 f
+g
*** End of File
*** End Patch`

	out, ok := runApplyPatch(t, e, patch)
	if !ok {
		t.Fatalf("unexpected error: %s", out)
	}
	if !strings.Contains(out, "M interleaved.txt") {
		t.Fatalf("expected modify summary, got: %q", out)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "a\nB\nc\nd\nE\nf\ng\n" {
		t.Fatalf("unexpected content: %q", string(data))
	}
}

func TestApplyPatch_PureAdditionChunkFollowedByRemoval(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(cwd, "panic.txt")
	if err := os.WriteFile(path, []byte("line1\nline2\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	e := New(cwd, t.TempDir(), t.Name())
	primeRead(t, e, "panic.txt")

	patch := `*** Begin Patch
*** Update File: panic.txt
@@
+after-context
+second-line
@@
 line1
-line2
-line3
+line2-replacement
*** End Patch`

	out, ok := runApplyPatch(t, e, patch)
	if !ok {
		t.Fatalf("unexpected error: %s", out)
	}
	if !strings.Contains(out, "M panic.txt") {
		t.Fatalf("expected modify summary, got: %q", out)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "line1\nline2-replacement\nafter-context\nsecond-line\n" {
		t.Fatalf("unexpected content: %q", string(data))
	}
}
