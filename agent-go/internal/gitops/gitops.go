// Package gitops provides git operations for the agent API (diff, format-patch).
package gitops

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// FileDiffEntry represents a single changed file in the diff.
type FileDiffEntry struct {
	Path      string `json:"path"`
	Status    string `json:"status"` // "added", "modified", "deleted", "renamed"
	OldPath   string `json:"oldPath,omitempty"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Binary    bool   `json:"binary"`
	Patch     string `json:"patch,omitempty"`
}

// DiffStats contains summary statistics for a diff.
type DiffStats struct {
	FilesChanged int `json:"filesChanged"`
	Additions    int `json:"additions"`
	Deletions    int `json:"deletions"`
}

// DiffResult is the full diff result.
type DiffResult struct {
	Files []FileDiffEntry `json:"files"`
	Stats DiffStats       `json:"stats"`
}

// CommitsResult is the successful result of getting recent commit replay data.
type CommitsResult struct {
	ReplayBundle string `json:"replayBundle"`
	CommitCount  int    `json:"commitCount"`
}

type commitReplayBundle struct {
	Version int                 `json:"version"`
	Commits []commitReplayEntry `json:"commits"`
}

type commitReplayEntry struct {
	SHA            string             `json:"sha,omitempty"`
	Message        string             `json:"message"`
	AuthorName     string             `json:"authorName"`
	AuthorEmail    string             `json:"authorEmail"`
	AuthorDate     time.Time          `json:"authorDate"`
	CommitterName  string             `json:"committerName,omitempty"`
	CommitterEmail string             `json:"committerEmail,omitempty"`
	CommitterDate  *time.Time         `json:"committerDate,omitempty"`
	Changes        []commitFileChange `json:"changes"`
}

type commitFileChange struct {
	Path            string `json:"path"`
	OldPath         string `json:"oldPath,omitempty"`
	Status          string `json:"status"`
	Binary          bool   `json:"binary,omitempty"`
	PreviousContent []byte `json:"previousContent,omitempty"`
	Content         []byte `json:"content,omitempty"`
}

// CommitsError represents an error during commit operations.
type CommitsError struct {
	Code       string // "invalid_parent", "not_git_repo", "parent_mismatch", "no_commits"
	Message    string
	IsClean    bool   // populated for "no_commits": true when working tree has no uncommitted changes
	HeadCommit string // populated for "no_commits": the current HEAD commit SHA
}

func (e *CommitsError) Error() string {
	return e.Message
}

// IsGitRepo checks if the directory is a git repository.
func IsGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// IsWorkingTreeClean returns true when there are no staged, unstaged, or untracked changes.
func IsWorkingTreeClean(workspaceRoot string) bool {
	out, err := gitCmd(workspaceRoot, "status", "--porcelain")
	return err == nil && strings.TrimSpace(out) == ""
}

// HeadCommitSHA returns the current HEAD commit SHA for the repository.
func HeadCommitSHA(workspaceRoot string) string {
	out, err := gitCmd(workspaceRoot, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// gitCmd runs a git command in the given directory and returns stdout.
func gitCmd(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimRight(string(out), "\r\n"), err
}

// gitCmdCtx runs a git command with a context.
func gitCmdCtx(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimRight(string(out), "\r\n"), err
}

func gitCmdBytes(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Output()
}

// fetchOrigin fetches from origin with a timeout.
func fetchOrigin(dir string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := gitCmdCtx(ctx, dir, "fetch", "origin"); err != nil {
		log.Printf("Warning: failed to fetch from origin: %v", err)
	}
}

// getRemoteTrackingBranch finds the remote tracking branch.
// Tries origin/HEAD, origin/main, origin/master.
func getRemoteTrackingBranch(dir string) string {
	for _, ref := range []string{"origin/HEAD", "origin/main", "origin/master"} {
		if _, err := gitCmd(dir, "rev-parse", "--verify", ref); err == nil {
			return ref
		}
	}
	return ""
}

// getMergeBase calculates the merge-base between HEAD and the remote tracking branch.
func getMergeBase(dir string) string {
	remoteBranch := getRemoteTrackingBranch(dir)
	if remoteBranch == "" {
		return ""
	}
	base, err := gitCmd(dir, "merge-base", "HEAD", remoteBranch)
	if err != nil {
		log.Printf("Warning: failed to find merge-base with %s: %v", remoteBranch, err)
		return ""
	}
	return base
}

// parseDiffOutput parses unified diff output into structured entries.
func parseDiffOutput(output string) DiffResult {
	var files []FileDiffEntry
	var current *FileDiffEntry
	var patchLines []string

	for _, line := range strings.Split(output, "\n") {
		// Check for diff header
		if strings.HasPrefix(line, "diff --git a/") {
			// Save previous entry
			if current != nil {
				current.Patch = strings.Join(patchLines, "\n")
				files = append(files, *current)
			}

			// Parse "diff --git a/oldPath b/newPath"
			parts := strings.SplitN(line, " b/", 2)
			oldPath := strings.TrimPrefix(parts[0], "diff --git a/")
			newPath := oldPath
			if len(parts) == 2 {
				newPath = parts[1]
			}

			current = &FileDiffEntry{
				Path:   newPath,
				Status: "modified",
			}
			if oldPath != newPath {
				current.OldPath = oldPath
			}
			patchLines = []string{line}
			continue
		}

		if current != nil {
			patchLines = append(patchLines, line)

			switch {
			case strings.HasPrefix(line, "new file mode"):
				current.Status = "added"
			case strings.HasPrefix(line, "deleted file mode"):
				current.Status = "deleted"
			case strings.HasPrefix(line, "rename from"):
				current.Status = "renamed"
			case strings.HasPrefix(line, "Binary files"):
				current.Binary = true
			case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
				current.Additions++
			case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
				current.Deletions++
			}
		}
	}

	// Don't forget the last entry
	if current != nil {
		current.Patch = strings.Join(patchLines, "\n")
		files = append(files, *current)
	}

	stats := DiffStats{FilesChanged: len(files)}
	for _, f := range files {
		stats.Additions += f.Additions
		stats.Deletions += f.Deletions
	}

	return DiffResult{Files: files, Stats: stats}
}

// getUntrackedFiles returns a list of untracked files.
func getUntrackedFiles(dir string) []string {
	out, err := gitCmd(dir, "ls-files", "--others", "--exclude-standard")
	if err != nil || out == "" {
		return nil
	}
	lines := strings.Split(out, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

// isBinaryContent checks if content contains null bytes (likely binary).
func isBinaryContent(data []byte) bool {
	checkLen := len(data)
	if checkLen > 8000 {
		checkLen = 8000
	}
	for i := 0; i < checkLen; i++ {
		if data[i] == 0 {
			return true
		}
	}
	return false
}

// getUntrackedFileDiff creates a synthetic diff entry for an untracked file.
func getUntrackedFileDiff(dir, filePath string) FileDiffEntry {
	fullPath := filepath.Join(dir, filePath)

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return FileDiffEntry{
			Path:   filePath,
			Status: "added",
			Patch:  fmt.Sprintf("diff --git a/%s b/%s\nnew file mode 100644", filePath, filePath),
		}
	}

	if isBinaryContent(data) {
		return FileDiffEntry{
			Path:   filePath,
			Status: "added",
			Binary: true,
			Patch:  fmt.Sprintf("diff --git a/%s b/%s\nnew file mode 100644\nBinary file", filePath, filePath),
		}
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	// Handle trailing newline
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	additions := len(lines)

	var patchLines []string
	patchLines = append(patchLines, fmt.Sprintf("diff --git a/%s b/%s", filePath, filePath))
	patchLines = append(patchLines, "new file mode 100644")
	patchLines = append(patchLines, "--- /dev/null")
	patchLines = append(patchLines, fmt.Sprintf("+++ b/%s", filePath))
	patchLines = append(patchLines, fmt.Sprintf("@@ -0,0 +1,%d @@", additions))
	for _, line := range lines {
		patchLines = append(patchLines, "+"+line)
	}

	return FileDiffEntry{
		Path:      filePath,
		Status:    "added",
		Additions: additions,
		Patch:     strings.Join(patchLines, "\n"),
	}
}

// GetDiff returns the workspace diff. It fetches from origin, calculates merge-base,
// runs git diff, and includes untracked files.
func GetDiff(workspaceRoot string, singlePath string) DiffResult {
	if !IsGitRepo(workspaceRoot) {
		return DiffResult{
			Files: []FileDiffEntry{},
			Stats: DiffStats{},
		}
	}

	// Fetch from origin to get latest refs
	fetchOrigin(workspaceRoot)

	// Calculate merge-base
	mergeBase := getMergeBase(workspaceRoot)

	// Build git diff command
	args := []string{"diff", "--no-color"}
	if mergeBase != "" {
		args = append(args, mergeBase)
	}
	if singlePath != "" {
		args = append(args, "--", singlePath)
	}

	var trackedDiff DiffResult
	out, err := gitCmd(workspaceRoot, args...)
	if err != nil {
		// git diff may return exit code 1 when there are differences
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			// Try to get stdout from combined output
			cmd := exec.Command("git", args...)
			cmd.Dir = workspaceRoot
			stdout, _ := cmd.Output()
			trackedDiff = parseDiffOutput(string(stdout))
		} else {
			trackedDiff = DiffResult{Files: []FileDiffEntry{}, Stats: DiffStats{}}
		}
	} else {
		trackedDiff = parseDiffOutput(out)
	}

	// Get untracked files
	untrackedFiles := getUntrackedFiles(workspaceRoot)

	// Filter untracked files if looking for a single path
	if singlePath != "" {
		filtered := make([]string, 0)
		for _, f := range untrackedFiles {
			if f == singlePath {
				filtered = append(filtered, f)
			}
		}
		untrackedFiles = filtered
	}

	// Build diff entries for untracked files
	for _, f := range untrackedFiles {
		trackedDiff.Files = append(trackedDiff.Files, getUntrackedFileDiff(workspaceRoot, f))
	}

	// Recalculate stats
	trackedDiff.Stats = DiffStats{FilesChanged: len(trackedDiff.Files)}
	for _, f := range trackedDiff.Files {
		trackedDiff.Stats.Additions += f.Additions
		trackedDiff.Stats.Deletions += f.Deletions
	}

	return trackedDiff
}

// GetCommitReplayBundle returns the serialized commit replay bundle for commits since a parent.
func GetCommitReplayBundle(workspaceRoot, parent string) (*CommitsResult, *CommitsError) {
	if parent == "" || strings.TrimSpace(parent) == "" {
		return nil, &CommitsError{Code: "invalid_parent", Message: "Parent commit SHA is required"}
	}

	if !IsGitRepo(workspaceRoot) {
		return nil, &CommitsError{Code: "not_git_repo", Message: "Workspace is not a git repository"}
	}

	if _, err := gitCmd(workspaceRoot, "cat-file", "-t", parent); err != nil {
		return nil, &CommitsError{Code: "invalid_parent", Message: fmt.Sprintf("Parent commit %s does not exist in repository", parent)}
	}

	if _, err := gitCmd(workspaceRoot, "merge-base", "--is-ancestor", parent, "HEAD"); err != nil {
		return nil, &CommitsError{Code: "parent_mismatch", Message: fmt.Sprintf("Commit %s is not an ancestor of HEAD", parent)}
	}

	countStr, err := gitCmd(workspaceRoot, "rev-list", "--count", parent+"..HEAD")
	if err != nil {
		return nil, &CommitsError{
			Code:       "no_commits",
			Message:    "Failed to count commits",
			IsClean:    IsWorkingTreeClean(workspaceRoot),
			HeadCommit: HeadCommitSHA(workspaceRoot),
		}
	}
	commitCount, err := strconv.Atoi(countStr)
	if err != nil || commitCount == 0 {
		return nil, &CommitsError{
			Code:       "no_commits",
			Message:    fmt.Sprintf("No commits found between %s and HEAD", parent),
			IsClean:    IsWorkingTreeClean(workspaceRoot),
			HeadCommit: HeadCommitSHA(workspaceRoot),
		}
	}

	bundleJSON, err := buildCommitReplayBundle(workspaceRoot, parent)
	if err != nil {
		return nil, &CommitsError{Code: "no_commits", Message: fmt.Sprintf("Failed to build commit replay bundle: %v", err)}
	}

	return &CommitsResult{ReplayBundle: bundleJSON, CommitCount: commitCount}, nil
}

func buildCommitReplayBundle(workspaceRoot, parent string) (string, error) {
	commitsOutput, err := gitCmd(workspaceRoot, "rev-list", "--reverse", parent+"..HEAD")
	if err != nil {
		return "", err
	}

	bundle := commitReplayBundle{Version: 1}
	previous := parent
	for _, sha := range strings.Split(strings.TrimSpace(commitsOutput), "\n") {
		sha = strings.TrimSpace(sha)
		if sha == "" {
			continue
		}
		entry, err := buildCommitReplayEntry(workspaceRoot, previous, sha)
		if err != nil {
			return "", err
		}
		bundle.Commits = append(bundle.Commits, entry)
		previous = sha
	}

	payload, err := json.Marshal(bundle)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func buildCommitReplayEntry(workspaceRoot, parent, sha string) (commitReplayEntry, error) {
	metaBytes, err := gitCmdBytes(workspaceRoot, "show", "-s", "--format=%an%x00%ae%x00%aI%x00%cn%x00%ce%x00%cI%x00%B", sha)
	if err != nil {
		return commitReplayEntry{}, err
	}
	meta := strings.SplitN(string(metaBytes), "\x00", 7)
	if len(meta) != 7 {
		return commitReplayEntry{}, fmt.Errorf("unexpected metadata for commit %s", sha)
	}
	authorDate, err := time.Parse(time.RFC3339, strings.TrimSpace(meta[2]))
	if err != nil {
		return commitReplayEntry{}, err
	}
	committerDate, err := time.Parse(time.RFC3339, strings.TrimSpace(meta[5]))
	if err != nil {
		return commitReplayEntry{}, err
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

	changeBytes, err := gitCmdBytes(workspaceRoot, "diff-tree", "--no-commit-id", "--find-renames", "-r", "--name-status", "-z", parent, sha)
	if err != nil {
		return commitReplayEntry{}, err
	}
	for i, tokens := 0, splitNullTokens(changeBytes); i < len(tokens); {
		statusToken := tokens[i]
		i++
		switch {
		case strings.HasPrefix(statusToken, "R"):
			oldPath := tokens[i]
			newPath := tokens[i+1]
			i += 2
			oldContent, err := gitCmdBytes(workspaceRoot, "show", parent+":"+oldPath)
			if err != nil {
				return commitReplayEntry{}, err
			}
			newContent, err := gitCmdBytes(workspaceRoot, "show", sha+":"+newPath)
			if err != nil {
				return commitReplayEntry{}, err
			}
			entry.Changes = append(entry.Changes, commitFileChange{Path: newPath, OldPath: oldPath, Status: "renamed", PreviousContent: oldContent, Content: newContent})
		case statusToken == "A":
			path := tokens[i]
			i++
			content, err := gitCmdBytes(workspaceRoot, "show", sha+":"+path)
			if err != nil {
				return commitReplayEntry{}, err
			}
			entry.Changes = append(entry.Changes, commitFileChange{Path: path, Status: "added", Content: content})
		case statusToken == "M":
			path := tokens[i]
			i++
			previousContent, err := gitCmdBytes(workspaceRoot, "show", parent+":"+path)
			if err != nil {
				return commitReplayEntry{}, err
			}
			content, err := gitCmdBytes(workspaceRoot, "show", sha+":"+path)
			if err != nil {
				return commitReplayEntry{}, err
			}
			entry.Changes = append(entry.Changes, commitFileChange{Path: path, Status: "modified", PreviousContent: previousContent, Content: content})
		case statusToken == "D":
			path := tokens[i]
			i++
			previousContent, err := gitCmdBytes(workspaceRoot, "show", parent+":"+path)
			if err != nil {
				return commitReplayEntry{}, err
			}
			entry.Changes = append(entry.Changes, commitFileChange{Path: path, Status: "deleted", PreviousContent: previousContent})
		default:
			return commitReplayEntry{}, fmt.Errorf("unsupported change type %q", statusToken)
		}
	}

	return entry, nil
}

func splitNullTokens(data []byte) []string {
	parts := strings.Split(string(data), "\x00")
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			filtered = append(filtered, part)
		}
	}
	return filtered
}
