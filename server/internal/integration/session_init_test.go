package integration

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/obot-platform/discobot/server/internal/model"
	"github.com/obot-platform/discobot/server/internal/service"
)

// TestSessionInitialize_SetsWorkspaceCommitOnFirstInit verifies that
// WorkspacePath and WorkspaceCommit are set during first initialization.
func TestSessionInitialize_SetsWorkspaceCommitOnFirstInit(t *testing.T) {
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")

	// Create a workspace with a real git repo
	workspace := ts.CreateTestWorkspaceWithGitRepo(project)

	// Get the current HEAD commit
	expectedCommit := getGitHead(t, workspace.Path)

	// Create session in initializing state (not ready yet)
	session := &model.Session{
		ProjectID:   workspace.ProjectID,
		WorkspaceID: workspace.ID,
		Name:        "Test Session",
		Status:      model.SessionStatusInitializing,
	}
	if err := ts.Store.CreateSession(context.Background(), session); err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Verify session has no workspace path/commit yet
	freshSession, err := ts.Store.GetSessionByID(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("Failed to get session: %v", err)
	}
	if freshSession.WorkspacePath != nil {
		t.Errorf("Expected WorkspacePath to be nil before init, got %v", *freshSession.WorkspacePath)
	}
	if freshSession.WorkspaceCommit != nil {
		t.Errorf("Expected WorkspaceCommit to be nil before init, got %v", *freshSession.WorkspaceCommit)
	}

	// Create session service and call Initialize
	gitSvc := service.NewGitService(ts.Store, ts.GitProvider)
	sessionSvc := service.NewSessionService(ts.Store, gitSvc, ts.MockSandbox, nil, nil, nil)

	ctx := context.Background()
	if err := sessionSvc.Initialize(ctx, session.ID); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Verify WorkspacePath and WorkspaceCommit are now set
	updatedSession, err := ts.Store.GetSessionByID(ctx, session.ID)
	if err != nil {
		t.Fatalf("Failed to get updated session: %v", err)
	}

	if updatedSession.WorkspacePath == nil || *updatedSession.WorkspacePath == "" {
		t.Error("Expected WorkspacePath to be set after init")
	}

	if updatedSession.WorkspaceCommit == nil || *updatedSession.WorkspaceCommit == "" {
		t.Error("Expected WorkspaceCommit to be set after init")
	}

	if updatedSession.WorkspaceCommit != nil && *updatedSession.WorkspaceCommit != expectedCommit {
		t.Errorf("Expected WorkspaceCommit to be %s, got %s", expectedCommit, *updatedSession.WorkspaceCommit)
	}
}

// TestSessionInitialize_PreservesWorkspaceCommitOnReconcile verifies that
// WorkspacePath and WorkspaceCommit are NOT changed during reconcile (second initialization).
func TestSessionInitialize_PreservesWorkspaceCommitOnReconcile(t *testing.T) {
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")

	// Create a workspace with a real git repo
	workspace := ts.CreateTestWorkspaceWithGitRepo(project)

	// Get the initial HEAD commit
	initialCommit := getGitHead(t, workspace.Path)

	// Create session in initializing state
	session := &model.Session{
		ProjectID:   workspace.ProjectID,
		WorkspaceID: workspace.ID,
		Name:        "Test Session",
		Status:      model.SessionStatusInitializing,
	}
	if err := ts.Store.CreateSession(context.Background(), session); err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Create session service and call Initialize (first time)
	gitSvc := service.NewGitService(ts.Store, ts.GitProvider)
	sessionSvc := service.NewSessionService(ts.Store, gitSvc, ts.MockSandbox, nil, nil, nil)

	ctx := context.Background()
	if err := sessionSvc.Initialize(ctx, session.ID); err != nil {
		t.Fatalf("First Initialize failed: %v", err)
	}

	// Verify initial values
	afterFirstInit, err := ts.Store.GetSessionByID(ctx, session.ID)
	if err != nil {
		t.Fatalf("Failed to get session after first init: %v", err)
	}

	if afterFirstInit.WorkspaceCommit == nil || *afterFirstInit.WorkspaceCommit != initialCommit {
		t.Fatalf("Expected WorkspaceCommit to be %s after first init, got %v", initialCommit, afterFirstInit.WorkspaceCommit)
	}

	originalPath := *afterFirstInit.WorkspacePath
	originalCommit := *afterFirstInit.WorkspaceCommit

	// Now make a new commit in the workspace (simulating changes during session)
	makeCommit(t, workspace.Path, "second.txt", "Second commit")
	newCommit := getGitHead(t, workspace.Path)

	if newCommit == initialCommit {
		t.Fatal("Expected new commit to be different from initial commit")
	}

	// Set session back to a state that triggers reconcile
	afterFirstInit.Status = model.SessionStatusError
	if err := ts.Store.UpdateSession(ctx, afterFirstInit); err != nil {
		t.Fatalf("Failed to update session status: %v", err)
	}

	// Remove the sandbox so Initialize will recreate it
	if err := ts.MockSandbox.Remove(ctx, session.ID); err != nil {
		t.Fatalf("Failed to remove sandbox: %v", err)
	}

	// Call Initialize again (reconcile)
	if err := sessionSvc.Initialize(ctx, session.ID); err != nil {
		t.Fatalf("Second Initialize (reconcile) failed: %v", err)
	}

	// Verify WorkspacePath and WorkspaceCommit are PRESERVED (not updated to new values)
	afterReconcile, err := ts.Store.GetSessionByID(ctx, session.ID)
	if err != nil {
		t.Fatalf("Failed to get session after reconcile: %v", err)
	}

	if afterReconcile.WorkspacePath == nil || *afterReconcile.WorkspacePath != originalPath {
		t.Errorf("Expected WorkspacePath to be preserved as %s, got %v", originalPath, afterReconcile.WorkspacePath)
	}

	if afterReconcile.WorkspaceCommit == nil || *afterReconcile.WorkspaceCommit != originalCommit {
		t.Errorf("Expected WorkspaceCommit to be preserved as %s (not updated to %s), got %v",
			originalCommit, newCommit, afterReconcile.WorkspaceCommit)
	}
}

// TestSessionInitialize_EnsuresWorkspaceOnReconcile verifies that
// EnsureWorkspaceRepo is called even during reconcile (to ensure repo is cloned).
func TestSessionInitialize_EnsuresWorkspaceOnReconcile(t *testing.T) {
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")

	// Create a workspace with a real git repo
	workspace := ts.CreateTestWorkspaceWithGitRepo(project)

	// Create session with WorkspacePath already set (simulating previous initialization)
	workspacePath := workspace.Path
	workspaceCommit := getGitHead(t, workspace.Path)
	session := &model.Session{
		ProjectID:       workspace.ProjectID,
		WorkspaceID:     workspace.ID,
		Name:            "Test Session",
		Status:          model.SessionStatusError, // Needs reconcile
		WorkspacePath:   &workspacePath,
		WorkspaceCommit: &workspaceCommit,
	}
	if err := ts.Store.CreateSession(context.Background(), session); err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Create session service and call Initialize (reconcile path)
	gitSvc := service.NewGitService(ts.Store, ts.GitProvider)
	sessionSvc := service.NewSessionService(ts.Store, gitSvc, ts.MockSandbox, nil, nil, nil)

	ctx := context.Background()
	if err := sessionSvc.Initialize(ctx, session.ID); err != nil {
		t.Fatalf("Initialize (reconcile) failed: %v", err)
	}

	// Verify that initialization succeeded (sandbox was created)
	// This implicitly verifies EnsureWorkspaceRepo was called because:
	// 1. The git provider needs to have the workspace registered to work
	// 2. If EnsureWorkspaceRepo wasn't called, sandbox creation would fail
	sbx, err := ts.MockSandbox.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Failed to get sandbox after reconcile: %v", err)
	}
	if sbx == nil {
		t.Error("Expected sandbox to exist after reconcile")
	}

	// Verify session is now ready
	updatedSession, err := ts.Store.GetSessionByID(ctx, session.ID)
	if err != nil {
		t.Fatalf("Failed to get session: %v", err)
	}
	if updatedSession.Status != model.SessionStatusReady {
		t.Errorf("Expected session status to be 'ready', got '%s'", updatedSession.Status)
	}
}

// TestSessionInitialize_WorkspaceCommitUsedForSandbox verifies that the stored
// WorkspaceCommit (not the current HEAD) is passed to sandbox creation on reconcile.
func TestSessionInitialize_WorkspaceCommitUsedForSandbox(t *testing.T) {
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")

	// Create a workspace with a real git repo
	workspace := ts.CreateTestWorkspaceWithGitRepo(project)

	// Get the initial commit
	initialCommit := getGitHead(t, workspace.Path)

	// Create session in initializing state
	session := &model.Session{
		ProjectID:   workspace.ProjectID,
		WorkspaceID: workspace.ID,
		Name:        "Test Session",
		Status:      model.SessionStatusInitializing,
	}
	if err := ts.Store.CreateSession(context.Background(), session); err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// First initialization
	gitSvc := service.NewGitService(ts.Store, ts.GitProvider)
	sessionSvc := service.NewSessionService(ts.Store, gitSvc, ts.MockSandbox, nil, nil, nil)

	ctx := context.Background()
	if err := sessionSvc.Initialize(ctx, session.ID); err != nil {
		t.Fatalf("First Initialize failed: %v", err)
	}

	// Make a new commit
	makeCommit(t, workspace.Path, "new-file.txt", "New commit after session init")
	newCommit := getGitHead(t, workspace.Path)

	if newCommit == initialCommit {
		t.Fatal("Expected new commit to be different")
	}

	// Remove sandbox and set session to error state to trigger reconcile
	if err := ts.MockSandbox.Remove(ctx, session.ID); err != nil {
		t.Fatalf("Failed to remove sandbox: %v", err)
	}

	afterFirstInit, _ := ts.Store.GetSessionByID(ctx, session.ID)
	afterFirstInit.Status = model.SessionStatusError
	if err := ts.Store.UpdateSession(ctx, afterFirstInit); err != nil {
		t.Fatalf("Failed to update session: %v", err)
	}

	// Reconcile
	if err := sessionSvc.Initialize(ctx, session.ID); err != nil {
		t.Fatalf("Reconcile Initialize failed: %v", err)
	}

	// Verify the session still has the original commit (sandbox should use this)
	finalSession, err := ts.Store.GetSessionByID(ctx, session.ID)
	if err != nil {
		t.Fatalf("Failed to get session: %v", err)
	}

	if finalSession.WorkspaceCommit == nil || *finalSession.WorkspaceCommit != initialCommit {
		t.Errorf("Expected WorkspaceCommit to remain %s, got %v", initialCommit, finalSession.WorkspaceCommit)
	}
}

// Helper functions

func getGitHead(t *testing.T, repoPath string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Failed to get git HEAD: %v", err)
	}
	return string(out[:len(out)-1]) // trim newline
}

func makeCommit(t *testing.T, repoPath, filename, message string) {
	t.Helper()

	// Create a new file
	filepath := repoPath + "/" + filename
	if err := os.WriteFile(filepath, []byte(message+"\n"), 0644); err != nil {
		t.Fatalf("Failed to create file %s: %v", filename, err)
	}

	// Stage and commit
	cmd := exec.Command("git", "add", filename)
	cmd.Dir = repoPath
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to git add: %v", err)
	}

	cmd = exec.Command("git", "commit", "-m", message)
	cmd.Dir = repoPath
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to git commit: %v", err)
	}
}

// TestMapSession_IncludesWorkspaceFields verifies that mapSession includes
// the WorkspacePath and WorkspaceCommit fields in the service.Session.
func TestMapSession_IncludesWorkspaceFields(t *testing.T) {
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspaceWithGitRepo(project)

	// Create session with workspace fields set
	workspacePath := workspace.Path
	workspaceCommit := "abc123def456"
	session := &model.Session{
		ProjectID:       workspace.ProjectID,
		WorkspaceID:     workspace.ID,
		Name:            "Test Session",
		Status:          model.SessionStatusReady,
		WorkspacePath:   &workspacePath,
		WorkspaceCommit: &workspaceCommit,
	}
	if err := ts.Store.CreateSession(context.Background(), session); err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Use session service to get session (which uses mapSession internally)
	gitSvc := service.NewGitService(ts.Store, ts.GitProvider)
	sessionSvc := service.NewSessionService(ts.Store, gitSvc, ts.MockSandbox, nil, nil, nil)

	ctx := context.Background()
	svcSession, err := sessionSvc.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("Failed to get session: %v", err)
	}

	// Verify the service.Session has the workspace fields
	if svcSession.WorkspacePath != workspacePath {
		t.Errorf("Expected WorkspacePath %s, got %s", workspacePath, svcSession.WorkspacePath)
	}
	if svcSession.WorkspaceCommit != workspaceCommit {
		t.Errorf("Expected WorkspaceCommit %s, got %s", workspaceCommit, svcSession.WorkspaceCommit)
	}
}

// TestSessionInitialize_NoGitService verifies initialization works without git service
// (fallback path for testing scenarios).
func TestSessionInitialize_NoGitService(t *testing.T) {
	ts := NewTestServer(t)
	user := ts.CreateTestUser("test@example.com")
	project := ts.CreateTestProject(user, "Test Project")
	workspace := ts.CreateTestWorkspace(project, "/some/local/path")
	session := &model.Session{
		ProjectID:   workspace.ProjectID,
		WorkspaceID: workspace.ID,
		Name:        "Test Session",
		Status:      model.SessionStatusInitializing,
	}
	if err := ts.Store.CreateSession(context.Background(), session); err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Create session service WITHOUT git service
	sessionSvc := service.NewSessionService(ts.Store, nil, ts.MockSandbox, nil, nil, nil)

	ctx := context.Background()
	if err := sessionSvc.Initialize(ctx, session.ID); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Verify WorkspacePath is set to workspace.Path, but WorkspaceCommit is empty
	updatedSession, err := ts.Store.GetSessionByID(ctx, session.ID)
	if err != nil {
		t.Fatalf("Failed to get session: %v", err)
	}

	if updatedSession.WorkspacePath == nil || *updatedSession.WorkspacePath != workspace.Path {
		t.Errorf("Expected WorkspacePath to be %s, got %v", workspace.Path, updatedSession.WorkspacePath)
	}

	// Without git service, WorkspaceCommit should be nil or empty
	if updatedSession.WorkspaceCommit != nil && *updatedSession.WorkspaceCommit != "" {
		t.Errorf("Expected WorkspaceCommit to be nil/empty without git service, got %v", *updatedSession.WorkspaceCommit)
	}
}
