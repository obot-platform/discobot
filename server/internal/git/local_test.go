package git

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// createTestRepo creates a test git repository with initial content
func createTestRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")

	// Create initial files
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test Repo\n"), 0644); err != nil {
		t.Fatalf("Failed to create README.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0644); err != nil {
		t.Fatalf("Failed to create main.go: %v", err)
	}

	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "Initial commit")

	return dir
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = cleanGitEnv()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\nOutput: %s", args, err, output)
	}
	return string(output)
}

func runGitBytes(t *testing.T, dir string, args ...string) []byte {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = cleanGitEnv()
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v failed: %v", args, err)
	}
	return output
}

func buildCommitReplayBundle(t *testing.T, dir, parent string) []byte {
	t.Helper()

	commitsOutput := strings.TrimSpace(runGit(t, dir, "rev-list", "--reverse", parent+"..HEAD"))
	if commitsOutput == "" {
		payload, err := json.Marshal(commitReplayBundle{Version: 1, Commits: []commitReplayEntry{}})
		if err != nil {
			t.Fatalf("failed to marshal empty bundle: %v", err)
		}
		return payload
	}

	bundle := commitReplayBundle{Version: 1}
	previous := parent
	for _, sha := range strings.Split(commitsOutput, "\n") {
		sha = strings.TrimSpace(sha)
		if sha == "" {
			continue
		}
		bundle.Commits = append(bundle.Commits, buildCommitReplayEntry(t, dir, previous, sha))
		previous = sha
	}

	payload, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("failed to marshal commit replay bundle: %v", err)
	}
	return payload
}

func buildCommitReplayEntry(t *testing.T, dir, parent, sha string) commitReplayEntry {
	t.Helper()

	meta := strings.SplitN(string(runGitBytes(t, dir, "show", "-s", "--format=%an%x00%ae%x00%aI%x00%cn%x00%ce%x00%cI%x00%B", sha)), "\x00", 7)
	if len(meta) != 7 {
		t.Fatalf("unexpected commit metadata for %s", sha)
	}
	authorDate, err := time.Parse(time.RFC3339, strings.TrimSpace(meta[2]))
	if err != nil {
		t.Fatalf("failed to parse author date: %v", err)
	}
	committerDate, err := time.Parse(time.RFC3339, strings.TrimSpace(meta[5]))
	if err != nil {
		t.Fatalf("failed to parse committer date: %v", err)
	}

	entry := commitReplayEntry{
		SHA:            sha,
		Message:        strings.TrimRight(meta[6], "\n"),
		AuthorName:     meta[0],
		AuthorEmail:    meta[1],
		AuthorDate:     authorDate,
		CommitterName:  meta[3],
		CommitterEmail: meta[4],
		CommitterDate:  &committerDate,
	}

	changed := runGitBytes(t, dir, "diff-tree", "--no-commit-id", "--find-renames", "-r", "--name-status", "-z", parent, sha)
	tokens := bytesToTokens(changed)
	for i := 0; i < len(tokens); {
		statusToken := tokens[i]
		i++
		switch {
		case strings.HasPrefix(statusToken, "R"):
			oldPath := tokens[i]
			newPath := tokens[i+1]
			i += 2
			oldContent := runGitBytes(t, dir, "show", parent+":"+oldPath)
			newContent := runGitBytes(t, dir, "show", sha+":"+newPath)
			entry.Changes = append(entry.Changes, commitFileChange{Path: newPath, OldPath: oldPath, Status: "renamed", PreviousContent: oldContent, Content: newContent})
		case statusToken == "A":
			path := tokens[i]
			i++
			entry.Changes = append(entry.Changes, commitFileChange{Path: path, Status: "added", Content: runGitBytes(t, dir, "show", sha+":"+path)})
		case statusToken == "M":
			path := tokens[i]
			i++
			entry.Changes = append(entry.Changes, commitFileChange{Path: path, Status: "modified", PreviousContent: runGitBytes(t, dir, "show", parent+":"+path), Content: runGitBytes(t, dir, "show", sha+":"+path)})
		case statusToken == "D":
			path := tokens[i]
			i++
			entry.Changes = append(entry.Changes, commitFileChange{Path: path, Status: "deleted", PreviousContent: runGitBytes(t, dir, "show", parent+":"+path)})
		default:
			t.Fatalf("unsupported name-status token %q", statusToken)
		}
	}

	return entry
}

func bytesToTokens(data []byte) []string {
	parts := strings.Split(string(bytes.TrimRight(data, "\x00")), "\x00")
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			filtered = append(filtered, part)
		}
	}
	return filtered
}

func TestNewLocalProvider(t *testing.T) {
	t.Run("creates base directory", func(t *testing.T) {
		baseDir := filepath.Join(t.TempDir(), "newdir")

		provider, err := NewLocalProvider(baseDir)
		if err != nil {
			t.Fatalf("NewLocalProvider failed: %v", err)
		}

		if provider.baseDir != baseDir {
			t.Errorf("Expected baseDir %s, got %s", baseDir, provider.baseDir)
		}

		// Check directory exists
		if _, err := os.Stat(baseDir); os.IsNotExist(err) {
			t.Error("Base directory was not created")
		}
	})

	t.Run("uses existing directory", func(t *testing.T) {
		baseDir := t.TempDir()

		provider, err := NewLocalProvider(baseDir)
		if err != nil {
			t.Fatalf("NewLocalProvider failed: %v", err)
		}

		if provider.baseDir != baseDir {
			t.Errorf("Expected baseDir %s, got %s", baseDir, provider.baseDir)
		}
	})
}

func TestEnsureWorkspace(t *testing.T) {
	ctx := context.Background()

	t.Run("clones local repository", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		workDir, commit, err := provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")
		if err != nil {
			t.Fatalf("EnsureWorkspace failed: %v", err)
		}

		if workDir == "" {
			t.Error("Expected workDir to be set")
		}

		if commit == "" {
			t.Error("Expected commit SHA to be returned")
		}

		// Verify directory structure
		expectedPath := filepath.Join(baseDir, "project1", "workspaces", "ws1")
		if workDir != expectedPath {
			t.Errorf("Expected workDir %s, got %s", expectedPath, workDir)
		}

		// Verify it's a git repo
		if _, err := os.Stat(filepath.Join(workDir, ".git")); os.IsNotExist(err) {
			t.Error("Working directory is not a git repo")
		}
	})

	t.Run("returns existing workspace from index", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		workDir1, _, _ := provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")
		workDir2, _, err := provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")

		if err != nil {
			t.Fatalf("Second EnsureWorkspace failed: %v", err)
		}

		if workDir1 != workDir2 {
			t.Errorf("Expected same workDir, got %s and %s", workDir1, workDir2)
		}
	})

	t.Run("recovers workspace from disk", func(t *testing.T) {
		baseDir := t.TempDir()
		provider1, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		workDir1, _, _ := provider1.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")

		// Create new provider (simulating restart)
		provider2, _ := NewLocalProvider(baseDir)
		workDir2, _, err := provider2.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")

		if err != nil {
			t.Fatalf("Recovery failed: %v", err)
		}

		if workDir1 != workDir2 {
			t.Errorf("Expected same workDir after recovery, got %s and %s", workDir1, workDir2)
		}
	})

	t.Run("checks out specific ref", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		// Create a branch in the source repo
		runGit(t, sourceRepo, "branch", "feature")

		workDir, _, err := provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "feature")
		if err != nil {
			t.Fatalf("EnsureWorkspace with ref failed: %v", err)
		}

		// Verify we're on the feature branch
		status, _ := provider.Status(ctx, "ws1")
		if status.Branch != "feature" {
			t.Errorf("Expected branch feature, got %s", status.Branch)
		}

		_ = workDir // suppress unused warning
	})

	t.Run("fails for non-git directory", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		notARepo := t.TempDir()

		_, _, err := provider.EnsureWorkspace(ctx, "project1", "ws1", notARepo, "")
		if err == nil {
			t.Error("Expected error for non-git directory")
		}
	})

	t.Run("stores workspaces under project directory", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		// Use different workspace IDs (as would happen with UUIDs in production)
		workDir1, _, _ := provider.EnsureWorkspace(ctx, "project1", "ws-uuid-1", sourceRepo, "")
		workDir2, _, _ := provider.EnsureWorkspace(ctx, "project2", "ws-uuid-2", sourceRepo, "")

		if workDir1 == workDir2 {
			t.Error("Expected different workDirs for different workspaces")
		}

		// Verify directory structure - each workspace goes under its project
		if !strings.HasPrefix(workDir1, filepath.Join(baseDir, "project1")) {
			t.Errorf("workDir1 should be under project1, got %s", workDir1)
		}
		if !strings.HasPrefix(workDir2, filepath.Join(baseDir, "project2")) {
			t.Errorf("workDir2 should be under project2, got %s", workDir2)
		}
	})
}

func TestFetch(t *testing.T) {
	ctx := context.Background()

	t.Run("fetches from origin", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")

		err := provider.Fetch(ctx, "ws1")
		if err != nil {
			t.Fatalf("Fetch failed: %v", err)
		}
	})

	t.Run("fails for unknown workspace", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)

		err := provider.Fetch(ctx, "nonexistent")
		if err == nil {
			t.Error("Expected error for unknown workspace")
		}
	})
}

func TestCheckout(t *testing.T) {
	ctx := context.Background()

	t.Run("checks out branch", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)
		runGit(t, sourceRepo, "branch", "feature")

		provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")

		err := provider.Checkout(ctx, "ws1", "feature")
		if err != nil {
			t.Fatalf("Checkout failed: %v", err)
		}

		status, _ := provider.Status(ctx, "ws1")
		if status.Branch != "feature" {
			t.Errorf("Expected branch feature, got %s", status.Branch)
		}
	})

	t.Run("fails for invalid ref", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")

		err := provider.Checkout(ctx, "ws1", "nonexistent-branch")
		if err == nil {
			t.Error("Expected error for invalid ref")
		}
	})
}

func TestStatus(t *testing.T) {
	ctx := context.Background()

	t.Run("returns clean status", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")

		status, err := provider.Status(ctx, "ws1")
		if err != nil {
			t.Fatalf("Status failed: %v", err)
		}

		if !status.IsClean {
			t.Error("Expected clean status")
		}

		if status.Commit == "" {
			t.Error("Expected commit SHA")
		}

		if status.Branch != "master" && status.Branch != "main" {
			t.Errorf("Expected branch master or main, got %s", status.Branch)
		}
	})

	t.Run("detects untracked files", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		workDir, _, _ := provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")

		// Create untracked file
		os.WriteFile(filepath.Join(workDir, "new.txt"), []byte("new"), 0644)

		status, _ := provider.Status(ctx, "ws1")

		if status.IsClean {
			t.Error("Expected dirty status")
		}

		if len(status.Untracked) == 0 {
			t.Error("Expected untracked files")
		}
	})

	t.Run("detects staged changes", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		workDir, _, _ := provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")

		// Create and stage file
		os.WriteFile(filepath.Join(workDir, "new.txt"), []byte("new"), 0644)
		provider.Stage(ctx, "ws1", []string{"new.txt"})

		status, _ := provider.Status(ctx, "ws1")

		if len(status.Staged) == 0 {
			t.Error("Expected staged files")
		}
	})

	t.Run("detects unstaged changes", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		workDir, _, _ := provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")

		// Modify existing file
		os.WriteFile(filepath.Join(workDir, "README.md"), []byte("modified"), 0644)

		status, _ := provider.Status(ctx, "ws1")

		if len(status.Unstaged) == 0 {
			t.Error("Expected unstaged files")
		}
	})
}

func TestDiff(t *testing.T) {
	ctx := context.Background()

	t.Run("returns empty diff for clean repo", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")

		diffs, err := provider.Diff(ctx, "ws1", DiffOptions{})
		if err != nil {
			t.Fatalf("Diff failed: %v", err)
		}

		if len(diffs) != 0 {
			t.Errorf("Expected no diffs, got %d", len(diffs))
		}
	})

	t.Run("returns diff for modified file", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		workDir, _, _ := provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")

		// Modify file
		os.WriteFile(filepath.Join(workDir, "README.md"), []byte("# Modified\n"), 0644)

		diffs, err := provider.Diff(ctx, "ws1", DiffOptions{})
		if err != nil {
			t.Fatalf("Diff failed: %v", err)
		}

		if len(diffs) != 1 {
			t.Fatalf("Expected 1 diff, got %d", len(diffs))
		}

		if diffs[0].Path != "README.md" {
			t.Errorf("Expected diff for README.md, got %s", diffs[0].Path)
		}

		if diffs[0].Status != "modified" {
			t.Errorf("Expected status modified, got %s", diffs[0].Status)
		}
	})

	t.Run("returns staged diff", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		workDir, _, _ := provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")

		// Modify and stage
		os.WriteFile(filepath.Join(workDir, "README.md"), []byte("# Modified\n"), 0644)
		provider.Stage(ctx, "ws1", []string{"README.md"})

		diffs, err := provider.Diff(ctx, "ws1", DiffOptions{Staged: true})
		if err != nil {
			t.Fatalf("Diff failed: %v", err)
		}

		if len(diffs) != 1 {
			t.Fatalf("Expected 1 staged diff, got %d", len(diffs))
		}
	})
}

func TestBranches(t *testing.T) {
	ctx := context.Background()

	t.Run("lists branches", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)
		runGit(t, sourceRepo, "branch", "feature")
		runGit(t, sourceRepo, "branch", "develop")

		provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")

		branches, err := provider.Branches(ctx, "ws1")
		if err != nil {
			t.Fatalf("Branches failed: %v", err)
		}

		if len(branches) < 3 {
			t.Errorf("Expected at least 3 branches, got %d", len(branches))
		}

		// Check for current branch
		hasCurrent := false
		for _, b := range branches {
			if b.IsCurrent {
				hasCurrent = true
				break
			}
		}

		if !hasCurrent {
			t.Error("Expected one branch to be current")
		}
	})
}

func TestFileTree(t *testing.T) {
	ctx := context.Background()

	t.Run("lists files at HEAD", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")

		files, err := provider.FileTree(ctx, "ws1", "")
		if err != nil {
			t.Fatalf("FileTree failed: %v", err)
		}

		if len(files) < 2 {
			t.Errorf("Expected at least 2 files, got %d", len(files))
		}

		hasReadme := false
		hasMain := false
		for _, f := range files {
			if f.Path == "README.md" {
				hasReadme = true
			}
			if f.Path == "main.go" {
				hasMain = true
			}
		}

		if !hasReadme || !hasMain {
			t.Error("Expected README.md and main.go in file tree")
		}
	})
}

func TestReadFile(t *testing.T) {
	ctx := context.Background()

	t.Run("reads file from working tree", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")

		content, err := provider.ReadFile(ctx, "ws1", "", "README.md")
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}

		if string(content) != "# Test Repo\n" {
			t.Errorf("Unexpected content: %s", content)
		}
	})

	t.Run("reads file from specific ref", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")

		content, err := provider.ReadFile(ctx, "ws1", "HEAD", "README.md")
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}

		if string(content) != "# Test Repo\n" {
			t.Errorf("Unexpected content: %s", content)
		}
	})

	t.Run("fails for nonexistent file", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")

		_, err := provider.ReadFile(ctx, "ws1", "", "nonexistent.txt")
		if err == nil {
			t.Error("Expected error for nonexistent file")
		}
	})
}

func TestWriteFile(t *testing.T) {
	ctx := context.Background()

	t.Run("writes new file", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")

		err := provider.WriteFile(ctx, "ws1", "new.txt", []byte("hello"))
		if err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		content, _ := provider.ReadFile(ctx, "ws1", "", "new.txt")
		if string(content) != "hello" {
			t.Errorf("Unexpected content: %s", content)
		}
	})

	t.Run("creates nested directories", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")

		err := provider.WriteFile(ctx, "ws1", "deep/nested/file.txt", []byte("deep"))
		if err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		content, _ := provider.ReadFile(ctx, "ws1", "", "deep/nested/file.txt")
		if string(content) != "deep" {
			t.Errorf("Unexpected content: %s", content)
		}
	})
}

func TestStage(t *testing.T) {
	ctx := context.Background()

	t.Run("stages file", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")
		provider.WriteFile(ctx, "ws1", "new.txt", []byte("hello"))

		err := provider.Stage(ctx, "ws1", []string{"new.txt"})
		if err != nil {
			t.Fatalf("Stage failed: %v", err)
		}

		status, _ := provider.Status(ctx, "ws1")
		if len(status.Staged) == 0 {
			t.Error("Expected staged files after staging")
		}
	})

	t.Run("stages multiple files", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")
		provider.WriteFile(ctx, "ws1", "file1.txt", []byte("1"))
		provider.WriteFile(ctx, "ws1", "file2.txt", []byte("2"))

		err := provider.Stage(ctx, "ws1", []string{"file1.txt", "file2.txt"})
		if err != nil {
			t.Fatalf("Stage failed: %v", err)
		}

		status, _ := provider.Status(ctx, "ws1")
		if len(status.Staged) != 2 {
			t.Errorf("Expected 2 staged files, got %d", len(status.Staged))
		}
	})

	t.Run("stages all with dot", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")
		provider.WriteFile(ctx, "ws1", "file1.txt", []byte("1"))
		provider.WriteFile(ctx, "ws1", "file2.txt", []byte("2"))

		err := provider.Stage(ctx, "ws1", []string{"."})
		if err != nil {
			t.Fatalf("Stage failed: %v", err)
		}

		status, _ := provider.Status(ctx, "ws1")
		if len(status.Staged) != 2 {
			t.Errorf("Expected 2 staged files, got %d", len(status.Staged))
		}
	})
}

func TestCommit(t *testing.T) {
	ctx := context.Background()

	t.Run("creates commit", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		workDir, _, _ := provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")
		// Configure git user for the workspace to avoid "Committer identity unknown" error
		runGit(t, workDir, "config", "user.email", "committer@example.com")
		runGit(t, workDir, "config", "user.name", "Test Committer")

		provider.WriteFile(ctx, "ws1", "new.txt", []byte("hello"))
		provider.Stage(ctx, "ws1", []string{"new.txt"})

		commit, err := provider.Commit(ctx, "ws1", "Add new file", "Test Author", "test@example.com")
		if err != nil {
			t.Fatalf("Commit failed: %v", err)
		}

		if commit.SHA == "" {
			t.Error("Expected commit SHA")
		}

		if commit.Message != "Add new file" {
			t.Errorf("Expected message 'Add new file', got %s", commit.Message)
		}

		if commit.Author != "Test Author" {
			t.Errorf("Expected author 'Test Author', got %s", commit.Author)
		}
	})

	t.Run("fails with no staged changes", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")

		_, err := provider.Commit(ctx, "ws1", "Empty commit", "", "")
		if err == nil {
			t.Error("Expected error for commit with no staged changes")
		}
	})
}

func TestLog(t *testing.T) {
	ctx := context.Background()

	t.Run("returns commit history", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")

		commits, err := provider.Log(ctx, "ws1", LogOptions{})
		if err != nil {
			t.Fatalf("Log failed: %v", err)
		}

		if len(commits) == 0 {
			t.Error("Expected at least one commit")
		}

		if commits[0].Message != "Initial commit" {
			t.Errorf("Expected 'Initial commit', got %s", commits[0].Message)
		}
	})

	t.Run("respects limit", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		// Add more commits
		for i := 0; i < 5; i++ {
			runGit(t, sourceRepo, "commit", "--allow-empty", "-m", "Commit")
		}

		provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")

		commits, _ := provider.Log(ctx, "ws1", LogOptions{Limit: 3})

		if len(commits) != 3 {
			t.Errorf("Expected 3 commits, got %d", len(commits))
		}
	})
}

func TestGetWorkDir(t *testing.T) {
	ctx := context.Background()

	t.Run("returns workDir for known workspace", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		workDir, _, _ := provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")

		got := provider.GetWorkDir(ctx, "ws1")
		if got != workDir {
			t.Errorf("Expected %s, got %s", workDir, got)
		}
	})

	t.Run("returns empty for unknown workspace", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)

		got := provider.GetWorkDir(ctx, "unknown")
		if got != "" {
			t.Errorf("Expected empty string, got %s", got)
		}
	})
}

func TestRemoveWorkspace(t *testing.T) {
	ctx := context.Background()

	t.Run("removes workspace", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		workDir, _, _ := provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")

		err := provider.RemoveWorkspace(ctx, "ws1")
		if err != nil {
			t.Fatalf("RemoveWorkspace failed: %v", err)
		}

		// Verify directory is gone
		if _, err := os.Stat(workDir); !os.IsNotExist(err) {
			t.Error("Expected workspace directory to be removed")
		}

		// Verify GetWorkDir returns empty
		if provider.GetWorkDir(ctx, "ws1") != "" {
			t.Error("Expected GetWorkDir to return empty after removal")
		}
	})

	t.Run("succeeds for unknown workspace", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)

		err := provider.RemoveWorkspace(ctx, "unknown")
		if err != nil {
			t.Errorf("RemoveWorkspace for unknown workspace should not fail: %v", err)
		}
	})
}

func TestIsGitURL(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"https://github.com/owner/repo.git", true},
		{"http://github.com/owner/repo", true},
		{"git@github.com:owner/repo.git", true},
		{"git://github.com/owner/repo.git", true},
		{"ssh://git@github.com/owner/repo.git", true},
		{"/local/path/to/repo", false},
		{"./relative/path", false},
		{"C:\\Windows\\Path", false},
		{"", false},
		{"abc", false},
		{"some-repo.git", true}, // ends with .git
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := IsGitURL(tt.input)
			if got != tt.expected {
				t.Errorf("IsGitURL(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestApplyReplayBundle(t *testing.T) {
	ctx := context.Background()

	t.Run("applies single commit patch", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		workDir, _, _ := provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")
		// Configure git user for the workspace to avoid "Committer identity unknown" error
		runGit(t, workDir, "config", "user.email", "committer@example.com")
		runGit(t, workDir, "config", "user.name", "Test Committer")

		// Get initial commit SHA
		initialCommit := strings.TrimSpace(runGit(t, workDir, "rev-parse", "HEAD"))

		// Create a patch (simulate what agent-api would return)
		// Create a commit in a separate repo and export it as a patch
		patchRepo := t.TempDir()
		runGit(t, patchRepo, "init")
		runGit(t, patchRepo, "config", "user.email", "patch@example.com")
		runGit(t, patchRepo, "config", "user.name", "Patch Author")

		// Clone from our workspace to get the same history
		runGit(t, patchRepo, "fetch", workDir, "HEAD")
		runGit(t, patchRepo, "reset", "--hard", "FETCH_HEAD")

		// Add a new commit
		if err := os.WriteFile(filepath.Join(patchRepo, "patched.txt"), []byte("patched content\n"), 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}
		runGit(t, patchRepo, "add", "patched.txt")
		runGit(t, patchRepo, "commit", "-m", "Add patched file")

		bundle := buildCommitReplayBundle(t, patchRepo, initialCommit)

		// Apply the patches
		finalCommit, err := provider.ApplyReplayBundle(ctx, "ws1", bundle)
		if err != nil {
			t.Fatalf("ApplyReplayBundle failed: %v", err)
		}

		// Verify final commit is different from initial
		if finalCommit == initialCommit {
			t.Error("Expected different final commit")
		}

		// Verify the patched file exists
		content, err := provider.ReadFile(ctx, "ws1", "", "patched.txt")
		if err != nil {
			t.Fatalf("Failed to read patched file: %v", err)
		}
		if string(content) != "patched content\n" {
			t.Errorf("Unexpected content: %s", content)
		}

		// Verify commit message and author were preserved
		commits, _ := provider.Log(ctx, "ws1", LogOptions{Limit: 1})
		if len(commits) == 0 {
			t.Fatal("Expected at least one commit")
		}
		if commits[0].Message != "Add patched file" {
			t.Errorf("Expected message 'Add patched file', got %s", commits[0].Message)
		}
		if commits[0].Author != "Patch Author" {
			t.Errorf("Expected author 'Patch Author', got %s", commits[0].Author)
		}
	})

	t.Run("applies multiple commit patches", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		workDir, _, _ := provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")
		// Configure git user for the workspace to avoid "Committer identity unknown" error
		runGit(t, workDir, "config", "user.email", "committer@example.com")
		runGit(t, workDir, "config", "user.name", "Test Committer")

		initialCommit := strings.TrimSpace(runGit(t, workDir, "rev-parse", "HEAD"))

		// Create patches with multiple commits
		patchRepo := t.TempDir()
		runGit(t, patchRepo, "init")
		runGit(t, patchRepo, "config", "user.email", "patch@example.com")
		runGit(t, patchRepo, "config", "user.name", "Patch Author")
		runGit(t, patchRepo, "fetch", workDir, "HEAD")
		runGit(t, patchRepo, "reset", "--hard", "FETCH_HEAD")

		// Add first commit
		if err := os.WriteFile(filepath.Join(patchRepo, "file1.txt"), []byte("file 1\n"), 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}
		runGit(t, patchRepo, "add", "file1.txt")
		runGit(t, patchRepo, "commit", "-m", "Add file 1")

		// Add second commit
		if err := os.WriteFile(filepath.Join(patchRepo, "file2.txt"), []byte("file 2\n"), 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}
		runGit(t, patchRepo, "add", "file2.txt")
		runGit(t, patchRepo, "commit", "-m", "Add file 2")

		// Add third commit
		if err := os.WriteFile(filepath.Join(patchRepo, "file3.txt"), []byte("file 3\n"), 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}
		runGit(t, patchRepo, "add", "file3.txt")
		runGit(t, patchRepo, "commit", "-m", "Add file 3")

		bundle := buildCommitReplayBundle(t, patchRepo, initialCommit)

		finalCommit, err := provider.ApplyReplayBundle(ctx, "ws1", bundle)
		if err != nil {
			t.Fatalf("ApplyReplayBundle failed: %v", err)
		}

		// Verify all files exist
		for _, fname := range []string{"file1.txt", "file2.txt", "file3.txt"} {
			if _, err := provider.ReadFile(ctx, "ws1", "", fname); err != nil {
				t.Errorf("Expected file %s to exist: %v", fname, err)
			}
		}

		// Verify we have 3 new commits (4 total including initial)
		commits, _ := provider.Log(ctx, "ws1", LogOptions{Limit: 10})
		if len(commits) != 4 {
			t.Errorf("Expected 4 commits, got %d", len(commits))
		}

		_ = finalCommit
	})

	t.Run("fails for unknown workspace", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)

		_, err := provider.ApplyReplayBundle(ctx, "nonexistent", []byte("patch content"))
		if err == nil {
			t.Error("Expected error for unknown workspace")
		}
	})

	t.Run("fails and rolls back on invalid patch", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		workDir, _, _ := provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")
		initialCommit := strings.TrimSpace(runGit(t, workDir, "rev-parse", "HEAD"))

		// Try to apply invalid patch
		_, err := provider.ApplyReplayBundle(ctx, "ws1", []byte("invalid patch content"))
		if err == nil {
			t.Error("Expected error for invalid patch")
		}

		// Verify we're back at the initial commit
		currentCommit := strings.TrimSpace(runGit(t, workDir, "rev-parse", "HEAD"))
		if currentCommit != initialCommit {
			t.Errorf("Expected rollback to %s, got %s", initialCommit, currentCommit)
		}
	})

	t.Run("preserves local changes when patch fails to apply", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		workDir, _, _ := provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")
		initialCommit := strings.TrimSpace(runGit(t, workDir, "rev-parse", "HEAD"))

		// Make local uncommitted changes to README.md (which exists in the test repo)
		localContent := "local uncommitted changes\n"
		if err := os.WriteFile(filepath.Join(workDir, "README.md"), []byte(localContent), 0644); err != nil {
			t.Fatalf("Failed to write local changes: %v", err)
		}

		// Create a patch that modifies the same file (will conflict)
		patchRepo := t.TempDir()
		runGit(t, patchRepo, "init")
		runGit(t, patchRepo, "config", "user.email", "patch@example.com")
		runGit(t, patchRepo, "config", "user.name", "Patch Author")
		runGit(t, patchRepo, "fetch", workDir, "HEAD")
		runGit(t, patchRepo, "reset", "--hard", "FETCH_HEAD")

		// Modify the same file in the patch
		if err := os.WriteFile(filepath.Join(patchRepo, "README.md"), []byte("conflicting patch content\n"), 0644); err != nil {
			t.Fatalf("Failed to write patch file: %v", err)
		}
		runGit(t, patchRepo, "add", "README.md")
		runGit(t, patchRepo, "commit", "-m", "Modify README")

		bundle := buildCommitReplayBundle(t, patchRepo, initialCommit)

		// Try to apply the conflicting patch
		_, err := provider.ApplyReplayBundle(ctx, "ws1", bundle)
		if err == nil {
			t.Error("Expected error for conflicting patch")
		}

		// Verify local changes are preserved
		content, err := os.ReadFile(filepath.Join(workDir, "README.md"))
		if err != nil {
			t.Fatalf("Failed to read file: %v", err)
		}
		if string(content) != localContent {
			t.Errorf("Local changes were lost. Expected %q, got %q", localContent, string(content))
		}

		// Verify we're still at the initial commit
		currentCommit := strings.TrimSpace(runGit(t, workDir, "rev-parse", "HEAD"))
		if currentCommit != initialCommit {
			t.Errorf("HEAD changed unexpectedly. Expected %s, got %s", initialCommit, currentCommit)
		}

		// Verify git status still shows the file as modified
		status := runGit(t, workDir, "status", "--porcelain")
		if !strings.Contains(status, "README.md") {
			t.Error("Expected README.md to still be in modified state")
		}
	})

	t.Run("preserves commit signatures in patches", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		workDir, _, _ := provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")
		// Configure git user for the workspace to avoid "Committer identity unknown" error
		runGit(t, workDir, "config", "user.email", "committer@example.com")
		runGit(t, workDir, "config", "user.name", "Test Committer")

		initialCommit := strings.TrimSpace(runGit(t, workDir, "rev-parse", "HEAD"))

		// Create a patch with specific author info
		patchRepo := t.TempDir()
		runGit(t, patchRepo, "init")
		runGit(t, patchRepo, "config", "user.email", "special@example.com")
		runGit(t, patchRepo, "config", "user.name", "Special Author")
		runGit(t, patchRepo, "fetch", workDir, "HEAD")
		runGit(t, patchRepo, "reset", "--hard", "FETCH_HEAD")

		if err := os.WriteFile(filepath.Join(patchRepo, "signed.txt"), []byte("signed content\n"), 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}
		runGit(t, patchRepo, "add", "signed.txt")
		runGit(t, patchRepo, "commit", "-m", "Signed commit")

		bundle := buildCommitReplayBundle(t, patchRepo, initialCommit)

		_, err := provider.ApplyReplayBundle(ctx, "ws1", bundle)
		if err != nil {
			t.Fatalf("ApplyReplayBundle failed: %v", err)
		}

		// Verify author info was preserved
		commits, _ := provider.Log(ctx, "ws1", LogOptions{Limit: 1})
		if commits[0].Author != "Special Author" {
			t.Errorf("Expected author 'Special Author', got %s", commits[0].Author)
		}
		if commits[0].AuthorEmail != "special@example.com" {
			t.Errorf("Expected email 'special@example.com', got %s", commits[0].AuthorEmail)
		}
	})
}

func TestWorkspaceIsolation(t *testing.T) {
	ctx := context.Background()

	t.Run("workspaces are isolated", func(t *testing.T) {
		baseDir := t.TempDir()
		provider, _ := NewLocalProvider(baseDir)
		sourceRepo := createTestRepo(t)

		workDir1, commit1, _ := provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")
		workDir2, commit2, _ := provider.EnsureWorkspace(ctx, "project1", "ws2", sourceRepo, "")

		// Verify different directories
		if workDir1 == workDir2 {
			t.Error("Expected different working directories")
		}

		// Verify same commit
		if commit1 != commit2 {
			t.Errorf("Expected same commit, got %s and %s", commit1, commit2)
		}

		// Make change in ws1
		provider.WriteFile(ctx, "ws1", "ws1-only.txt", []byte("ws1"))

		// ws2 should not see the change
		_, err := provider.ReadFile(ctx, "ws2", "", "ws1-only.txt")
		if err == nil {
			t.Error("ws2 should not see ws1's uncommitted file")
		}
	})
}

func TestConcurrentEnsureWorkspace(t *testing.T) {
	ctx := context.Background()
	baseDir := t.TempDir()
	provider, _ := NewLocalProvider(baseDir)
	sourceRepo := createTestRepo(t)

	// Ensure concurrent calls for the same workspace return the same result
	done := make(chan string, 10)

	for i := 0; i < 10; i++ {
		go func() {
			workDir, _, _ := provider.EnsureWorkspace(ctx, "project1", "ws1", sourceRepo, "")
			done <- workDir
		}()
	}

	var results []string
	for i := 0; i < 10; i++ {
		results = append(results, <-done)
	}

	// All results should be the same
	for _, r := range results {
		if r != results[0] {
			t.Errorf("Concurrent EnsureWorkspace returned different results: %v", results)
			break
		}
	}
}

func TestGetUserConfig(t *testing.T) {
	ctx := context.Background()
	baseDir := t.TempDir()
	provider, _ := NewLocalProvider(baseDir)

	t.Run("returns configured user name and email", func(t *testing.T) {
		// Set up global git config in a controlled environment
		// Use HOME override to isolate from system config
		homeDir := t.TempDir()
		t.Setenv("HOME", homeDir)
		t.Setenv("XDG_CONFIG_HOME", homeDir)

		// Configure git user in the test environment
		cmd := exec.Command("git", "config", "--global", "user.name", "Test User")
		cmd.Env = append(os.Environ(), "HOME="+homeDir, "XDG_CONFIG_HOME="+homeDir)
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to set user.name: %v", err)
		}

		cmd = exec.Command("git", "config", "--global", "user.email", "test@example.com")
		cmd.Env = append(os.Environ(), "HOME="+homeDir, "XDG_CONFIG_HOME="+homeDir)
		if err := cmd.Run(); err != nil {
			t.Fatalf("Failed to set user.email: %v", err)
		}

		name, email := provider.GetUserConfig(ctx)

		if name != "Test User" {
			t.Errorf("Expected name 'Test User', got %q", name)
		}
		if email != "test@example.com" {
			t.Errorf("Expected email 'test@example.com', got %q", email)
		}
	})

	t.Run("returns empty strings when not configured", func(t *testing.T) {
		// Use HOME override with an empty directory (no .gitconfig)
		emptyHome := t.TempDir()
		t.Setenv("HOME", emptyHome)
		t.Setenv("XDG_CONFIG_HOME", emptyHome)

		name, email := provider.GetUserConfig(ctx)

		// Both should be empty when not configured
		if name != "" {
			t.Errorf("Expected empty name, got %q", name)
		}
		if email != "" {
			t.Errorf("Expected empty email, got %q", email)
		}
	})
}
