package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestClassifyLocalWorkspacePath verifies workspace path classification.
func TestClassifyLocalWorkspacePath(t *testing.T) {
	t.Parallel()

	t.Run("nonexistent path with valid parent is new", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		newPath := filepath.Join(tmpDir, "new-project")

		_, classification, err := classifyLocalWorkspacePath(newPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if classification != LocalWorkspaceClassificationNew {
			t.Errorf("expected classification %q, got %q", LocalWorkspaceClassificationNew, classification)
		}
	})

	t.Run("empty directory is empty", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		emptyDir := filepath.Join(tmpDir, "empty")
		if err := os.Mkdir(emptyDir, 0755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}

		_, classification, err := classifyLocalWorkspacePath(emptyDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if classification != LocalWorkspaceClassificationEmpty {
			t.Errorf("expected classification %q, got %q", LocalWorkspaceClassificationEmpty, classification)
		}
	})

	t.Run("path with no existing parent is invalid", func(t *testing.T) {
		t.Parallel()
		path := "/nonexistent-root-abc123/nested/path"

		_, classification, err := classifyLocalWorkspacePath(path)
		if err == nil {
			t.Fatal("expected error for deeply nonexistent path")
		}
		if classification != LocalWorkspaceClassificationInvalid {
			t.Errorf("expected classification %q, got %q", LocalWorkspaceClassificationInvalid, classification)
		}
	})

	t.Run("file path is invalid", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "somefile.txt")
		if err := os.WriteFile(filePath, []byte("hello"), 0644); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}

		_, classification, err := classifyLocalWorkspacePath(filePath)
		if err == nil {
			t.Fatal("expected error for file path")
		}
		if classification != LocalWorkspaceClassificationInvalid {
			t.Errorf("expected classification %q, got %q", LocalWorkspaceClassificationInvalid, classification)
		}
		if !strings.Contains(err.Error(), "not a directory") {
			t.Errorf("expected 'not a directory' error, got %q", err.Error())
		}
	})

	t.Run("non-empty non-git directory is invalid", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		noGitDir := filepath.Join(tmpDir, "not-git")
		if err := os.Mkdir(noGitDir, 0755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(noGitDir, "file.txt"), []byte("hello"), 0644); err != nil {
			t.Fatalf("failed to create file: %v", err)
		}

		_, classification, err := classifyLocalWorkspacePath(noGitDir)
		if err == nil {
			t.Fatal("expected error for non-git directory")
		}
		if classification != LocalWorkspaceClassificationInvalid {
			t.Errorf("expected classification %q, got %q", LocalWorkspaceClassificationInvalid, classification)
		}
	})

	t.Run("returns expanded path", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		newPath := filepath.Join(tmpDir, "new-workspace")

		gotPath, _, _ := classifyLocalWorkspacePath(newPath)
		if gotPath == "" {
			t.Error("expected non-empty returned path")
		}
	})
}
