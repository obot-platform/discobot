package git

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-billy/v5"
	gitrepo "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	indexformat "github.com/go-git/go-git/v5/plumbing/format/index"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/utils/merkletrie"
	"github.com/go-git/go-git/v5/utils/merkletrie/filesystem"
	mindex "github.com/go-git/go-git/v5/utils/merkletrie/index"
	"github.com/go-git/go-git/v5/utils/merkletrie/noder"
	"github.com/pmezard/go-difflib/difflib"

	"github.com/obot-platform/discobot/server/internal/model"
)

// LocalProvider implements Provider using go-git against local repositories.
// Workspaces are cloned directly to {baseDir}/{projectID}/workspaces/{workspaceID}.
type LocalProvider struct {
	baseDir string

	workspaceSource WorkspaceSource

	projectMu    sync.Mutex
	projectLocks map[string]*sync.Mutex

	mu             sync.RWMutex
	workspaceIndex map[string]*workspaceInfo
}

// LocalProviderOption configures a LocalProvider.
type LocalProviderOption func(*LocalProvider)

// WithWorkspaceSource sets the workspace source for the provider.
// This enables EnsureWorkspaceByID and auto-recovery in GetWorkDir.
func WithWorkspaceSource(src WorkspaceSource) LocalProviderOption {
	return func(p *LocalProvider) {
		p.workspaceSource = src
	}
}

type workspaceInfo struct {
	projectID string
	workDir   string
	source    string
	isRemote  bool
}

// NewLocalProvider creates a new local git provider.
// baseDir is the root directory where workspaces will be stored.
// Structure: {baseDir}/{projectID}/workspaces/{workspaceID}/
func NewLocalProvider(baseDir string, opts ...LocalProviderOption) (*LocalProvider, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create base directory: %w", err)
	}

	p := &LocalProvider{
		baseDir:        baseDir,
		projectLocks:   make(map[string]*sync.Mutex),
		workspaceIndex: make(map[string]*workspaceInfo),
	}

	for _, opt := range opts {
		opt(p)
	}

	return p, nil
}

func (p *LocalProvider) getProjectLock(projectID string) *sync.Mutex {
	p.projectMu.Lock()
	defer p.projectMu.Unlock()

	if lock, ok := p.projectLocks[projectID]; ok {
		return lock
	}

	lock := &sync.Mutex{}
	p.projectLocks[projectID] = lock
	return lock
}

// EnsureWorkspace ensures a workspace has a working copy ready.
func (p *LocalProvider) EnsureWorkspace(ctx context.Context, projectID, workspaceID, source, ref string) (string, string, error) {
	p.mu.RLock()
	if info, ok := p.workspaceIndex[workspaceID]; ok {
		p.mu.RUnlock()
		commit, _ := p.currentCommit(ctx, info.workDir)
		return info.workDir, commit, nil
	}
	p.mu.RUnlock()

	projectLock := p.getProjectLock(projectID)
	projectLock.Lock()
	defer projectLock.Unlock()

	p.mu.RLock()
	if info, ok := p.workspaceIndex[workspaceID]; ok {
		p.mu.RUnlock()
		commit, _ := p.currentCommit(ctx, info.workDir)
		return info.workDir, commit, nil
	}
	p.mu.RUnlock()

	projectWorkspacesDir := filepath.Join(p.baseDir, projectID, "workspaces")
	if err := os.MkdirAll(projectWorkspacesDir, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create project workspaces directory: %w", err)
	}

	workDir := filepath.Join(projectWorkspacesDir, workspaceID)
	if _, err := gitrepo.PlainOpen(workDir); err == nil {
		info := &workspaceInfo{projectID: projectID, workDir: workDir, source: source, isRemote: IsGitURL(source)}
		p.mu.Lock()
		p.workspaceIndex[workspaceID] = info
		p.mu.Unlock()
		commit, _ := p.currentCommit(ctx, workDir)
		return workDir, commit, nil
	}

	var cloneSource string
	var err error
	if IsGitURL(source) {
		cloneSource = source
	} else {
		cloneSource, err = filepath.Abs(source)
		if err != nil {
			return "", "", fmt.Errorf("invalid path: %w", err)
		}
		if _, err := gitrepo.PlainOpen(cloneSource); err != nil {
			return "", "", fmt.Errorf("%w: %s", ErrNotARepository, cloneSource)
		}
	}

	cloneOpts := &gitrepo.CloneOptions{URL: cloneSource}
	if ref != "" {
		if branchRef, ok := branchReferenceName(ref); ok {
			cloneOpts.ReferenceName = branchRef
			cloneOpts.SingleBranch = true
		}
	}

	repo, err := gitrepo.PlainCloneContext(ctx, workDir, false, cloneOpts)
	if err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrCloneFailed, err)
	}

	if ref != "" && cloneOpts.ReferenceName == "" {
		wt, err := repo.Worktree()
		if err != nil {
			return "", "", fmt.Errorf("failed to open worktree: %w", err)
		}
		if err := p.checkoutResolvedRef(repo, wt, ref); err != nil {
			return "", "", fmt.Errorf("%w: %v", ErrCheckoutFailed, err)
		}
	}

	info := &workspaceInfo{projectID: projectID, workDir: workDir, source: cloneSource, isRemote: IsGitURL(source)}
	p.mu.Lock()
	p.workspaceIndex[workspaceID] = info
	p.mu.Unlock()

	commit, _ := p.currentCommit(ctx, workDir)
	return workDir, commit, nil
}

// EnsureWorkspaceByID ensures workspace is ready using only workspaceID.
func (p *LocalProvider) EnsureWorkspaceByID(ctx context.Context, workspaceID string) (string, string, error) {
	if p.workspaceSource == nil {
		return "", "", fmt.Errorf("workspace source not configured")
	}

	p.mu.RLock()
	if info, ok := p.workspaceIndex[workspaceID]; ok {
		p.mu.RUnlock()
		commit, _ := p.currentCommit(ctx, info.workDir)
		return info.workDir, commit, nil
	}
	p.mu.RUnlock()

	wsInfo, err := p.workspaceSource.GetWorkspaceInfo(ctx, workspaceID)
	if err != nil {
		return "", "", fmt.Errorf("workspace lookup failed: %w", err)
	}

	isRemote := IsGitURL(wsInfo.Path)
	if !isRemote && (wsInfo.SourceType == model.WorkspaceSourceTypeLocal || wsInfo.SourceType == model.WorkspaceSourceTypeManaged) {
		return p.registerLocalWorkspace(ctx, workspaceID, wsInfo.ProjectID, wsInfo.Path)
	}

	return p.EnsureWorkspace(ctx, wsInfo.ProjectID, workspaceID, wsInfo.Path, "")
}

func (p *LocalProvider) registerLocalWorkspace(ctx context.Context, workspaceID, projectID, localPath string) (string, string, error) {
	absPath, err := filepath.Abs(localPath)
	if err != nil {
		return "", "", fmt.Errorf("invalid path: %w", err)
	}

	if _, err := gitrepo.PlainOpen(absPath); err != nil {
		return "", "", fmt.Errorf("%w: %s", ErrNotARepository, absPath)
	}

	commit, err := p.currentCommit(ctx, absPath)
	if err != nil {
		return "", "", fmt.Errorf("failed to get HEAD: %w", err)
	}

	p.mu.Lock()
	p.workspaceIndex[workspaceID] = &workspaceInfo{projectID: projectID, workDir: absPath, source: localPath, isRemote: false}
	p.mu.Unlock()

	return absPath, commit, nil
}

// Fetch fetches updates from remote.
func (p *LocalProvider) Fetch(ctx context.Context, workspaceID string) error {
	workDir := p.GetWorkDir(ctx, workspaceID)
	if workDir == "" {
		return fmt.Errorf("%w: workspace %s", ErrNotFound, workspaceID)
	}

	repo, err := gitrepo.PlainOpen(workDir)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	}

	err = repo.FetchContext(ctx, &gitrepo.FetchOptions{RemoteName: gitrepo.DefaultRemoteName, Prune: true, Tags: gitrepo.AllTags})
	if err != nil && err != gitrepo.NoErrAlreadyUpToDate {
		return fmt.Errorf("%w: %v", ErrFetchFailed, err)
	}

	return nil
}

// Checkout checks out a specific ref.
func (p *LocalProvider) Checkout(ctx context.Context, workspaceID, ref string) error {
	workDir := p.GetWorkDir(ctx, workspaceID)
	if workDir == "" {
		return fmt.Errorf("%w: workspace %s", ErrNotFound, workspaceID)
	}

	repo, wt, err := p.openWorktree(workDir)
	if err != nil {
		return err
	}

	if err := p.checkoutResolvedRef(repo, wt, ref); err != nil {
		return fmt.Errorf("%w: %v", ErrCheckoutFailed, err)
	}

	return nil
}

// Status returns the current git status.
func (p *LocalProvider) Status(ctx context.Context, workspaceID string) (*Status, error) {
	workDir := p.GetWorkDir(ctx, workspaceID)
	if workDir == "" {
		return nil, fmt.Errorf("%w: workspace %s", ErrNotFound, workspaceID)
	}

	repo, wt, err := p.openWorktree(workDir)
	if err != nil {
		return nil, err
	}

	gitStatus, err := wt.StatusWithOptions(gitrepo.StatusOptions{Strategy: gitrepo.Preload})
	if err != nil {
		return nil, err
	}

	status := &Status{Staged: []FileStatus{}, Unstaged: []FileStatus{}, Untracked: []string{}}

	if head, err := repo.Head(); err == nil {
		status.Commit = head.Hash().String()
		if len(status.Commit) >= 7 {
			status.CommitShort = status.Commit[:7]
		}
		if head.Name().IsBranch() {
			status.Branch = head.Name().Short()
		} else {
			status.Branch = head.Name().Short()
		}
		status.Ahead, status.Behind = p.countAheadBehind(repo, head)
	}

	paths := make([]string, 0, len(gitStatus))
	for path := range gitStatus {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	status.IsClean = true
	for _, path := range paths {
		entry := gitStatus[path]
		if entry.Staging == gitrepo.Unmodified && entry.Worktree == gitrepo.Unmodified {
			continue
		}
		status.IsClean = false
		if entry.Staging == gitrepo.UpdatedButUnmerged || entry.Worktree == gitrepo.UpdatedButUnmerged {
			status.HasConflicts = true
		}
		if entry.Staging != gitrepo.Unmodified && entry.Staging != gitrepo.Untracked {
			status.Staged = append(status.Staged, FileStatus{Path: path, Status: statusCodeToString(entry.Staging), OldPath: entry.Extra})
		}
		if entry.Worktree == gitrepo.Untracked {
			status.Untracked = append(status.Untracked, path)
			continue
		}
		if entry.Worktree != gitrepo.Unmodified {
			status.Unstaged = append(status.Unstaged, FileStatus{Path: path, Status: statusCodeToString(entry.Worktree), OldPath: entry.Extra})
		}
	}

	return status, nil
}

// Diff returns file diffs.
func (p *LocalProvider) Diff(ctx context.Context, workspaceID string, opts DiffOptions) ([]FileDiff, error) {
	workDir := p.GetWorkDir(ctx, workspaceID)
	if workDir == "" {
		return nil, fmt.Errorf("%w: workspace %s", ErrNotFound, workspaceID)
	}

	repo, wt, err := p.openWorktree(workDir)
	if err != nil {
		return nil, err
	}

	contextLines := 3
	if opts.Context > 0 {
		contextLines = opts.Context
	}

	switch {
	case opts.BaseRef != "" && opts.HeadRef != "":
		return p.diffBetweenRefs(ctx, repo, opts.BaseRef, opts.HeadRef, opts.Paths, contextLines)
	case opts.BaseRef != "" && opts.Staged:
		baseTree, err := p.resolveTree(repo, opts.BaseRef)
		if err != nil {
			return nil, err
		}
		return p.diffTreeToIndex(repo, wt, baseTree, opts.Paths, contextLines)
	case opts.BaseRef != "":
		baseTree, err := p.resolveTree(repo, opts.BaseRef)
		if err != nil {
			return nil, err
		}
		return p.diffTreeToWorktree(repo, wt, baseTree, opts.Paths, contextLines)
	case opts.Staged:
		headTree, err := p.headTree(repo)
		if err != nil {
			return nil, err
		}
		return p.diffTreeToIndex(repo, wt, headTree, opts.Paths, contextLines)
	default:
		return p.diffIndexToWorktree(repo, wt, opts.Paths, contextLines)
	}
}

// Branches lists all branches.
func (p *LocalProvider) Branches(ctx context.Context, workspaceID string) ([]Branch, error) {
	workDir := p.GetWorkDir(ctx, workspaceID)
	if workDir == "" {
		return nil, fmt.Errorf("%w: workspace %s", ErrNotFound, workspaceID)
	}

	repo, err := gitrepo.PlainOpen(workDir)
	if err != nil {
		return nil, err
	}

	current := ""
	if head, err := repo.Head(); err == nil && head.Name().IsBranch() {
		current = head.Name().Short()
	}

	cfg, _ := repo.Config()
	var branches []Branch

	localIter, err := repo.Branches()
	if err != nil {
		return nil, err
	}
	if err := localIter.ForEach(func(ref *plumbing.Reference) error {
		branch := Branch{Name: ref.Name().Short(), IsRemote: false, IsCurrent: ref.Name().Short() == current, Commit: ref.Hash().String()}
		if cfg != nil {
			if bcfg, ok := cfg.Branches[branch.Name]; ok && bcfg.Remote != "" && bcfg.Merge != "" {
				branch.Upstream = plumbing.NewRemoteReferenceName(bcfg.Remote, bcfg.Merge.Short()).Short()
			}
		}
		branches = append(branches, branch)
		return nil
	}); err != nil {
		return nil, err
	}

	refIter, err := repo.References()
	if err != nil {
		return nil, err
	}
	if err := refIter.ForEach(func(ref *plumbing.Reference) error {
		if !ref.Name().IsRemote() {
			return nil
		}
		name := ref.Name().Short()
		if name == "origin/HEAD" || strings.HasSuffix(name, "/HEAD") {
			return nil
		}
		branches = append(branches, Branch{Name: name, IsRemote: true, Commit: ref.Hash().String()})
		return nil
	}); err != nil {
		return nil, err
	}

	sort.Slice(branches, func(i, j int) bool { return branches[i].Name < branches[j].Name })
	return branches, nil
}

// FileTree returns the file listing at a specific ref.
func (p *LocalProvider) FileTree(ctx context.Context, workspaceID, ref string) ([]FileEntry, error) {
	workDir := p.GetWorkDir(ctx, workspaceID)
	if workDir == "" {
		return nil, fmt.Errorf("%w: workspace %s", ErrNotFound, workspaceID)
	}

	repo, err := gitrepo.PlainOpen(workDir)
	if err != nil {
		return nil, err
	}

	tree, err := p.resolveTree(repo, defaultRef(ref))
	if err != nil {
		return nil, err
	}

	var entries []FileEntry
	iter := tree.Files()
	if err := iter.ForEach(func(file *object.File) error {
		entries = append(entries, FileEntry{Path: file.Name, Name: filepath.Base(file.Name), IsDir: false, Size: file.Size, Mode: file.Mode.String()})
		return nil
	}); err != nil {
		return nil, err
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

// ReadFile reads a file at a specific ref.
func (p *LocalProvider) ReadFile(ctx context.Context, workspaceID, ref, path string) ([]byte, error) {
	workDir := p.GetWorkDir(ctx, workspaceID)
	if workDir == "" {
		return nil, fmt.Errorf("%w: workspace %s", ErrNotFound, workspaceID)
	}

	if ref == "" {
		return os.ReadFile(filepath.Join(workDir, path))
	}

	repo, err := gitrepo.PlainOpen(workDir)
	if err != nil {
		return nil, err
	}

	tree, err := p.resolveTree(repo, ref)
	if err != nil {
		return nil, err
	}

	file, err := tree.File(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %s at %s", ErrNotFound, path, ref)
	}
	return readBlobContent(file)
}

// WriteFile writes content to a file in the working tree.
func (p *LocalProvider) WriteFile(ctx context.Context, workspaceID, path string, content []byte) error {
	workDir := p.GetWorkDir(ctx, workspaceID)
	if workDir == "" {
		return fmt.Errorf("%w: workspace %s", ErrNotFound, workspaceID)
	}

	fullPath := filepath.Join(workDir, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return err
	}
	return os.WriteFile(fullPath, content, 0644)
}

// Stage stages files for commit.
func (p *LocalProvider) Stage(ctx context.Context, workspaceID string, paths []string) error {
	workDir := p.GetWorkDir(ctx, workspaceID)
	if workDir == "" {
		return fmt.Errorf("%w: workspace %s", ErrNotFound, workspaceID)
	}

	_, wt, err := p.openWorktree(workDir)
	if err != nil {
		return err
	}

	for _, path := range paths {
		if path == "." {
			if err := wt.AddWithOptions(&gitrepo.AddOptions{All: true}); err != nil {
				return err
			}
			continue
		}
		if _, err := wt.Add(path); err != nil {
			return err
		}
	}
	return nil
}

// Commit creates a commit with the staged changes.
func (p *LocalProvider) Commit(ctx context.Context, workspaceID, message, authorName, authorEmail string) (*Commit, error) {
	workDir := p.GetWorkDir(ctx, workspaceID)
	if workDir == "" {
		return nil, fmt.Errorf("%w: workspace %s", ErrNotFound, workspaceID)
	}

	repo, wt, err := p.openWorktree(workDir)
	if err != nil {
		return nil, err
	}

	commitOpts := &gitrepo.CommitOptions{}
	if authorName != "" && authorEmail != "" {
		commitOpts.Author = &object.Signature{Name: authorName, Email: authorEmail, When: time.Now()}
		if committer := p.loadCommitterSignature(repo); committer != nil {
			commitOpts.Committer = committer
		}
	}

	if _, err := wt.Commit(message, commitOpts); err != nil {
		return nil, err
	}
	return p.getCommit(ctx, repo, "HEAD")
}

// Log returns commit history.
func (p *LocalProvider) Log(ctx context.Context, workspaceID string, opts LogOptions) ([]Commit, error) {
	workDir := p.GetWorkDir(ctx, workspaceID)
	if workDir == "" {
		return nil, fmt.Errorf("%w: workspace %s", ErrNotFound, workspaceID)
	}

	repo, err := gitrepo.PlainOpen(workDir)
	if err != nil {
		return nil, err
	}

	startHash, _, err := p.resolveCommit(repo, defaultRef(opts.Ref))
	if err != nil {
		return nil, err
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}

	logOpts := &gitrepo.LogOptions{From: startHash, Order: gitrepo.LogOrderCommitterTime}
	if len(opts.Paths) == 1 {
		logOpts.FileName = &opts.Paths[0]
	} else if len(opts.Paths) > 1 {
		logOpts.PathFilter = func(path string) bool { return matchesPaths(path, opts.Paths) }
	}

	iter, err := repo.Log(logOpts)
	if err != nil {
		return nil, err
	}

	var commits []Commit
	skipped := 0
	err = iter.ForEach(func(c *object.Commit) error {
		if skipped < opts.Skip {
			skipped++
			return nil
		}
		commits = append(commits, mapCommit(c))
		if len(commits) >= limit {
			return io.EOF
		}
		return nil
	})
	if err != nil && err != io.EOF {
		return nil, err
	}

	return commits, nil
}

// GetWorkDir returns the working directory path for a workspace.
func (p *LocalProvider) GetWorkDir(ctx context.Context, workspaceID string) string {
	p.mu.RLock()
	if info, ok := p.workspaceIndex[workspaceID]; ok {
		p.mu.RUnlock()
		return info.workDir
	}
	p.mu.RUnlock()

	if p.workspaceSource != nil {
		if workDir, _, err := p.EnsureWorkspaceByID(ctx, workspaceID); err == nil {
			return workDir
		}
	}

	return ""
}

// RemoveWorkspace removes the workspace working directory.
func (p *LocalProvider) RemoveWorkspace(_ context.Context, workspaceID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	info, ok := p.workspaceIndex[workspaceID]
	if !ok {
		return nil
	}

	delete(p.workspaceIndex, workspaceID)
	return os.RemoveAll(info.workDir)
}

// ApplyReplayBundle applies the serialized commit replay bundle to the workspace.
func (p *LocalProvider) ApplyReplayBundle(ctx context.Context, workspaceID string, replayBundle []byte) (string, error) {
	workDir := p.GetWorkDir(ctx, workspaceID)
	if workDir == "" {
		return "", fmt.Errorf("%w: workspace %s", ErrNotFound, workspaceID)
	}

	bundle, err := decodeCommitReplayBundle(replayBundle)
	if err != nil {
		return "", fmt.Errorf("failed to apply commit replay bundle: %w", err)
	}

	repo, wt, err := p.openWorktree(workDir)
	if err != nil {
		return "", err
	}

	state := newReplayState(workDir)
	for _, commit := range bundle.Commits {
		if err := state.ValidateAndApply(commit.Changes); err != nil {
			return "", fmt.Errorf("commit %q will not apply cleanly: %w", commit.Message, err)
		}
	}

	rollback, err := snapshotReplayRollback(repo, wt, workDir, bundle)
	if err != nil {
		return "", err
	}

	defer func() {
		if err != nil {
			_ = rollback.Restore()
		}
	}()

	for _, replayCommit := range bundle.Commits {
		stagedPaths := make([]string, 0, len(replayCommit.Changes)*2)
		for _, change := range replayCommit.Changes {
			if err := applyReplayChange(workDir, change); err != nil {
				return "", fmt.Errorf("failed to apply commit %q: %w", replayCommit.Message, err)
			}
			switch change.Status {
			case "renamed":
				stagedPaths = append(stagedPaths, change.OldPath, change.Path)
			default:
				stagedPaths = append(stagedPaths, change.Path)
			}
		}

		if err := stagePaths(wt, stagedPaths); err != nil {
			return "", fmt.Errorf("failed to stage replayed commit %q: %w", replayCommit.Message, err)
		}

		author := &object.Signature{Name: replayCommit.AuthorName, Email: replayCommit.AuthorEmail, When: replayCommit.AuthorDate}
		committer := author
		if replayCommit.CommitterName != "" && replayCommit.CommitterEmail != "" {
			when := replayCommit.AuthorDate
			if replayCommit.CommitterDate != nil {
				when = *replayCommit.CommitterDate
			}
			committer = &object.Signature{Name: replayCommit.CommitterName, Email: replayCommit.CommitterEmail, When: when}
		}

		if _, err := wt.Commit(replayCommit.Message, &gitrepo.CommitOptions{Author: author, Committer: committer}); err != nil {
			return "", fmt.Errorf("failed to create replayed commit %q: %w", replayCommit.Message, err)
		}
	}

	finalCommit, err := p.currentCommit(ctx, workDir)
	if err != nil {
		return "", fmt.Errorf("failed to get final commit: %w", err)
	}
	return finalCommit, nil
}

func cleanGitEnv() []string {
	var env []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			env = append(env, entry)
		}
	}
	return env
}

// GetUserConfig retrieves the global git user name and email configuration.
func (p *LocalProvider) GetUserConfig(_ context.Context) (name, email string) {
	cfg, err := config.LoadConfig(config.GlobalScope)
	if err != nil {
		return "", ""
	}
	return cfg.User.Name, cfg.User.Email
}

func (p *LocalProvider) openWorktree(workDir string) (*gitrepo.Repository, *gitrepo.Worktree, error) {
	repo, err := gitrepo.PlainOpen(workDir)
	if err != nil {
		return nil, nil, err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return nil, nil, err
	}
	return repo, wt, nil
}

func (p *LocalProvider) currentCommit(_ context.Context, workDir string) (string, error) {
	repo, err := gitrepo.PlainOpen(workDir)
	if err != nil {
		return "", err
	}
	head, err := repo.Head()
	if err != nil {
		return "", err
	}
	return head.Hash().String(), nil
}

func defaultRef(ref string) string {
	if ref == "" {
		return "HEAD"
	}
	return ref
}

func branchReferenceName(ref string) (plumbing.ReferenceName, bool) {
	if ref == "" {
		return "", false
	}
	if strings.HasPrefix(ref, "refs/heads/") {
		return plumbing.ReferenceName(ref), true
	}
	if strings.Contains(ref, "/") || strings.HasPrefix(ref, "refs/") {
		return "", false
	}
	return plumbing.NewBranchReferenceName(ref), true
}

func (p *LocalProvider) resolveCommit(repo *gitrepo.Repository, ref string) (plumbing.Hash, *plumbing.Reference, error) {
	if ref == "" || ref == "HEAD" {
		head, err := repo.Head()
		if err != nil {
			return plumbing.ZeroHash, nil, err
		}
		return head.Hash(), head, nil
	}

	candidates := []plumbing.ReferenceName{
		plumbing.ReferenceName(ref),
		plumbing.NewBranchReferenceName(ref),
		plumbing.NewTagReferenceName(ref),
		plumbing.NewRemoteReferenceName(gitrepo.DefaultRemoteName, ref),
	}
	for _, candidate := range candidates {
		resolved, err := repo.Reference(candidate, true)
		if err == nil {
			return resolved.Hash(), resolved, nil
		}
	}

	hash, err := repo.ResolveRevision(plumbing.Revision(ref))
	if err != nil {
		return plumbing.ZeroHash, nil, fmt.Errorf("%w: %s", ErrInvalidRef, ref)
	}
	return *hash, nil, nil
}

func (p *LocalProvider) resolveTree(repo *gitrepo.Repository, ref string) (*object.Tree, error) {
	hash, _, err := p.resolveCommit(repo, ref)
	if err != nil {
		return nil, err
	}
	commit, err := repo.CommitObject(hash)
	if err != nil {
		return nil, err
	}
	return commit.Tree()
}

func (p *LocalProvider) headTree(repo *gitrepo.Repository) (*object.Tree, error) {
	head, err := repo.Head()
	if err != nil {
		if err == plumbing.ErrReferenceNotFound {
			return nil, nil
		}
		return nil, err
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return nil, err
	}
	return commit.Tree()
}

func (p *LocalProvider) checkoutResolvedRef(repo *gitrepo.Repository, wt *gitrepo.Worktree, ref string) error {
	if branchRef, ok := branchReferenceName(ref); ok {
		if _, err := repo.Reference(branchRef, false); err == nil {
			return wt.Checkout(&gitrepo.CheckoutOptions{Branch: branchRef})
		}
		remoteRefName := plumbing.NewRemoteReferenceName(gitrepo.DefaultRemoteName, branchRef.Short())
		if remoteRef, err := repo.Reference(remoteRefName, true); err == nil {
			if err := wt.Checkout(&gitrepo.CheckoutOptions{Branch: branchRef, Create: true, Hash: remoteRef.Hash()}); err == nil {
				return nil
			}
		}
	}

	hash, resolvedRef, err := p.resolveCommit(repo, ref)
	if err != nil {
		return err
	}
	checkoutOpts := &gitrepo.CheckoutOptions{}
	if resolvedRef != nil && resolvedRef.Name().IsBranch() {
		checkoutOpts.Branch = resolvedRef.Name()
	} else {
		checkoutOpts.Hash = hash
	}
	return wt.Checkout(checkoutOpts)
}

func (p *LocalProvider) countAheadBehind(repo *gitrepo.Repository, head *plumbing.Reference) (int, int) {
	if head == nil || !head.Name().IsBranch() {
		return 0, 0
	}

	cfg, err := repo.Config()
	if err != nil {
		return 0, 0
	}
	branchCfg, ok := cfg.Branches[head.Name().Short()]
	if !ok || branchCfg.Remote == "" || branchCfg.Merge == "" {
		return 0, 0
	}

	upstreamName := plumbing.NewRemoteReferenceName(branchCfg.Remote, branchCfg.Merge.Short())
	upstreamRef, err := repo.Reference(upstreamName, true)
	if err != nil {
		return 0, 0
	}

	headCommit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return 0, 0
	}
	upstreamCommit, err := repo.CommitObject(upstreamRef.Hash())
	if err != nil {
		return 0, 0
	}

	bases, err := headCommit.MergeBase(upstreamCommit)
	if err != nil || len(bases) == 0 {
		return 0, 0
	}
	baseHash := bases[0].Hash

	return countCommitsUntil(repo, head.Hash(), baseHash), countCommitsUntil(repo, upstreamRef.Hash(), baseHash)
}

func countCommitsUntil(repo *gitrepo.Repository, from, until plumbing.Hash) int {
	iter, err := repo.Log(&gitrepo.LogOptions{From: from})
	if err != nil {
		return 0
	}
	count := 0
	_ = iter.ForEach(func(commit *object.Commit) error {
		if commit.Hash == until {
			return io.EOF
		}
		count++
		return nil
	})
	return count
}

func (p *LocalProvider) diffBetweenRefs(ctx context.Context, repo *gitrepo.Repository, baseRef, headRef string, paths []string, contextLines int) ([]FileDiff, error) {
	baseCommit, _, err := p.resolveCommit(repo, baseRef)
	if err != nil {
		return nil, err
	}
	headCommit, _, err := p.resolveCommit(repo, headRef)
	if err != nil {
		return nil, err
	}
	fromCommit, err := repo.CommitObject(baseCommit)
	if err != nil {
		return nil, err
	}
	toCommit, err := repo.CommitObject(headCommit)
	if err != nil {
		return nil, err
	}
	fromTree, err := fromCommit.Tree()
	if err != nil {
		return nil, err
	}
	toTree, err := toCommit.Tree()
	if err != nil {
		return nil, err
	}
	changes, err := object.DiffTreeWithOptions(ctx, fromTree, toTree, object.DefaultDiffTreeOptions)
	if err != nil {
		return nil, err
	}
	return buildTreeBackedDiffs(changes, paths, contextLines)
}

func (p *LocalProvider) diffTreeToIndex(repo *gitrepo.Repository, _ *gitrepo.Worktree, tree *object.Tree, paths []string, contextLines int) ([]FileDiff, error) {
	idx, err := repo.Storer.Index()
	if err != nil {
		return nil, err
	}
	changes, err := diffTreeToIndexChanges(tree, idx)
	if err != nil {
		return nil, err
	}
	return buildIndexBackedDiffs(repo, tree, idx, changes, paths, contextLines)
}

func (p *LocalProvider) diffIndexToWorktree(repo *gitrepo.Repository, wt *gitrepo.Worktree, paths []string, contextLines int) ([]FileDiff, error) {
	idx, err := repo.Storer.Index()
	if err != nil {
		return nil, err
	}
	changes, err := diffIndexToWorktreeChanges(wt, idx)
	if err != nil {
		return nil, err
	}
	return buildFilesystemBackedDiffs(repo, idx, nil, wt.Filesystem, changes, paths, contextLines, true)
}

func (p *LocalProvider) diffTreeToWorktree(repo *gitrepo.Repository, wt *gitrepo.Worktree, tree *object.Tree, paths []string, contextLines int) ([]FileDiff, error) {
	idx, err := repo.Storer.Index()
	if err != nil {
		return nil, err
	}
	changes, err := diffTreeToWorktreeChanges(wt, tree, idx)
	if err != nil {
		return nil, err
	}
	return buildFilesystemBackedDiffs(repo, idx, tree, wt.Filesystem, changes, paths, contextLines, false)
}

func diffTreeToIndexChanges(tree *object.Tree, idx *indexformat.Index) (merkletrie.Changes, error) {
	var from noder.Noder
	if tree != nil {
		from = object.NewTreeRootNode(tree)
	}
	to := mindex.NewRootNode(idx)
	return merkletrie.DiffTree(from, to, diffTreeIsEquals)
}

func diffIndexToWorktreeChanges(wt *gitrepo.Worktree, idx *indexformat.Index) (merkletrie.Changes, error) {
	submodules, err := getSubmoduleStatus(wt)
	if err != nil {
		return nil, err
	}
	from := mindex.NewRootNode(idx)
	to := filesystem.NewRootNodeWithOptions(wt.Filesystem, submodules, filesystem.Options{Index: idx})
	return merkletrie.DiffTree(from, to, diffTreeIsEquals)
}

func diffTreeToWorktreeChanges(wt *gitrepo.Worktree, tree *object.Tree, idx *indexformat.Index) (merkletrie.Changes, error) {
	var from noder.Noder
	if tree != nil {
		from = object.NewTreeRootNode(tree)
	}
	submodules, err := getSubmoduleStatus(wt)
	if err != nil {
		return nil, err
	}
	to := filesystem.NewRootNodeWithOptions(wt.Filesystem, submodules, filesystem.Options{Index: idx})
	return merkletrie.DiffTree(from, to, diffTreeIsEquals)
}

func getSubmoduleStatus(wt *gitrepo.Worktree) (map[string]plumbing.Hash, error) {
	result := map[string]plumbing.Hash{}
	submodules, err := wt.Submodules()
	if err != nil {
		return nil, err
	}
	statuses, err := submodules.Status()
	if err != nil {
		return nil, err
	}
	for _, status := range statuses {
		if status.Current.IsZero() {
			result[status.Path] = status.Expected
			continue
		}
		result[status.Path] = status.Current
	}
	return result, nil
}

var emptyNoderHash = make([]byte, 24)

func diffTreeIsEquals(a, b noder.Hasher) bool {
	hashA := a.Hash()
	hashB := b.Hash()
	if bytes.Equal(hashA, emptyNoderHash) || bytes.Equal(hashB, emptyNoderHash) {
		return false
	}
	return bytes.Equal(hashA, hashB)
}

func buildIndexBackedDiffs(repo *gitrepo.Repository, tree *object.Tree, idx *indexformat.Index, changes merkletrie.Changes, paths []string, contextLines int) ([]FileDiff, error) {
	entries := indexEntryMap(idx)
	var diffs []FileDiff
	for _, change := range changes {
		action, err := change.Action()
		if err != nil {
			return nil, err
		}
		var path string
		switch action {
		case merkletrie.Delete:
			path = change.From.String()
		default:
			path = change.To.String()
		}
		if !matchesDiffPaths(path, "", paths) {
			continue
		}

		var oldContent []byte
		var newContent []byte
		status := "modified"
		oldPath := ""
		switch action {
		case merkletrie.Insert:
			status = "added"
			entry := entries[path]
			newContent, err = readIndexEntry(repo, entry)
		case merkletrie.Delete:
			status = "deleted"
			oldContent, err = readTreePath(tree, path)
		case merkletrie.Modify:
			oldContent, err = readTreePath(tree, path)
			if err == nil {
				newContent, err = readIndexEntry(repo, entries[path])
			}
		}
		if err != nil {
			return nil, err
		}
		diffs = append(diffs, buildFileDiff(path, oldPath, status, oldContent, newContent, contextLines))
	}
	sort.Slice(diffs, func(i, j int) bool { return diffs[i].Path < diffs[j].Path })
	return diffs, nil
}

func buildFilesystemBackedDiffs(repo *gitrepo.Repository, idx *indexformat.Index, tree *object.Tree, fs billy.Filesystem, changes merkletrie.Changes, paths []string, contextLines int, compareIndex bool) ([]FileDiff, error) {
	entries := indexEntryMap(idx)
	var diffs []FileDiff
	for _, change := range changes {
		action, err := change.Action()
		if err != nil {
			return nil, err
		}
		var path string
		switch action {
		case merkletrie.Delete:
			path = change.From.String()
		default:
			path = change.To.String()
		}
		if !matchesDiffPaths(path, "", paths) {
			continue
		}

		var oldContent []byte
		var newContent []byte
		status := "modified"
		switch action {
		case merkletrie.Insert:
			status = "added"
			newContent, err = readFilesystemPath(fs, path)
		case merkletrie.Delete:
			status = "deleted"
			if compareIndex {
				oldContent, err = readIndexEntry(repo, entries[path])
			} else {
				oldContent, err = readTreePath(tree, path)
			}
		case merkletrie.Modify:
			if compareIndex {
				oldContent, err = readIndexEntry(repo, entries[path])
			} else {
				oldContent, err = readTreePath(tree, path)
			}
			if err == nil {
				newContent, err = readFilesystemPath(fs, path)
			}
		}
		if err != nil {
			return nil, err
		}
		diffs = append(diffs, buildFileDiff(path, "", status, oldContent, newContent, contextLines))
	}
	sort.Slice(diffs, func(i, j int) bool { return diffs[i].Path < diffs[j].Path })
	return diffs, nil
}

func buildTreeBackedDiffs(changes object.Changes, paths []string, contextLines int) ([]FileDiff, error) {
	var diffs []FileDiff
	for _, change := range changes {
		action, err := change.Action()
		if err != nil {
			return nil, err
		}
		path := change.To.Name
		oldPath := change.From.Name
		if path == "" {
			path = oldPath
		}
		if !matchesDiffPaths(path, oldPath, paths) {
			continue
		}
		fromFile, toFile, err := change.Files()
		if err != nil {
			return nil, err
		}
		var oldContent []byte
		var newContent []byte
		if fromFile != nil {
			oldContent, err = readBlobContent(fromFile)
			if err != nil {
				return nil, err
			}
		}
		if toFile != nil {
			newContent, err = readBlobContent(toFile)
			if err != nil {
				return nil, err
			}
		}
		status := "modified"
		switch action {
		case merkletrie.Insert:
			status = "added"
		case merkletrie.Delete:
			status = "deleted"
		}
		if oldPath != "" && path != oldPath && status == "modified" {
			status = "renamed"
		}
		diffs = append(diffs, buildFileDiff(path, oldPath, status, oldContent, newContent, contextLines))
	}
	sort.Slice(diffs, func(i, j int) bool { return diffs[i].Path < diffs[j].Path })
	return diffs, nil
}

func readTreePath(tree *object.Tree, path string) ([]byte, error) {
	if tree == nil {
		return nil, nil
	}
	file, err := tree.File(path)
	if err != nil {
		return nil, nil
	}
	return readBlobContent(file)
}

func readBlobContent(file interface{ Reader() (io.ReadCloser, error) }) ([]byte, error) {
	reader, err := file.Reader()
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	return io.ReadAll(reader)
}

func readIndexEntry(repo *gitrepo.Repository, entry *indexformat.Entry) ([]byte, error) {
	if entry == nil {
		return nil, nil
	}
	blob, err := repo.BlobObject(entry.Hash)
	if err != nil {
		return nil, err
	}
	reader, err := blob.Reader()
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	return io.ReadAll(reader)
}

func readFilesystemPath(fs billy.Filesystem, path string) ([]byte, error) {
	file, err := fs.Open(path)
	if err != nil {
		return nil, nil
	}
	defer func() { _ = file.Close() }()
	return io.ReadAll(file)
}

func indexEntryMap(idx *indexformat.Index) map[string]*indexformat.Entry {
	entries := make(map[string]*indexformat.Entry, len(idx.Entries))
	for _, entry := range idx.Entries {
		entries[entry.Name] = entry
	}
	return entries
}

func matchesDiffPaths(path, oldPath string, paths []string) bool {
	if len(paths) == 0 {
		return true
	}
	for _, candidate := range paths {
		if candidate == path || candidate == oldPath {
			return true
		}
	}
	return false
}

func matchesPaths(path string, paths []string) bool {
	for _, candidate := range paths {
		if candidate == path || strings.HasPrefix(path, candidate+"/") {
			return true
		}
	}
	return false
}

func buildFileDiff(path, oldPath, status string, oldContent, newContent []byte, contextLines int) FileDiff {
	binary := isBinaryContent(oldContent) || isBinaryContent(newContent)
	patch := buildUnifiedPatch(path, oldPath, status, oldContent, newContent, contextLines, binary)
	additions, deletions := countPatchLines(patch)
	return FileDiff{Path: path, OldPath: oldPath, Status: status, Binary: binary, Additions: additions, Deletions: deletions, Patch: patch}
}

func buildUnifiedPatch(path, oldPath, status string, oldContent, newContent []byte, contextLines int, binary bool) string {
	oldFile := fmt.Sprintf("a/%s", path)
	newFile := fmt.Sprintf("b/%s", path)
	if oldPath != "" {
		oldFile = fmt.Sprintf("a/%s", oldPath)
	}
	if status == "added" {
		oldFile = "/dev/null"
	}
	if status == "deleted" {
		newFile = "/dev/null"
	}

	if binary {
		var lines []string
		lines = append(lines, fmt.Sprintf("diff --git %s %s", oldFile, newFile))
		switch status {
		case "added":
			lines = append(lines, "new file mode 100644")
		case "deleted":
			lines = append(lines, "deleted file mode 100644")
		case "renamed":
			if oldPath != "" {
				lines = append(lines, fmt.Sprintf("rename from %s", oldPath))
				lines = append(lines, fmt.Sprintf("rename to %s", path))
			}
		}
		lines = append(lines, fmt.Sprintf("Binary files %s and %s differ", oldFile, newFile))
		return strings.Join(lines, "\n")
	}

	ud := difflib.UnifiedDiff{A: difflib.SplitLines(string(oldContent)), B: difflib.SplitLines(string(newContent)), FromFile: oldFile, ToFile: newFile, Context: contextLines}
	body, _ := difflib.GetUnifiedDiffString(ud)
	body = strings.TrimRight(body, "\n")
	var lines []string
	lines = append(lines, fmt.Sprintf("diff --git %s %s", oldFile, newFile))
	switch status {
	case "added":
		lines = append(lines, "new file mode 100644")
	case "deleted":
		lines = append(lines, "deleted file mode 100644")
	case "renamed":
		if oldPath != "" {
			lines = append(lines, fmt.Sprintf("rename from %s", oldPath))
			lines = append(lines, fmt.Sprintf("rename to %s", path))
		}
	}
	if body != "" {
		lines = append(lines, strings.Split(body, "\n")...)
	}
	return strings.Join(lines, "\n")
}

func countPatchLines(patch string) (int, int) {
	additions := 0
	deletions := 0
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			additions++
		}
		if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			deletions++
		}
	}
	return additions, deletions
}

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

func statusCodeToString(code gitrepo.StatusCode) string {
	switch code {
	case gitrepo.Added:
		return "added"
	case gitrepo.Modified:
		return "modified"
	case gitrepo.Deleted:
		return "deleted"
	case gitrepo.Renamed:
		return "renamed"
	case gitrepo.Copied:
		return "copied"
	case gitrepo.UpdatedButUnmerged:
		return "unmerged"
	default:
		return "unknown"
	}
}

func (p *LocalProvider) getCommit(_ context.Context, repo *gitrepo.Repository, ref string) (*Commit, error) {
	hash, _, err := p.resolveCommit(repo, ref)
	if err != nil {
		return nil, err
	}
	commit, err := repo.CommitObject(hash)
	if err != nil {
		return nil, err
	}
	mapped := mapCommit(commit)
	return &mapped, nil
}

func mapCommit(commit *object.Commit) Commit {
	parents := make([]string, 0, len(commit.ParentHashes))
	for _, parent := range commit.ParentHashes {
		parents = append(parents, parent.String())
	}
	short := commit.Hash.String()
	if len(short) > 7 {
		short = short[:7]
	}
	return Commit{SHA: commit.Hash.String(), ShortSHA: short, Message: strings.TrimSuffix(commit.Message, "\n"), Author: commit.Author.Name, AuthorEmail: commit.Author.Email, AuthorDate: commit.Author.When, Committer: commit.Committer.Name, CommitDate: commit.Committer.When, Parents: parents}
}

func (p *LocalProvider) loadCommitterSignature(repo *gitrepo.Repository) *object.Signature {
	cfg, err := repo.Config()
	if err != nil {
		return nil
	}
	if cfg.Committer.Name != "" && cfg.Committer.Email != "" {
		return &object.Signature{Name: cfg.Committer.Name, Email: cfg.Committer.Email, When: time.Now()}
	}
	if cfg.User.Name != "" && cfg.User.Email != "" {
		return &object.Signature{Name: cfg.User.Name, Email: cfg.User.Email, When: time.Now()}
	}
	name, email := p.GetUserConfig(context.Background())
	if name == "" || email == "" {
		return nil
	}
	return &object.Signature{Name: name, Email: email, When: time.Now()}
}

type replayState struct {
	workDir string
	files   map[string]*replayFileState
}

type replayFileState struct {
	exists  bool
	content []byte
	loaded  bool
}

func newReplayState(workDir string) *replayState {
	return &replayState{workDir: workDir, files: map[string]*replayFileState{}}
}

func (s *replayState) read(path string) (*replayFileState, error) {
	if file, ok := s.files[path]; ok {
		return file, nil
	}
	fullPath := filepath.Join(s.workDir, path)
	data, err := os.ReadFile(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			state := &replayFileState{exists: false, loaded: true}
			s.files[path] = state
			return state, nil
		}
		return nil, err
	}
	state := &replayFileState{exists: true, content: append([]byte(nil), data...), loaded: true}
	s.files[path] = state
	return state, nil
}

func (s *replayState) ValidateAndApply(changes []commitFileChange) error {
	for _, change := range changes {
		if err := s.validate(change); err != nil {
			return err
		}
	}
	for _, change := range changes {
		s.apply(change)
	}
	return nil
}

func (s *replayState) validate(change commitFileChange) error {
	switch change.Status {
	case "added":
		state, err := s.read(change.Path)
		if err != nil {
			return err
		}
		if state.exists {
			return fmt.Errorf("%s already exists", change.Path)
		}
	case "modified", "deleted":
		state, err := s.read(change.Path)
		if err != nil {
			return err
		}
		if !state.exists {
			return fmt.Errorf("%s does not exist", change.Path)
		}
		if !bytes.Equal(state.content, change.PreviousContent) {
			return fmt.Errorf("%s does not match expected preimage", change.Path)
		}
	case "renamed":
		oldState, err := s.read(change.OldPath)
		if err != nil {
			return err
		}
		if !oldState.exists {
			return fmt.Errorf("%s does not exist", change.OldPath)
		}
		if !bytes.Equal(oldState.content, change.PreviousContent) {
			return fmt.Errorf("%s does not match expected preimage", change.OldPath)
		}
		newState, err := s.read(change.Path)
		if err != nil {
			return err
		}
		if newState.exists {
			return fmt.Errorf("%s already exists", change.Path)
		}
	default:
		return fmt.Errorf("unsupported replay change status %q", change.Status)
	}
	return nil
}

func (s *replayState) apply(change commitFileChange) {
	switch change.Status {
	case "added", "modified":
		s.files[change.Path] = &replayFileState{exists: true, content: append([]byte(nil), change.Content...), loaded: true}
	case "deleted":
		s.files[change.Path] = &replayFileState{exists: false, loaded: true}
	case "renamed":
		s.files[change.OldPath] = &replayFileState{exists: false, loaded: true}
		s.files[change.Path] = &replayFileState{exists: true, content: append([]byte(nil), change.Content...), loaded: true}
	}
}

type replayRollback struct {
	repo          *gitrepo.Repository
	worktree      *gitrepo.Worktree
	workDir       string
	index         *indexformat.Index
	headRef       *plumbing.Reference
	branchRef     *plumbing.Reference
	fileSnapshots map[string]*replayFileSnapshot
}

type replayFileSnapshot struct {
	exists  bool
	content []byte
}

func snapshotReplayRollback(repo *gitrepo.Repository, wt *gitrepo.Worktree, workDir string, bundle *commitReplayBundle) (*replayRollback, error) {
	originalIndex, err := repo.Storer.Index()
	if err != nil {
		return nil, err
	}
	rollback := &replayRollback{repo: repo, worktree: wt, workDir: workDir, index: cloneIndex(originalIndex), fileSnapshots: map[string]*replayFileSnapshot{}}
	if head, err := repo.Storer.Reference(plumbing.HEAD); err == nil {
		rollback.headRef = cloneReference(head)
		if head.Type() == plumbing.SymbolicReference {
			if branch, err := repo.Storer.Reference(head.Target()); err == nil {
				rollback.branchRef = cloneReference(branch)
			}
		}
	}
	for _, commit := range bundle.Commits {
		for _, change := range commit.Changes {
			for _, path := range []string{change.Path, change.OldPath} {
				if path == "" {
					continue
				}
				if _, ok := rollback.fileSnapshots[path]; ok {
					continue
				}
				data, err := os.ReadFile(filepath.Join(workDir, path))
				if err != nil {
					if os.IsNotExist(err) {
						rollback.fileSnapshots[path] = &replayFileSnapshot{exists: false}
						continue
					}
					return nil, err
				}
				rollback.fileSnapshots[path] = &replayFileSnapshot{exists: true, content: data}
			}
		}
	}
	return rollback, nil
}

func (r *replayRollback) Restore() error {
	for path, snapshot := range r.fileSnapshots {
		fullPath := filepath.Join(r.workDir, path)
		if snapshot.exists {
			if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
				return err
			}
			if err := os.WriteFile(fullPath, snapshot.content, 0644); err != nil {
				return err
			}
		} else if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if r.index != nil {
		if err := r.repo.Storer.SetIndex(cloneIndex(r.index)); err != nil {
			return err
		}
	}
	if r.branchRef != nil {
		if err := r.repo.Storer.SetReference(cloneReference(r.branchRef)); err != nil {
			return err
		}
	}
	if r.headRef != nil {
		if err := r.repo.Storer.SetReference(cloneReference(r.headRef)); err != nil {
			return err
		}
	}
	return nil
}

func cloneIndex(idx *indexformat.Index) *indexformat.Index {
	if idx == nil {
		return nil
	}
	cloned := &indexformat.Index{Version: idx.Version}
	cloned.Entries = make([]*indexformat.Entry, len(idx.Entries))
	for i, entry := range idx.Entries {
		copyEntry := *entry
		cloned.Entries[i] = &copyEntry
	}
	return cloned
}

func cloneReference(ref *plumbing.Reference) *plumbing.Reference {
	if ref == nil {
		return nil
	}
	if ref.Type() == plumbing.SymbolicReference {
		return plumbing.NewSymbolicReference(ref.Name(), ref.Target())
	}
	return plumbing.NewHashReference(ref.Name(), ref.Hash())
}

func applyReplayChange(workDir string, change commitFileChange) error {
	switch change.Status {
	case "added", "modified":
		fullPath := filepath.Join(workDir, change.Path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return err
		}
		return os.WriteFile(fullPath, change.Content, 0644)
	case "deleted":
		if err := os.Remove(filepath.Join(workDir, change.Path)); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	case "renamed":
		if err := os.Remove(filepath.Join(workDir, change.OldPath)); err != nil && !os.IsNotExist(err) {
			return err
		}
		fullPath := filepath.Join(workDir, change.Path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return err
		}
		return os.WriteFile(fullPath, change.Content, 0644)
	default:
		return fmt.Errorf("unsupported replay change status %q", change.Status)
	}
}

func stagePaths(wt *gitrepo.Worktree, paths []string) error {
	seen := map[string]struct{}{}
	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		if _, err := wt.Add(path); err != nil {
			return err
		}
	}
	return nil
}

// Ensure LocalProvider implements Provider.
var _ Provider = (*LocalProvider)(nil)
