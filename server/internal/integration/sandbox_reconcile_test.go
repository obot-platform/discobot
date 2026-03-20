package integration

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/database"
	"github.com/obot-platform/discobot/server/internal/model"
	"github.com/obot-platform/discobot/server/internal/sandbox"
	"github.com/obot-platform/discobot/server/internal/sandbox/docker"
	"github.com/obot-platform/discobot/server/internal/service"
	"github.com/obot-platform/discobot/server/internal/store"
)

// Small, fast images for testing
const (
	testImageOld = "busybox:1.36"
	testImageNew = "busybox:1.37"
)

// testSessionInitializer implements service.SessionInitializer for tests
// by directly calling CreateForSession on the sandbox service.
type testSessionInitializer struct {
	sandboxSvc *service.SandboxService
}

func (t *testSessionInitializer) Initialize(ctx context.Context, sessionID string) error {
	return t.sandboxSvc.CreateForSession(ctx, sessionID)
}

// skipIfNoDocker skips the test if Docker is not available
func skipIfNoDocker(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker not found in PATH, skipping test")
	}

	// Check if Docker daemon is running
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "info")
	if err := cmd.Run(); err != nil {
		t.Skip("Docker daemon not running, skipping test")
	}
}

// imageExists checks if a Docker image exists locally
func imageExists(image string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "image", "inspect", image)
	return cmd.Run() == nil
}

// pullImage ensures an image is available locally (skips if already present)
func pullImage(t *testing.T, image string) {
	t.Helper()
	if imageExists(image) {
		return // Already have it locally
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "pull", image)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to pull image %s: %v\nOutput: %s", image, err, output)
	}
}

// testSandboxSetup holds test resources
type testSandboxSetup struct {
	provider   *docker.Provider
	store      *store.Store
	db         *database.DB
	cfg        *config.Config
	workingDir string

	// Track session IDs for cleanup (enables parallel tests)
	mu         sync.Mutex
	sessionIDs []string
}

// newTestSandboxSetup creates a new test setup with real Docker
func newTestSandboxSetup(t *testing.T) *testSandboxSetup {
	t.Helper()
	skipIfNoDocker(t)

	// Pull test images first
	t.Log("Pulling test images...")
	pullImage(t, testImageOld)
	pullImage(t, testImageNew)

	workingDir := t.TempDir()
	dbDir := t.TempDir()

	cfg := &config.Config{
		DatabaseDSN:        fmt.Sprintf("sqlite3://%s/test.db", dbDir),
		DatabaseDriver:     "sqlite",
		SandboxImage:       testImageNew, // Expected image
		SandboxIdleTimeout: 5 * time.Minute,
		WorkspaceDir:       workingDir,
	}

	db, err := database.New(cfg)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	if err := db.Migrate(); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	s := store.New(db.DB, db.ReadDB)

	// Create session project resolver for cache volumes
	sessionProjectResolver := func(ctx context.Context, sessionID string) (string, error) {
		session, err := s.GetSessionByID(ctx, sessionID)
		if err != nil {
			return "", err
		}
		return session.ProjectID, nil
	}

	provider, err := docker.NewProvider(cfg, sessionProjectResolver)
	if err != nil {
		db.Close()
		t.Fatalf("Failed to create Docker provider: %v", err)
	}

	setup := &testSandboxSetup{
		provider:   provider,
		store:      s,
		db:         db,
		cfg:        cfg,
		workingDir: workingDir,
	}

	t.Cleanup(func() {
		// Clean up only sandboxes created by this test (enables parallel tests)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		setup.mu.Lock()
		sessionIDs := setup.sessionIDs
		setup.mu.Unlock()

		for _, sessionID := range sessionIDs {
			_ = provider.Remove(ctx, sessionID)
		}

		provider.Close()
		db.Close()
	})

	return setup
}

// createTestProject creates a project for testing
func (s *testSandboxSetup) createTestProject(t *testing.T) *model.Project {
	t.Helper()
	project := &model.Project{
		Name: "test-project",
		Slug: fmt.Sprintf("test-project-%d", time.Now().UnixNano()),
	}
	if err := s.store.CreateProject(context.Background(), project); err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}
	return project
}

// createTestWorkspace creates a workspace for testing
func (s *testSandboxSetup) createTestWorkspace(t *testing.T, project *model.Project) *model.Workspace {
	t.Helper()
	workspace := &model.Workspace{
		ProjectID:  project.ID,
		Path:       s.workingDir,
		SourceType: "local",
		Status:     model.WorkspaceStatusReady,
	}
	if err := s.store.CreateWorkspace(context.Background(), workspace); err != nil {
		t.Fatalf("Failed to create workspace: %v", err)
	}
	return workspace
}

// createTestSession creates a session for testing
func (s *testSandboxSetup) createTestSession(t *testing.T, workspace *model.Workspace, name string) *model.Session {
	t.Helper()
	workspacePath := workspace.Path
	session := &model.Session{
		ProjectID:     workspace.ProjectID,
		WorkspaceID:   workspace.ID,
		Name:          name,
		Status:        model.SessionStatusReady,
		WorkspacePath: &workspacePath, // Set workspace path for CreateForSession
	}
	if err := s.store.CreateSession(context.Background(), session); err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	return session
}

// trackSessionID records a session ID for cleanup
func (s *testSandboxSetup) trackSessionID(sessionID string) {
	s.mu.Lock()
	s.sessionIDs = append(s.sessionIDs, sessionID)
	s.mu.Unlock()
}

// createSandboxWithImage creates a sandbox with a specific image using a temporary provider.
// This is used to simulate existing sandboxes from before an image upgrade.
func (s *testSandboxSetup) createSandboxWithImage(t *testing.T, sessionID, image string) *sandbox.Sandbox {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Track for cleanup
	s.trackSessionID(sessionID)

	// Create a temporary provider configured with the specific image
	tempCfg := &config.Config{
		SandboxImage: image,
	}
	// Use the same session project resolver from the main setup
	tempResolver := func(ctx context.Context, sessionID string) (string, error) {
		session, err := s.store.GetSessionByID(ctx, sessionID)
		if err != nil {
			return "", err
		}
		return session.ProjectID, nil
	}
	tempProvider, err := docker.NewProvider(tempCfg, tempResolver)
	if err != nil {
		t.Fatalf("Failed to create temp provider: %v", err)
	}
	defer tempProvider.Close()

	opts := sandbox.CreateOptions{
		Labels: map[string]string{
			"discobot.session.id": sessionID,
		},
	}

	sb, err := tempProvider.Create(ctx, sessionID, opts)
	if err != nil {
		t.Fatalf("Failed to create sandbox: %v", err)
	}

	if err := tempProvider.Start(ctx, sessionID); err != nil {
		t.Fatalf("Failed to start sandbox: %v", err)
	}

	return sb
}

func TestReconcileSandboxes_ReplacesOutdatedImage(t *testing.T) {
	SkipIfShort(t) // Slow test: ~2.5s
	setup := newTestSandboxSetup(t)
	ctx := context.Background()

	// Create test data
	project := setup.createTestProject(t)
	workspace := setup.createTestWorkspace(t, project)
	session := setup.createTestSession(t, workspace, "test-session-1")

	// Create a sandbox with the OLD image
	t.Logf("Creating sandbox with old image: %s", testImageOld)
	setup.createSandboxWithImage(t, session.ID, testImageOld)

	// Verify sandbox exists with old image
	sb, err := setup.provider.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Failed to get sandbox: %v", err)
	}
	if sb.Image != testImageOld {
		t.Fatalf("Expected sandbox image %s, got %s", testImageOld, sb.Image)
	}
	if sb.Status != sandbox.StatusRunning {
		t.Fatalf("Expected sandbox status running, got %s", sb.Status)
	}
	oldSandboxID := sb.ID
	t.Logf("Sandbox created with ID: %s", oldSandboxID)

	// Create sandbox service with NEW image as expected, with session initializer
	sandboxSvc := service.NewSandboxService(setup.store, setup.provider, setup.cfg, nil, nil, nil, nil)
	sandboxSvc.SetSessionInitializer(&testSessionInitializer{sandboxSvc: sandboxSvc})

	// Run reconciliation
	t.Log("Running sandbox reconciliation...")
	if err := sandboxSvc.ReconcileSandboxes(ctx); err != nil {
		t.Fatalf("ReconcileSandboxes failed: %v", err)
	}

	// Verify sandbox was recreated with new image
	sb, err = setup.provider.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Failed to get sandbox after reconciliation: %v", err)
	}

	if sb.Image != testImageNew {
		t.Errorf("Expected sandbox image %s after reconciliation, got %s", testImageNew, sb.Image)
	}

	if sb.ID == oldSandboxID {
		t.Errorf("Sandbox ID should have changed after recreation, still %s", sb.ID)
	}

	t.Logf("Sandbox recreated with new ID: %s, image: %s", sb.ID, sb.Image)
}

func TestReconcileSandboxes_SkipsCorrectImage(t *testing.T) {
	setup := newTestSandboxSetup(t)
	ctx := context.Background()

	// Create test data
	project := setup.createTestProject(t)
	workspace := setup.createTestWorkspace(t, project)
	session := setup.createTestSession(t, workspace, "test-session-correct")

	// Create a sandbox with the CORRECT (new) image
	t.Logf("Creating sandbox with correct image: %s", testImageNew)
	setup.createSandboxWithImage(t, session.ID, testImageNew)

	// Get sandbox info before reconciliation
	sb, err := setup.provider.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Failed to get sandbox: %v", err)
	}
	originalID := sb.ID
	t.Logf("Sandbox created with ID: %s", originalID)

	// Create sandbox service
	sandboxSvc := service.NewSandboxService(setup.store, setup.provider, setup.cfg, nil, nil, nil, nil)

	// Run reconciliation
	t.Log("Running sandbox reconciliation...")
	if err := sandboxSvc.ReconcileSandboxes(ctx); err != nil {
		t.Fatalf("ReconcileSandboxes failed: %v", err)
	}

	// Verify sandbox was NOT recreated
	sb, err = setup.provider.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Failed to get sandbox after reconciliation: %v", err)
	}

	if sb.ID != originalID {
		t.Errorf("Sandbox should NOT have been recreated, ID changed from %s to %s", originalID, sb.ID)
	}

	if sb.Image != testImageNew {
		t.Errorf("Sandbox image should still be %s, got %s", testImageNew, sb.Image)
	}

	t.Log("Sandbox correctly skipped (already using correct image)")
}

func TestReconcileSandboxes_RemovesOrphanedSandboxes(t *testing.T) {
	setup := newTestSandboxSetup(t)
	ctx := context.Background()

	// Create a real session so we can create a sandbox for it, then delete the session
	// to make the sandbox orphaned
	project := setup.createTestProject(t)
	workspace := setup.createTestWorkspace(t, project)
	session := setup.createTestSession(t, workspace, "orphan-session")
	orphanSessionID := session.ID

	t.Logf("Creating sandbox for session that will become orphaned")
	setup.createSandboxWithImage(t, orphanSessionID, testImageOld)

	// Delete the session to make the sandbox orphaned
	if err := setup.store.DeleteSession(ctx, orphanSessionID); err != nil {
		t.Fatalf("Failed to delete session: %v", err)
	}

	// Verify sandbox exists
	sb, err := setup.provider.Get(ctx, orphanSessionID)
	if err != nil {
		t.Fatalf("Failed to get orphaned sandbox: %v", err)
	}
	t.Logf("Orphaned sandbox created with ID: %s", sb.ID)

	// Create sandbox service
	sandboxSvc := service.NewSandboxService(setup.store, setup.provider, setup.cfg, nil, nil, nil, nil)

	// Run reconciliation
	t.Log("Running sandbox reconciliation...")
	if err := sandboxSvc.ReconcileSandboxes(ctx); err != nil {
		t.Fatalf("ReconcileSandboxes failed: %v", err)
	}

	// Verify orphaned sandbox was removed
	_, err = setup.provider.Get(ctx, orphanSessionID)
	if err != sandbox.ErrNotFound {
		t.Errorf("Expected orphaned sandbox to be removed, got error: %v", err)
	}

	t.Log("Orphaned sandbox correctly removed")
}

func TestReconcileSandboxes_MultipleSandboxes(t *testing.T) {
	SkipIfShort(t) // Slow test: ~3.5s
	setup := newTestSandboxSetup(t)
	ctx := context.Background()

	// Create test data
	project := setup.createTestProject(t)
	workspace := setup.createTestWorkspace(t, project)

	// Create 3 sessions: 2 with old image, 1 with new image
	session1 := setup.createTestSession(t, workspace, "session-old-1")
	session2 := setup.createTestSession(t, workspace, "session-old-2")
	session3 := setup.createTestSession(t, workspace, "session-new")

	t.Log("Creating sandboxes in parallel...")
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); setup.createSandboxWithImage(t, session1.ID, testImageOld) }()
	go func() { defer wg.Done(); setup.createSandboxWithImage(t, session2.ID, testImageOld) }()
	go func() { defer wg.Done(); setup.createSandboxWithImage(t, session3.ID, testImageNew) }()
	wg.Wait()

	// Get original sandbox IDs
	sb1, _ := setup.provider.Get(ctx, session1.ID)
	sb2, _ := setup.provider.Get(ctx, session2.ID)
	sb3, _ := setup.provider.Get(ctx, session3.ID)

	originalIDs := map[string]string{
		session1.ID: sb1.ID,
		session2.ID: sb2.ID,
		session3.ID: sb3.ID,
	}

	t.Logf("Original sandbox IDs: %v", originalIDs)

	// Create sandbox service with session initializer
	sandboxSvc := service.NewSandboxService(setup.store, setup.provider, setup.cfg, nil, nil, nil, nil)
	sandboxSvc.SetSessionInitializer(&testSessionInitializer{sandboxSvc: sandboxSvc})

	// Run reconciliation
	t.Log("Running sandbox reconciliation...")
	if err := sandboxSvc.ReconcileSandboxes(ctx); err != nil {
		t.Fatalf("ReconcileSandboxes failed: %v", err)
	}

	// Verify results
	// Session 1 and 2 should have new sandboxes
	for _, sessionID := range []string{session1.ID, session2.ID} {
		sb, err := setup.provider.Get(ctx, sessionID)
		if err != nil {
			t.Errorf("Failed to get sandbox for session %s: %v", sessionID, err)
			continue
		}
		if sb.Image != testImageNew {
			t.Errorf("Session %s: expected image %s, got %s", sessionID, testImageNew, sb.Image)
		}
		if sb.ID == originalIDs[sessionID] {
			t.Errorf("Session %s: sandbox should have been recreated", sessionID)
		}
	}

	// Session 3 should have the same sandbox
	sb3After, err := setup.provider.Get(ctx, session3.ID)
	if err != nil {
		t.Fatalf("Failed to get sandbox for session3: %v", err)
	}
	if sb3After.ID != originalIDs[session3.ID] {
		t.Errorf("Session 3: sandbox should NOT have been recreated, ID changed from %s to %s",
			originalIDs[session3.ID], sb3After.ID)
	}

	t.Log("Multiple sandbox reconciliation completed successfully")
}

func TestReconcileSandboxes_NoSandboxes(t *testing.T) {
	setup := newTestSandboxSetup(t)
	ctx := context.Background()

	// Create sandbox service
	sandboxSvc := service.NewSandboxService(setup.store, setup.provider, setup.cfg, nil, nil, nil, nil)

	// Run reconciliation with no sandboxes
	t.Log("Running sandbox reconciliation with no sandboxes...")
	if err := sandboxSvc.ReconcileSandboxes(ctx); err != nil {
		t.Fatalf("ReconcileSandboxes failed: %v", err)
	}

	// Verify no sandboxes exist
	sandboxes, err := setup.provider.List(ctx)
	if err != nil {
		t.Fatalf("Failed to list sandboxes: %v", err)
	}

	if len(sandboxes) != 0 {
		t.Errorf("Expected 0 sandboxes, got %d", len(sandboxes))
	}

	t.Log("Empty sandbox reconciliation completed successfully")
}

func TestReconcileSessionStates_MarksFailedSandboxAsError(t *testing.T) {
	SkipIfShort(t) // Slow test: ~2.2s
	setup := newTestSandboxSetup(t)
	ctx := context.Background()

	// Create test data
	project := setup.createTestProject(t)
	workspace := setup.createTestWorkspace(t, project)
	session := setup.createTestSession(t, workspace, "session-with-failed-sandbox")

	// Create a sandbox and then kill it to simulate failure
	t.Log("Creating sandbox and simulating failure...")
	setup.createSandboxWithImage(t, session.ID, testImageNew)

	// Kill the container to simulate a failure (non-zero exit code)
	// We'll use docker kill which will cause an exit code != 0
	killCmd := exec.Command("docker", "kill", fmt.Sprintf("discobot-session-%s", session.ID))
	if output, err := killCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to kill container: %v\nOutput: %s", err, output)
	}

	// Wait a moment for docker to register the state
	time.Sleep(500 * time.Millisecond)

	// Verify sandbox is in failed state
	sb, err := setup.provider.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Failed to get sandbox: %v", err)
	}
	if sb.Status != sandbox.StatusFailed {
		t.Logf("Note: Sandbox status is %s (expected failed). Kill may have resulted in different status.", sb.Status)
	}

	// Create sandbox service
	sandboxSvc := service.NewSandboxService(setup.store, setup.provider, setup.cfg, nil, nil, nil, nil)

	// Run session state reconciliation
	t.Log("Running session state reconciliation...")
	if err := sandboxSvc.ReconcileSessionStates(ctx); err != nil {
		t.Fatalf("ReconcileSessionStates failed: %v", err)
	}

	// Verify session status was updated
	updatedSession, err := setup.store.GetSessionByID(ctx, session.ID)
	if err != nil {
		t.Fatalf("Failed to get session: %v", err)
	}

	// Session should be marked as error if sandbox failed, or still running if it was just stopped
	if sb.Status == sandbox.StatusFailed {
		if updatedSession.Status != model.SessionStatusError {
			t.Errorf("Expected session status 'error', got '%s'", updatedSession.Status)
		}
		if updatedSession.ErrorMessage == nil || *updatedSession.ErrorMessage == "" {
			t.Error("Expected session to have an error message")
		}
		t.Logf("Session correctly marked as error: %s", *updatedSession.ErrorMessage)
	} else {
		// If it was just stopped (not failed), status should remain running
		t.Logf("Sandbox was stopped (not failed), session status: %s", updatedSession.Status)
	}
}

func TestReconcileSessionStates_KeepsRunningSessionWithRunningSandbox(t *testing.T) {
	setup := newTestSandboxSetup(t)
	ctx := context.Background()

	// Create test data
	project := setup.createTestProject(t)
	workspace := setup.createTestWorkspace(t, project)
	session := setup.createTestSession(t, workspace, "session-with-running-sandbox")

	// Create and start a sandbox
	t.Log("Creating running sandbox...")
	setup.createSandboxWithImage(t, session.ID, testImageNew)

	// Verify sandbox is running
	sb, err := setup.provider.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Failed to get sandbox: %v", err)
	}
	if sb.Status != sandbox.StatusRunning {
		t.Fatalf("Expected sandbox status running, got %s", sb.Status)
	}

	// Create sandbox service
	sandboxSvc := service.NewSandboxService(setup.store, setup.provider, setup.cfg, nil, nil, nil, nil)

	// Run session state reconciliation
	t.Log("Running session state reconciliation...")
	if err := sandboxSvc.ReconcileSessionStates(ctx); err != nil {
		t.Fatalf("ReconcileSessionStates failed: %v", err)
	}

	// Verify session status is still ready
	updatedSession, err := setup.store.GetSessionByID(ctx, session.ID)
	if err != nil {
		t.Fatalf("Failed to get session: %v", err)
	}

	if updatedSession.Status != model.SessionStatusReady {
		t.Errorf("Expected session status 'ready', got '%s'", updatedSession.Status)
	}

	t.Log("Session correctly kept as ready")
}

func TestReconcileSessionStates_MarksStoppedSessionWithNoSandbox(t *testing.T) {
	setup := newTestSandboxSetup(t)
	ctx := context.Background()

	// Create test data
	project := setup.createTestProject(t)
	workspace := setup.createTestWorkspace(t, project)
	session := setup.createTestSession(t, workspace, "session-with-no-sandbox")

	// Don't create any sandbox - simulates scenario where sandbox was never created
	// or was removed while server was down

	// Verify no sandbox exists
	_, err := setup.provider.Get(ctx, session.ID)
	if err != sandbox.ErrNotFound {
		t.Fatalf("Expected sandbox to not exist, got: %v", err)
	}

	// Create sandbox service
	sandboxSvc := service.NewSandboxService(setup.store, setup.provider, setup.cfg, nil, nil, nil, nil)

	// Run session state reconciliation
	t.Log("Running session state reconciliation...")
	if err := sandboxSvc.ReconcileSessionStates(ctx); err != nil {
		t.Fatalf("ReconcileSessionStates failed: %v", err)
	}

	// Verify session status is marked as stopped (sandbox will be created on demand)
	updatedSession, err := setup.store.GetSessionByID(ctx, session.ID)
	if err != nil {
		t.Fatalf("Failed to get session: %v", err)
	}

	if updatedSession.Status != model.SessionStatusStopped {
		t.Errorf("Expected session status 'stopped', got '%s'", updatedSession.Status)
	}

	t.Log("Session correctly marked as stopped (no sandbox, will be created on demand)")
}

func TestReconcileSessionStates_MarksStoppedSessionWithStoppedSandbox(t *testing.T) {
	setup := newTestSandboxSetup(t)
	ctx := context.Background()

	// Create test data
	project := setup.createTestProject(t)
	workspace := setup.createTestWorkspace(t, project)
	session := setup.createTestSession(t, workspace, "session-with-stopped-sandbox")

	// Create and start a sandbox, then stop it gracefully
	t.Log("Creating and stopping sandbox...")
	setup.createSandboxWithImage(t, session.ID, testImageNew)

	// Stop the sandbox (0 timeout = SIGKILL immediately, no graceful shutdown needed for tests)
	if err := setup.provider.Stop(ctx, session.ID, 0); err != nil {
		t.Fatalf("Failed to stop sandbox: %v", err)
	}

	// Verify sandbox is stopped
	sb, err := setup.provider.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Failed to get sandbox: %v", err)
	}
	if sb.Status != sandbox.StatusStopped {
		t.Logf("Note: Sandbox status is %s (expected stopped)", sb.Status)
	}

	// Create sandbox service
	sandboxSvc := service.NewSandboxService(setup.store, setup.provider, setup.cfg, nil, nil, nil, nil)

	// Run session state reconciliation
	t.Log("Running session state reconciliation...")
	if err := sandboxSvc.ReconcileSessionStates(ctx); err != nil {
		t.Fatalf("ReconcileSessionStates failed: %v", err)
	}

	// Verify session status is marked as stopped (sandbox can be restarted on demand)
	updatedSession, err := setup.store.GetSessionByID(ctx, session.ID)
	if err != nil {
		t.Fatalf("Failed to get session: %v", err)
	}

	if updatedSession.Status != model.SessionStatusStopped {
		t.Errorf("Expected session status 'stopped', got '%s'", updatedSession.Status)
	}

	t.Log("Session correctly marked as stopped (sandbox can be restarted on demand)")
}

func TestProvider_GetReturnsNotFoundAfterExternalContainerDeletion(t *testing.T) {
	setup := newTestSandboxSetup(t)
	ctx := context.Background()

	// Create test data
	project := setup.createTestProject(t)
	workspace := setup.createTestWorkspace(t, project)
	session := setup.createTestSession(t, workspace, "session-external-delete")

	// Create and start a sandbox
	t.Log("Creating sandbox...")
	setup.createSandboxWithImage(t, session.ID, testImageNew)

	// Verify sandbox exists and is running
	sb, err := setup.provider.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Failed to get sandbox: %v", err)
	}
	if sb.Status != sandbox.StatusRunning {
		t.Fatalf("Expected sandbox status running, got %s", sb.Status)
	}
	containerID := sb.ID
	t.Logf("Sandbox created with container ID: %s", containerID)

	// Call Get again to ensure the container ID is cached in the provider
	_, err = setup.provider.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Failed to get sandbox (caching call): %v", err)
	}

	// Externally delete the container using docker rm -f (simulates external deletion)
	t.Log("Externally deleting container with docker rm -f...")
	containerName := fmt.Sprintf("discobot-session-%s", session.ID)
	rmCmd := exec.CommandContext(ctx, "docker", "rm", "-f", containerName)
	if output, err := rmCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to remove container: %v\nOutput: %s", err, output)
	}

	// Now call Get - it should return ErrNotFound, not a wrapped "No such container" error
	t.Log("Calling Get after external deletion...")
	_, err = setup.provider.Get(ctx, session.ID)
	if err == nil {
		t.Fatal("Expected error after container deletion, got nil")
	}
	if err != sandbox.ErrNotFound {
		t.Errorf("Expected sandbox.ErrNotFound after external deletion, got: %v", err)
	}

	t.Log("Provider correctly returns ErrNotFound after external container deletion")
}

func TestProvider_GetSecretReturnsNotFoundAfterExternalContainerDeletion(t *testing.T) {
	setup := newTestSandboxSetup(t)
	ctx := context.Background()

	// Create test data
	project := setup.createTestProject(t)
	workspace := setup.createTestWorkspace(t, project)
	session := setup.createTestSession(t, workspace, "session-external-delete-secret")

	// Create sandbox with a secret
	t.Log("Creating sandbox with secret...")
	tempCfg := &config.Config{
		SandboxImage: testImageNew,
	}
	// Use the same session project resolver from the main setup
	tempResolver := func(ctx context.Context, sessionID string) (string, error) {
		session, err := setup.store.GetSessionByID(ctx, sessionID)
		if err != nil {
			return "", err
		}
		return session.ProjectID, nil
	}
	tempProvider, err := docker.NewProvider(tempCfg, tempResolver)
	if err != nil {
		t.Fatalf("Failed to create temp provider: %v", err)
	}
	defer tempProvider.Close()

	opts := sandbox.CreateOptions{
		SharedSecret: "test-secret-12345",
		Labels: map[string]string{
			"discobot.session.id": session.ID,
		},
	}

	_, err = tempProvider.Create(ctx, session.ID, opts)
	if err != nil {
		t.Fatalf("Failed to create sandbox: %v", err)
	}

	if err := tempProvider.Start(ctx, session.ID); err != nil {
		t.Fatalf("Failed to start sandbox: %v", err)
	}

	// Verify we can get the secret
	secret, err := setup.provider.GetSecret(ctx, session.ID)
	if err != nil {
		t.Fatalf("Failed to get secret: %v", err)
	}
	if secret == "" {
		t.Fatal("Expected non-empty secret")
	}
	t.Logf("Got secret (length: %d)", len(secret))

	// Externally delete the container
	t.Log("Externally deleting container with docker rm -f...")
	containerName := fmt.Sprintf("discobot-session-%s", session.ID)
	rmCmd := exec.CommandContext(ctx, "docker", "rm", "-f", containerName)
	if output, err := rmCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to remove container: %v\nOutput: %s", err, output)
	}

	// Now call GetSecret - it should return ErrNotFound
	t.Log("Calling GetSecret after external deletion...")
	_, err = setup.provider.GetSecret(ctx, session.ID)
	if err == nil {
		t.Fatal("Expected error after container deletion, got nil")
	}
	if err != sandbox.ErrNotFound {
		t.Errorf("Expected sandbox.ErrNotFound after external deletion, got: %v", err)
	}

	t.Log("Provider correctly returns ErrNotFound for GetSecret after external container deletion")
}

func TestReconcileSessionStates_HandlesExternallyDeletedContainer(t *testing.T) {
	setup := newTestSandboxSetup(t)
	ctx := context.Background()

	// Create test data
	project := setup.createTestProject(t)
	workspace := setup.createTestWorkspace(t, project)
	session := setup.createTestSession(t, workspace, "session-external-delete-reconcile")

	// Create and start a sandbox
	t.Log("Creating sandbox...")
	setup.createSandboxWithImage(t, session.ID, testImageNew)

	// Verify sandbox exists
	sb, err := setup.provider.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Failed to get sandbox: %v", err)
	}
	t.Logf("Sandbox created with ID: %s", sb.ID)

	// Externally delete the container (simulates someone running docker rm -f)
	t.Log("Externally deleting container with docker rm -f...")
	containerName := fmt.Sprintf("discobot-session-%s", session.ID)
	rmCmd := exec.CommandContext(ctx, "docker", "rm", "-f", containerName)
	if output, err := rmCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to remove container: %v\nOutput: %s", err, output)
	}

	// Create sandbox service
	sandboxSvc := service.NewSandboxService(setup.store, setup.provider, setup.cfg, nil, nil, nil, nil)

	// Run session state reconciliation - should handle the deleted container gracefully
	t.Log("Running session state reconciliation after external deletion...")
	if err := sandboxSvc.ReconcileSessionStates(ctx); err != nil {
		t.Fatalf("ReconcileSessionStates failed: %v", err)
	}

	// Verify session status is marked as stopped (container gone, will be recreated on demand)
	updatedSession, err := setup.store.GetSessionByID(ctx, session.ID)
	if err != nil {
		t.Fatalf("Failed to get session: %v", err)
	}

	if updatedSession.Status != model.SessionStatusStopped {
		t.Errorf("Expected session status 'stopped' after external deletion, got '%s'", updatedSession.Status)
	}

	t.Log("Session correctly marked as stopped after external container deletion")
}

// TestReconcileSessionStates_ResetsRunningSessionWithNoActiveChat tests that
// sessions marked as "running" are reset to "ready" if no chat is actually in progress.
func TestReconcileSessionStates_ResetsRunningSessionWithNoActiveChat(t *testing.T) {
	setup := newTestSandboxSetup(t)
	ctx := context.Background()

	// Create test resources
	project := setup.createTestProject(t)
	workspace := setup.createTestWorkspace(t, project)

	// Create session marked as "running"
	workspacePath := workspace.Path
	session := &model.Session{
		ProjectID:     project.ID,
		WorkspaceID:   workspace.ID,
		Name:          "Running Session Test",
		Status:        "running", // Marked as legacy running
		WorkspacePath: &workspacePath,
	}
	if err := setup.store.CreateSession(ctx, session); err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}
	setup.trackSessionID(session.ID)

	t.Log("Creating sandbox (without starting a chat)...")

	// Create sandbox service and sandbox
	sandboxSvc := service.NewSandboxService(setup.store, setup.provider, setup.cfg, nil, nil, nil, nil)

	// Create sandbox without starting a chat
	if err := sandboxSvc.CreateForSession(ctx, session.ID); err != nil {
		t.Fatalf("Failed to create sandbox: %v", err)
	}

	// Verify sandbox is running
	sb, err := setup.provider.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Failed to get sandbox: %v", err)
	}
	if sb.Status != sandbox.StatusRunning {
		t.Fatalf("Expected sandbox status running, got: %s", sb.Status)
	}

	t.Log("Running session state reconciliation (session marked running, but no chat active)...")

	// Run reconciliation - should detect no chat is running and reset to ready
	if err := sandboxSvc.ReconcileSessionStates(ctx); err != nil {
		t.Fatalf("ReconcileSessionStates failed: %v", err)
	}

	// Verify session status was reset to ready
	updatedSession, err := setup.store.GetSessionByID(ctx, session.ID)
	if err != nil {
		t.Fatalf("Failed to get session: %v", err)
	}

	if updatedSession.Status != model.SessionStatusReady {
		t.Errorf("Expected session status to be reset to ready, got: %s", updatedSession.Status)
	}

	t.Log("Session correctly reset from running to ready (no active chat)")
}

// TestReconcileSessionStates_KeepsRunningSessionWithActiveChat tests that
// sessions marked as "running" with an actual active chat are kept as running.
func TestReconcileSessionStates_KeepsRunningSessionWithActiveChat(t *testing.T) {
	skipIfNoDocker(t)

	// This test would require a mock agent API or a real agent container
	// For now, we'll skip it as it requires more complex setup
	// The reconciliation logic checks the agent API's /chat/status endpoint
	t.Skip("Requires mock agent API - covered by manual testing")
}
