// Package git provides git operations with an abstracted interface.
// This allows for different implementations (local, remote) while keeping
// the same API.
package git

import (
	"context"
	"errors"
	"time"
)

// Common errors
var (
	ErrNotFound       = errors.New("not found")
	ErrNotARepository = errors.New("not a git repository")
	ErrInvalidRef     = errors.New("invalid ref")
	ErrCloneFailed    = errors.New("clone failed")
	ErrFetchFailed    = errors.New("fetch failed")
	ErrCheckoutFailed = errors.New("checkout failed")
	ErrDirtyWorkTree  = errors.New("working tree has uncommitted changes")
)

// WorkspaceSource provides workspace information to the git provider.
// This allows the provider to lookup workspace details from a store.
type WorkspaceSource interface {
	GetWorkspaceInfo(ctx context.Context, workspaceID string) (*WorkspaceInfo, error)
}

// WorkspaceInfo contains workspace details needed for git operations.
type WorkspaceInfo struct {
	WorkspaceID string
	ProjectID   string
	Path        string // git URL or local path
	SourceType  string // "local" or "git"
}

// Provider defines the interface for git operations.
// Implementations can be local (using git CLI) or remote (using a service).
type Provider interface {
	// EnsureWorkspace ensures a workspace has a working copy ready.
	// For git URLs: clones directly to the workspace directory.
	// For local paths: clones to get an isolated working copy.
	// projectID scopes the clone to a specific project's directory.
	// Returns the absolute path to the working directory and the current HEAD commit SHA.
	EnsureWorkspace(ctx context.Context, projectID, workspaceID, source, ref string) (workDir string, commit string, err error)

	// Fetch fetches updates from remote to the workspace.
	Fetch(ctx context.Context, workspaceID string) error

	// Checkout checks out a specific ref (branch, tag, or commit SHA).
	Checkout(ctx context.Context, workspaceID, ref string) error

	// Status returns the current git status of the workspace.
	Status(ctx context.Context, workspaceID string) (*Status, error)

	// Diff returns file diffs for the workspace.
	Diff(ctx context.Context, workspaceID string, opts DiffOptions) ([]FileDiff, error)

	// Branches lists all branches (local and remote).
	Branches(ctx context.Context, workspaceID string) ([]Branch, error)

	// FileTree returns the file listing at a specific ref (or HEAD if empty).
	FileTree(ctx context.Context, workspaceID, ref string) ([]FileEntry, error)

	// ReadFile reads a file at a specific ref (or working tree if ref is empty).
	ReadFile(ctx context.Context, workspaceID, ref, path string) ([]byte, error)

	// WriteFile writes content to a file in the working tree.
	WriteFile(ctx context.Context, workspaceID, path string, content []byte) error

	// Stage stages files for commit. Use "." to stage all changes.
	Stage(ctx context.Context, workspaceID string, paths []string) error

	// Commit creates a commit with the staged changes.
	Commit(ctx context.Context, workspaceID, message, authorName, authorEmail string) (*Commit, error)

	// Log returns commit history.
	Log(ctx context.Context, workspaceID string, opts LogOptions) ([]Commit, error)

	// GetWorkDir returns the working directory path for a workspace.
	// Returns empty string if workspace doesn't exist.
	GetWorkDir(ctx context.Context, workspaceID string) string

	// RemoveWorkspace removes the workspace working directory.
	RemoveWorkspace(ctx context.Context, workspaceID string) error

	// ApplyReplayBundle applies a serialized commit replay bundle to the workspace.
	// Returns the final commit SHA after all commits are replayed.
	// If application fails, the working tree is restored to its original state.
	ApplyReplayBundle(ctx context.Context, workspaceID string, replayBundle []byte) (finalCommit string, err error)

	// GetUserConfig retrieves the global git user name and email configuration.
	// Returns empty strings if not configured.
	GetUserConfig(ctx context.Context) (name, email string)
}

// Status represents the git status of a repository.
type Status struct {
	Branch       string       `json:"branch"`
	Commit       string       `json:"commit"`       // Current HEAD commit SHA
	CommitShort  string       `json:"commitShort"`  // Short commit SHA
	Ahead        int          `json:"ahead"`        // Commits ahead of upstream
	Behind       int          `json:"behind"`       // Commits behind upstream
	Staged       []FileStatus `json:"staged"`       // Staged changes
	Unstaged     []FileStatus `json:"unstaged"`     // Unstaged changes
	Untracked    []string     `json:"untracked"`    // Untracked files
	IsClean      bool         `json:"isClean"`      // No uncommitted changes
	HasConflicts bool         `json:"hasConflicts"` // Merge conflicts present
}

// FileStatus represents the status of a single file.
type FileStatus struct {
	Path    string `json:"path"`
	Status  string `json:"status"`  // "added", "modified", "deleted", "renamed", "copied"
	OldPath string `json:"oldPath"` // For renamed/copied files
}

// DiffOptions configures what diff to compute.
type DiffOptions struct {
	// Compare staged changes (git diff --cached)
	Staged bool

	// Compare against a specific ref (default: working tree vs HEAD)
	BaseRef string
	HeadRef string

	// Limit to specific paths
	Paths []string

	// Context lines around changes (default: 3)
	Context int
}

// FileDiff represents the diff of a single file.
type FileDiff struct {
	Path      string `json:"path"`
	OldPath   string `json:"oldPath"` // For renamed files
	Status    string `json:"status"`  // "added", "modified", "deleted", "renamed"
	Binary    bool   `json:"binary"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Patch     string `json:"patch"` // Unified diff content
}

// Branch represents a git branch.
type Branch struct {
	Name      string `json:"name"`
	IsRemote  bool   `json:"isRemote"`
	IsCurrent bool   `json:"isCurrent"`
	Commit    string `json:"commit"`   // HEAD commit SHA
	Upstream  string `json:"upstream"` // Upstream branch name (if tracking)
}

// FileEntry represents a file in the repository tree.
type FileEntry struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
	Mode  string `json:"mode"` // File mode (e.g., "100644")
}

// Commit represents a git commit.
type Commit struct {
	SHA         string    `json:"sha"`
	ShortSHA    string    `json:"shortSha"`
	Message     string    `json:"message"`
	Author      string    `json:"author"`
	AuthorEmail string    `json:"authorEmail"`
	AuthorDate  time.Time `json:"authorDate"`
	Committer   string    `json:"committer"`
	CommitDate  time.Time `json:"commitDate"`
	Parents     []string  `json:"parents"`
}

// LogOptions configures commit log retrieval.
type LogOptions struct {
	// Maximum number of commits to return (default: 50)
	Limit int

	// Start from this ref (default: HEAD)
	Ref string

	// Only commits affecting these paths
	Paths []string

	// Skip this many commits
	Skip int
}

// IsGitURL returns true if the source looks like a git URL.
func IsGitURL(source string) bool {
	// Check common git URL patterns
	if len(source) < 4 {
		return false
	}

	// git@host:path, https://host/path, git://host/path, ssh://host/path
	prefixes := []string{"git@", "https://", "http://", "git://", "ssh://"}
	for _, prefix := range prefixes {
		if len(source) >= len(prefix) && source[:len(prefix)] == prefix {
			return true
		}
	}

	// Also check for .git suffix which strongly suggests a git URL
	if len(source) > 4 && source[len(source)-4:] == ".git" {
		return true
	}

	return false
}
