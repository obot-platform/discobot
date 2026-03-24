package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/model"
	"github.com/obot-platform/discobot/server/internal/sandbox"
	"github.com/obot-platform/discobot/server/internal/sandbox/mock"
	"github.com/obot-platform/discobot/server/internal/store"
)

// Use the config constant for test consistency
var testImage = config.DefaultSandboxImage()
var testEncryptionKey = []byte("0123456789abcdef0123456789abcdef")

// sandboxCreatingInitializer provides a SessionInitializer for tests
// that actually creates sandboxes (unlike the no-op testSessionInitializer in perform_commit_test.go)
type sandboxCreatingInitializer struct {
	sandboxSvc *SandboxService
}

func (t *sandboxCreatingInitializer) Initialize(ctx context.Context, sessionID string) error {
	// Check if sandbox already exists (mimics SessionService.Initialize behavior)
	existingSandbox, err := t.sandboxSvc.provider.Get(ctx, sessionID)
	if err != nil && !errors.Is(err, sandbox.ErrNotFound) {
		return err
	}

	if existingSandbox != nil {
		// Sandbox exists - handle based on status
		switch existingSandbox.Status {
		case sandbox.StatusRunning:
			return nil // Already running
		case sandbox.StatusCreated, sandbox.StatusStopped:
			// Try to start it
			if err := t.sandboxSvc.provider.Start(ctx, sessionID); err != nil {
				// Start failed - remove and recreate
				_ = t.sandboxSvc.provider.Remove(ctx, sessionID)
				return t.sandboxSvc.CreateForSession(ctx, sessionID)
			}
			return nil
		default:
			// Failed state - remove and recreate
			_ = t.sandboxSvc.provider.Remove(ctx, sessionID)
			return t.sandboxSvc.CreateForSession(ctx, sessionID)
		}
	}

	// No existing sandbox - create new one
	return t.sandboxSvc.CreateForSession(ctx, sessionID)
}

// setupTestStore creates an in-memory SQLite database for testing
func setupTestStore(t *testing.T) *store.Store {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}

	// Run migrations
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}

	return store.New(db, nil)
}

// createTestSession creates a session with the given workspace path for testing
func createTestSession(t *testing.T, s *store.Store, sessionID, workspacePath string) {
	t.Helper()

	ctx := context.Background()

	// Create a workspace first
	workspace := &model.Workspace{
		ID:         "test-workspace",
		ProjectID:  "test-project",
		Path:       workspacePath,
		SourceType: "local",
		Status:     "ready",
	}
	if err := s.CreateWorkspace(ctx, workspace); err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}

	// Create the session with workspace path set
	session := &model.Session{
		ID:            sessionID,
		ProjectID:     "test-project",
		WorkspaceID:   "test-workspace",
		Name:          "Test Session",
		Status:        model.SessionStatusReady,
		WorkspacePath: &workspacePath,
	}
	if err := s.CreateSession(ctx, session); err != nil {
		t.Fatalf("failed to create test session: %v", err)
	}
}

type imageIDAwareReconcileProvider struct {
	sandboxes         map[string]*sandbox.Sandbox
	configuredImage   string
	configuredImageID string
	createCount       int
	cleanupCalls      int
}

func newImageIDAwareReconcileProvider(image, imageID string) *imageIDAwareReconcileProvider {
	return &imageIDAwareReconcileProvider{
		sandboxes:         make(map[string]*sandbox.Sandbox),
		configuredImage:   image,
		configuredImageID: imageID,
	}
}

func (p *imageIDAwareReconcileProvider) ImageExists(_ context.Context) bool {
	return true
}

func (p *imageIDAwareReconcileProvider) Image() string {
	return p.configuredImage
}

func (p *imageIDAwareReconcileProvider) CurrentImageID(_ context.Context) (string, error) {
	return p.configuredImageID, nil
}

func (p *imageIDAwareReconcileProvider) CleanupUnusedImages(_ context.Context) error {
	p.cleanupCalls++
	return nil
}

func (p *imageIDAwareReconcileProvider) Create(_ context.Context, sessionID string, _ sandbox.CreateOptions) (*sandbox.Sandbox, error) {
	if _, exists := p.sandboxes[sessionID]; exists {
		return nil, sandbox.ErrAlreadyExists
	}

	p.createCount++
	sb := &sandbox.Sandbox{
		ID:        fmt.Sprintf("recreated-%d", p.createCount),
		SessionID: sessionID,
		Status:    sandbox.StatusCreated,
		Image:     p.configuredImage,
		CreatedAt: time.Now(),
		Metadata: map[string]string{
			sandbox.MetadataImageID: p.configuredImageID,
		},
	}
	p.sandboxes[sessionID] = sb
	return sb, nil
}

func (p *imageIDAwareReconcileProvider) Start(_ context.Context, sessionID string) error {
	sb, ok := p.sandboxes[sessionID]
	if !ok {
		return sandbox.ErrNotFound
	}

	now := time.Now()
	sb.Status = sandbox.StatusRunning
	sb.StartedAt = &now
	return nil
}

func (p *imageIDAwareReconcileProvider) Stop(_ context.Context, sessionID string, _ time.Duration) error {
	sb, ok := p.sandboxes[sessionID]
	if !ok {
		return sandbox.ErrNotFound
	}

	sb.Status = sandbox.StatusStopped
	return nil
}

func (p *imageIDAwareReconcileProvider) Remove(_ context.Context, sessionID string, _ ...sandbox.RemoveOption) error {
	delete(p.sandboxes, sessionID)
	return nil
}

func (p *imageIDAwareReconcileProvider) Get(_ context.Context, sessionID string) (*sandbox.Sandbox, error) {
	sb, ok := p.sandboxes[sessionID]
	if !ok {
		return nil, sandbox.ErrNotFound
	}
	return sb, nil
}

func (p *imageIDAwareReconcileProvider) GetSecret(_ context.Context, _ string) (string, error) {
	return "", sandbox.ErrNotFound
}

func (p *imageIDAwareReconcileProvider) List(_ context.Context) ([]*sandbox.Sandbox, error) {
	result := make([]*sandbox.Sandbox, 0, len(p.sandboxes))
	for _, sb := range p.sandboxes {
		result = append(result, sb)
	}
	return result, nil
}

func (p *imageIDAwareReconcileProvider) Exec(_ context.Context, _ string, _ []string, _ sandbox.ExecOptions) (*sandbox.ExecResult, error) {
	return nil, errors.New("not implemented")
}

func (p *imageIDAwareReconcileProvider) Attach(_ context.Context, _ string, _ sandbox.AttachOptions) (sandbox.PTY, error) {
	return nil, errors.New("not implemented")
}

func (p *imageIDAwareReconcileProvider) ExecStream(_ context.Context, _ string, _ []string, _ sandbox.ExecStreamOptions) (sandbox.Stream, error) {
	return nil, errors.New("not implemented")
}

func (p *imageIDAwareReconcileProvider) HTTPClient(_ context.Context, _ string) (*http.Client, error) {
	return &http.Client{}, nil
}

func (p *imageIDAwareReconcileProvider) Watch(_ context.Context) (<-chan sandbox.StateEvent, error) {
	ch := make(chan sandbox.StateEvent)
	close(ch)
	return ch, nil
}

func (p *imageIDAwareReconcileProvider) Reconcile(_ context.Context) error {
	return nil
}

func (p *imageIDAwareReconcileProvider) RemoveProject(_ context.Context, _ string) error {
	return nil
}

func TestSandboxService_CreateForSession(t *testing.T) {
	mockProvider := mock.NewProviderWithImage(testImage)
	testStore := setupTestStore(t)
	cfg := &config.Config{
		SandboxIdleTimeout: 30 * time.Minute,
		EncryptionKey:      testEncryptionKey,
	}
	svc := NewSandboxService(testStore, mockProvider, cfg, nil, nil, nil, nil)

	ctx := context.Background()
	sessionID := "test-session-1"
	workspacePath := "/home/user/workspace"

	// Create test session with workspace path
	createTestSession(t, testStore, sessionID, workspacePath)

	err := svc.CreateForSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("CreateForSession failed: %v", err)
	}

	// Verify sandbox was created and started
	sb, err := mockProvider.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if sb.Status != sandbox.StatusRunning {
		t.Errorf("Expected status %s, got %s", sandbox.StatusRunning, sb.Status)
	}

	if sb.Image != testImage {
		t.Errorf("Expected image %s, got %s", testImage, sb.Image)
	}
}

func TestReconcileSandboxes_UsesImageIDAndRunsCleanup(t *testing.T) {
	provider := newImageIDAwareReconcileProvider("ghcr.io/obot-platform/discobot:latest", "sha256:new")
	testStore := setupTestStore(t)
	cfg := &config.Config{EncryptionKey: testEncryptionKey}
	svc := NewSandboxService(testStore, provider, cfg, nil, nil, nil, nil)
	svc.SetSessionInitializer(&sandboxCreatingInitializer{sandboxSvc: svc})

	ctx := context.Background()
	sessionID := "test-session-image-id"
	workspacePath := "/workspace"
	createTestSession(t, testStore, sessionID, workspacePath)

	provider.sandboxes[sessionID] = &sandbox.Sandbox{
		ID:        "old-sandbox",
		SessionID: sessionID,
		Status:    sandbox.StatusRunning,
		Image:     provider.configuredImage,
		CreatedAt: time.Now(),
		Metadata: map[string]string{
			sandbox.MetadataImageID: "sha256:old",
		},
	}

	if err := svc.ReconcileSandboxes(ctx); err != nil {
		t.Fatalf("ReconcileSandboxes failed: %v", err)
	}

	sb, err := provider.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if sb.ID == "old-sandbox" {
		t.Fatalf("expected sandbox to be recreated when image ID changed")
	}

	if got := sb.Metadata[sandbox.MetadataImageID]; got != provider.configuredImageID {
		t.Fatalf("expected recreated sandbox image ID %s, got %s", provider.configuredImageID, got)
	}

	if provider.cleanupCalls != 1 {
		t.Fatalf("expected cleanup to run once, got %d", provider.cleanupCalls)
	}
}

func TestSandboxService_CreateForSession_AlreadyExists(t *testing.T) {
	mockProvider := mock.NewProvider()
	testStore := setupTestStore(t)
	cfg := &config.Config{EncryptionKey: testEncryptionKey}
	svc := NewSandboxService(testStore, mockProvider, cfg, nil, nil, nil, nil)

	ctx := context.Background()
	sessionID := "test-session-1"
	workspacePath := "/workspace"

	// Create test session
	createTestSession(t, testStore, sessionID, workspacePath)

	// Create first time
	err := svc.CreateForSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("First CreateForSession failed: %v", err)
	}

	// Try to create again - should fail
	err = svc.CreateForSession(ctx, sessionID)
	if err == nil {
		t.Error("Expected error when creating duplicate sandbox")
	}
}

func TestSandboxService_GetForSession(t *testing.T) {
	mockProvider := mock.NewProvider()
	testStore := setupTestStore(t)
	cfg := &config.Config{EncryptionKey: testEncryptionKey}
	svc := NewSandboxService(testStore, mockProvider, cfg, nil, nil, nil, nil)

	ctx := context.Background()
	sessionID := "test-session-1"
	workspacePath := "/workspace"

	// Create test session
	createTestSession(t, testStore, sessionID, workspacePath)

	// Create sandbox
	err := svc.CreateForSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("CreateForSession failed: %v", err)
	}

	// Get sandbox
	sb, err := svc.GetForSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetForSession failed: %v", err)
	}

	if sb.SessionID != sessionID {
		t.Errorf("Expected sessionID %s, got %s", sessionID, sb.SessionID)
	}
}

func TestSandboxService_GetForSession_NotFound(t *testing.T) {
	mockProvider := mock.NewProvider()
	testStore := setupTestStore(t)
	cfg := &config.Config{EncryptionKey: testEncryptionKey}
	svc := NewSandboxService(testStore, mockProvider, cfg, nil, nil, nil, nil)

	ctx := context.Background()

	_, err := svc.GetForSession(ctx, "nonexistent")
	if err != sandbox.ErrNotFound {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}
}

func TestSandboxService_EnsureSandboxReady_CreatesNew(t *testing.T) {
	mockProvider := mock.NewProvider()
	testStore := setupTestStore(t)
	cfg := &config.Config{EncryptionKey: testEncryptionKey}
	svc := NewSandboxService(testStore, mockProvider, cfg, nil, nil, nil, nil)
	// Provide a session initializer for the test fallback path
	svc.SetSessionInitializer(&sandboxCreatingInitializer{sandboxSvc: svc})

	ctx := context.Background()
	sessionID := "test-session-1"
	workspacePath := "/workspace"

	// Create test session
	createTestSession(t, testStore, sessionID, workspacePath)

	// GetClient should create sandbox if not exists (session is "ready")
	_, err := svc.GetClient(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetClient failed: %v", err)
	}

	sb, err := mockProvider.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if sb.Status != sandbox.StatusRunning {
		t.Errorf("Expected status %s, got %s", sandbox.StatusRunning, sb.Status)
	}
}

func TestSandboxService_EnsureSandboxReady_AlreadyRunning(t *testing.T) {
	mockProvider := mock.NewProvider()
	testStore := setupTestStore(t)
	cfg := &config.Config{EncryptionKey: testEncryptionKey}
	svc := NewSandboxService(testStore, mockProvider, cfg, nil, nil, nil, nil)

	ctx := context.Background()
	sessionID := "test-session-1"
	workspacePath := "/workspace"

	// Create test session
	createTestSession(t, testStore, sessionID, workspacePath)

	// Create and start
	err := svc.CreateForSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("CreateForSession failed: %v", err)
	}

	// GetClient on already running sandbox should succeed
	_, err = svc.GetClient(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetClient failed: %v", err)
	}
}

func TestSandboxService_EnsureSandboxReady_StartsStopped(t *testing.T) {
	mockProvider := mock.NewProvider()
	testStore := setupTestStore(t)
	cfg := &config.Config{EncryptionKey: testEncryptionKey}
	svc := NewSandboxService(testStore, mockProvider, cfg, nil, nil, nil, nil)
	// Provide a session initializer for the test fallback path
	svc.SetSessionInitializer(&sandboxCreatingInitializer{sandboxSvc: svc})

	ctx := context.Background()
	sessionID := "test-session-1"
	workspacePath := "/workspace"

	// Create test session
	createTestSession(t, testStore, sessionID, workspacePath)

	// Create and start
	err := svc.CreateForSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("CreateForSession failed: %v", err)
	}

	// Stop the sandbox
	err = svc.StopForSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("StopForSession failed: %v", err)
	}

	// GetClient should restart the stopped sandbox
	_, err = svc.GetClient(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetClient failed: %v", err)
	}

	sb, err := mockProvider.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if sb.Status != sandbox.StatusRunning {
		t.Errorf("Expected status %s, got %s", sandbox.StatusRunning, sb.Status)
	}
}

func TestSandboxService_DestroyForSession(t *testing.T) {
	mockProvider := mock.NewProvider()
	testStore := setupTestStore(t)
	cfg := &config.Config{EncryptionKey: testEncryptionKey}
	svc := NewSandboxService(testStore, mockProvider, cfg, nil, nil, nil, nil)

	ctx := context.Background()
	sessionID := "test-session-1"
	workspacePath := "/workspace"

	// Create test session
	createTestSession(t, testStore, sessionID, workspacePath)

	// Create sandbox
	err := svc.CreateForSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("CreateForSession failed: %v", err)
	}

	// Destroy sandbox
	err = svc.DestroyForSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("DestroyForSession failed: %v", err)
	}

	// Verify sandbox is gone
	_, err = mockProvider.Get(ctx, sessionID)
	if err != sandbox.ErrNotFound {
		t.Errorf("Expected ErrNotFound after destroy, got %v", err)
	}
}

func TestSandboxService_DestroyForSession_NotFound(t *testing.T) {
	mockProvider := mock.NewProvider()
	testStore := setupTestStore(t)
	cfg := &config.Config{EncryptionKey: testEncryptionKey}
	svc := NewSandboxService(testStore, mockProvider, cfg, nil, nil, nil, nil)

	ctx := context.Background()

	// Destroy nonexistent sandbox should not error (idempotent)
	err := svc.DestroyForSession(ctx, "nonexistent")
	if err != nil {
		t.Errorf("DestroyForSession should be idempotent, got: %v", err)
	}
}

func TestSandboxService_Exec(t *testing.T) {
	mockProvider := mock.NewProvider()
	testStore := setupTestStore(t)
	cfg := &config.Config{EncryptionKey: testEncryptionKey}
	svc := NewSandboxService(testStore, mockProvider, cfg, nil, nil, nil, nil)

	ctx := context.Background()
	sessionID := "test-session-1"
	workspacePath := "/workspace"

	// Create test session
	createTestSession(t, testStore, sessionID, workspacePath)

	// Create sandbox
	err := svc.CreateForSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("CreateForSession failed: %v", err)
	}

	// Execute command
	result, err := svc.Exec(ctx, sessionID, []string{"echo", "hello"}, sandbox.ExecOptions{})
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}

	if result.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", result.ExitCode)
	}
}

func TestSandboxService_Attach(t *testing.T) {
	mockProvider := mock.NewProvider()
	testStore := setupTestStore(t)
	cfg := &config.Config{EncryptionKey: testEncryptionKey}
	svc := NewSandboxService(testStore, mockProvider, cfg, nil, nil, nil, nil)

	ctx := context.Background()
	sessionID := "test-session-1"
	workspacePath := "/workspace"

	// Create test session
	createTestSession(t, testStore, sessionID, workspacePath)

	// Create sandbox
	err := svc.CreateForSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("CreateForSession failed: %v", err)
	}

	// Attach PTY
	pty, err := svc.Attach(ctx, sessionID, 24, 80, "", nil)
	if err != nil {
		t.Fatalf("Attach failed: %v", err)
	}
	defer pty.Close()

	// Write to PTY
	_, err = pty.Write([]byte("test"))
	if err != nil {
		t.Errorf("Write failed: %v", err)
	}

	// Read from PTY
	buf := make([]byte, 1024)
	n, err := pty.Read(buf)
	if err != nil {
		t.Errorf("Read failed: %v", err)
	}
	if n == 0 {
		t.Error("Expected some output from PTY")
	}
}

func TestSandboxService_CreateForSession_NoWorkspacePath(t *testing.T) {
	mockProvider := mock.NewProvider()
	testStore := setupTestStore(t)
	cfg := &config.Config{EncryptionKey: testEncryptionKey}
	svc := NewSandboxService(testStore, mockProvider, cfg, nil, nil, nil, nil)

	ctx := context.Background()
	sessionID := "test-session-no-path"

	// Create workspace without setting workspace path on session
	workspace := &model.Workspace{
		ID:         "test-workspace-2",
		ProjectID:  "test-project",
		Path:       "/some/path",
		SourceType: "local",
		Status:     "ready",
	}
	if err := testStore.CreateWorkspace(ctx, workspace); err != nil {
		t.Fatalf("failed to create test workspace: %v", err)
	}

	// Create session WITHOUT workspace path (simulating a session that hasn't been initialized)
	session := &model.Session{
		ID:          sessionID,
		ProjectID:   "test-project",
		WorkspaceID: "test-workspace-2",
		Name:        "Test Session",
		Status:      model.SessionStatusInitializing,
		// WorkspacePath is nil - not set
	}
	if err := testStore.CreateSession(ctx, session); err != nil {
		t.Fatalf("failed to create test session: %v", err)
	}

	// CreateForSession should fail because workspace path is not set
	err := svc.CreateForSession(ctx, sessionID)
	if err == nil {
		t.Error("Expected error when session has no workspace path")
	}
}

func TestSandboxService_CreateForSession_StoresEncryptedSSHKey(t *testing.T) {
	mockProvider := mock.NewProviderWithImage(testImage)
	testStore := setupTestStore(t)
	cfg := &config.Config{
		SandboxIdleTimeout: 30 * time.Minute,
		EncryptionKey:      testEncryptionKey,
	}
	svc := NewSandboxService(testStore, mockProvider, cfg, nil, nil, nil, nil)

	ctx := context.Background()
	sessionID := "test-session-ssh-key"
	workspacePath := "/home/user/workspace"
	createTestSession(t, testStore, sessionID, workspacePath)

	if err := svc.CreateForSession(ctx, sessionID); err != nil {
		t.Fatalf("CreateForSession failed: %v", err)
	}

	sess, err := testStore.GetSessionByID(ctx, sessionID)
	if err != nil {
		t.Fatalf("failed to reload session: %v", err)
	}
	if len(sess.SSHKeyEncryptedData) == 0 {
		t.Fatal("expected encrypted ssh key data to be stored on the session")
	}

	opts, ok := mockProvider.GetCreateOptions(sessionID)
	if !ok {
		t.Fatal("expected mock provider to record create options")
	}
	if opts.SSHKey == nil {
		t.Fatal("expected sandbox create options to include an ssh key")
	}
	if opts.SSHKey.Filename != sandboxSSHKeyFilename {
		t.Fatalf("ssh key filename = %q, want %q", opts.SSHKey.Filename, sandboxSSHKeyFilename)
	}
	if opts.SSHKey.Algorithm != "ecdsa-sha2-nistp256" {
		t.Fatalf("ssh key algorithm = %q, want ecdsa-sha2-nistp256", opts.SSHKey.Algorithm)
	}
	if opts.SSHKey.PrivateKey == "" || opts.SSHKey.PublicKey == "" {
		t.Fatal("expected sandbox ssh key material to be populated")
	}
}

func TestSandboxService_CreateForSession_ReusesExistingSSHKey(t *testing.T) {
	mockProvider := mock.NewProviderWithImage(testImage)
	testStore := setupTestStore(t)
	cfg := &config.Config{
		SandboxIdleTimeout: 30 * time.Minute,
		EncryptionKey:      testEncryptionKey,
	}
	svc := NewSandboxService(testStore, mockProvider, cfg, nil, nil, nil, nil)

	ctx := context.Background()
	sessionID := "test-session-ssh-key-reuse"
	workspacePath := "/home/user/workspace"
	createTestSession(t, testStore, sessionID, workspacePath)

	if err := svc.CreateForSession(ctx, sessionID); err != nil {
		t.Fatalf("first CreateForSession failed: %v", err)
	}
	firstOpts, ok := mockProvider.GetCreateOptions(sessionID)
	if !ok || firstOpts.SSHKey == nil {
		t.Fatal("expected first sandbox create options to include an ssh key")
	}

	if err := mockProvider.Remove(ctx, sessionID); err != nil {
		t.Fatalf("failed to remove mock sandbox: %v", err)
	}
	if err := svc.CreateForSession(ctx, sessionID); err != nil {
		t.Fatalf("second CreateForSession failed: %v", err)
	}
	secondOpts, ok := mockProvider.GetCreateOptions(sessionID)
	if !ok || secondOpts.SSHKey == nil {
		t.Fatal("expected second sandbox create options to include an ssh key")
	}

	if firstOpts.SSHKey.PrivateKey != secondOpts.SSHKey.PrivateKey {
		t.Fatal("expected sandbox ssh private key to be reused across sandbox recreation")
	}
	if firstOpts.SSHKey.PublicKey != secondOpts.SSHKey.PublicKey {
		t.Fatal("expected sandbox ssh public key to be reused across sandbox recreation")
	}
}
