package service

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/model"
	"github.com/obot-platform/discobot/server/internal/sandbox"
	"github.com/obot-platform/discobot/server/internal/store"
)

// setupTestStoreForIdleMonitor creates an in-memory SQLite database for testing
func setupTestStoreForIdleMonitor(t *testing.T) *store.Store {
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

// TestSandboxIdleMonitor_StopsIdleSessions verifies that the monitor
// correctly stops sessions that have been idle longer than the timeout.
func TestSandboxIdleMonitor_StopsIdleSessions(t *testing.T) {
	ctx := context.Background()
	testStore := setupTestStoreForIdleMonitor(t)
	logger := slog.Default()

	// Track stop calls
	var stopCalled atomic.Bool

	// Create mock agent API that returns not running (204 No Content on stream endpoint)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/chat/status") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"isRunning":false}`)
			return
		}
		http.NotFound(w, r)
	})

	mockProvider := &mockSandboxProvider{
		secret:  "test-secret",
		handler: handler,
		onStop:  func(string) { stopCalled.Store(true) },
	}

	cfg := &config.Config{}
	sandboxSvc := NewSandboxService(testStore, mockProvider, cfg, nil, nil, nil, nil)
	sessionSvc := NewSessionService(testStore, nil, mockProvider, sandboxSvc, nil, nil)

	// Create idle monitor with short timeout
	idleTimeout := 1 * time.Second
	checkInterval := 100 * time.Millisecond
	monitor := NewSandboxIdleMonitor(
		testStore,
		sandboxSvc,
		sessionSvc,
		nil,
		logger,
		idleTimeout,
		checkInterval,
	)

	// Create test project and workspace
	project := &model.Project{ID: "test-project", Name: "Test"}
	workspace := &model.Workspace{
		ID:          "test-ws",
		ProjectID:   project.ID,
		Path:        "/test",
		SourceType:  "local",
		DisplayName: func() *string { s := "Test WS"; return &s }(),
	}

	if err := testStore.CreateProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	if err := testStore.CreateWorkspace(ctx, workspace); err != nil {
		t.Fatal(err)
	}

	// Create a session with old updated_at (simulating idle)
	session := &model.Session{
		ID:          "test-session",
		ProjectID:   project.ID,
		WorkspaceID: workspace.ID,
		Status:      model.SessionStatusReady,
		UpdatedAt:   time.Now().Add(-2 * time.Second), // Older than idle timeout
	}

	if err := testStore.CreateSession(ctx, session); err != nil {
		t.Fatal(err)
	}

	// Create sandbox for this session
	if _, err := mockProvider.Create(ctx, session.ID, sandbox.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	// Manually check idle sessions
	if err := monitor.checkIdleSessions(ctx); err != nil {
		t.Fatalf("checkIdleSessions failed: %v", err)
	}

	// Verify sandbox was stopped
	if !stopCalled.Load() {
		t.Error("Expected sandbox to be stopped for idle session")
	}

	// Verify session status updated to stopped
	updatedSession, err := testStore.GetSessionByID(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}

	if updatedSession.Status != model.SessionStatusStopped {
		t.Errorf("Expected session status %q, got %q", model.SessionStatusStopped, updatedSession.Status)
	}
}

// TestSandboxIdleMonitor_SkipsRunningCompletions verifies that the monitor
// does not stop sessions with active completions in progress.
func TestSandboxIdleMonitor_SkipsRunningCompletions(t *testing.T) {
	ctx := context.Background()
	testStore := setupTestStoreForIdleMonitor(t)
	logger := slog.Default()

	// Track stop calls
	var stopCalled atomic.Bool

	// Create mock agent API that returns running completion
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/chat/status") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"isRunning":true}`)
			return
		}
		http.NotFound(w, r)
	})

	mockProvider := &mockSandboxProvider{
		secret:  "test-secret",
		handler: handler,
		onStop:  func(string) { stopCalled.Store(true) },
	}

	cfg := &config.Config{}
	sandboxSvc := NewSandboxService(testStore, mockProvider, cfg, nil, nil, nil, nil)
	sessionSvc := NewSessionService(testStore, nil, mockProvider, sandboxSvc, nil, nil)

	// Create idle monitor with short timeout
	idleTimeout := 1 * time.Second
	checkInterval := 100 * time.Millisecond
	monitor := NewSandboxIdleMonitor(
		testStore,
		sandboxSvc,
		sessionSvc,
		nil,
		logger,
		idleTimeout,
		checkInterval,
	)

	// Create test project and workspace
	project := &model.Project{ID: "test-project", Name: "Test"}
	workspace := &model.Workspace{
		ID:          "test-ws",
		ProjectID:   project.ID,
		Path:        "/test",
		SourceType:  "local",
		DisplayName: func() *string { s := "Test WS"; return &s }(),
	}

	if err := testStore.CreateProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	if err := testStore.CreateWorkspace(ctx, workspace); err != nil {
		t.Fatal(err)
	}

	// Create a ready session with old updated_at
	session := &model.Session{
		ID:          "test-session",
		ProjectID:   project.ID,
		WorkspaceID: workspace.ID,
		Status:      model.SessionStatusReady,
		UpdatedAt:   time.Now().Add(-2 * time.Second), // Older than idle timeout
	}

	if err := testStore.CreateSession(ctx, session); err != nil {
		t.Fatal(err)
	}

	// Create sandbox for this session
	if _, err := mockProvider.Create(ctx, session.ID, sandbox.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	// Manually check idle sessions
	if err := monitor.checkIdleSessions(ctx); err != nil {
		t.Fatalf("checkIdleSessions failed: %v", err)
	}

	// Verify sandbox was NOT stopped (completion in progress)
	if stopCalled.Load() {
		t.Error("Expected sandbox NOT to be stopped when completion is running")
	}

	// Verify session status remains ready
	updatedSession, err := testStore.GetSessionByID(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}

	if updatedSession.Status != model.SessionStatusReady {
		t.Errorf("Expected session to stay ready, got %q", updatedSession.Status)
	}
}

// TestSandboxIdleMonitor_ActivityResetsTimer verifies that recent activity
// prevents a session from being stopped.
func TestSandboxIdleMonitor_ActivityResetsTimer(t *testing.T) {
	ctx := context.Background()
	testStore := setupTestStoreForIdleMonitor(t)
	logger := slog.Default()

	// Track stop calls
	var stopCalled atomic.Bool

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/chat/status") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"isRunning":false}`)
			return
		}
		http.NotFound(w, r)
	})

	mockProvider := &mockSandboxProvider{
		secret:  "test-secret",
		handler: handler,
		onStop:  func(string) { stopCalled.Store(true) },
	}

	cfg := &config.Config{}
	sandboxSvc := NewSandboxService(testStore, mockProvider, cfg, nil, nil, nil, nil)
	sessionSvc := NewSessionService(testStore, nil, mockProvider, sandboxSvc, nil, nil)

	// Create idle monitor with short timeout
	idleTimeout := 1 * time.Second
	checkInterval := 100 * time.Millisecond
	monitor := NewSandboxIdleMonitor(
		testStore,
		sandboxSvc,
		sessionSvc,
		nil,
		logger,
		idleTimeout,
		checkInterval,
	)

	// Create test project and workspace
	project := &model.Project{ID: "test-project", Name: "Test"}
	workspace := &model.Workspace{
		ID:          "test-ws",
		ProjectID:   project.ID,
		Path:        "/test",
		SourceType:  "local",
		DisplayName: func() *string { s := "Test WS"; return &s }(),
	}

	if err := testStore.CreateProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	if err := testStore.CreateWorkspace(ctx, workspace); err != nil {
		t.Fatal(err)
	}

	// Create session with old updated_at (would be idle)
	session := &model.Session{
		ID:          "test-session",
		ProjectID:   project.ID,
		WorkspaceID: workspace.ID,
		Status:      model.SessionStatusReady,
		UpdatedAt:   time.Now().Add(-2 * time.Second),
	}

	if err := testStore.CreateSession(ctx, session); err != nil {
		t.Fatal(err)
	}

	// Create sandbox
	if _, err := mockProvider.Create(ctx, session.ID, sandbox.CreateOptions{}); err != nil {
		t.Fatal(err)
	}

	// Record recent activity (should reset idle timer)
	sandboxSvc.RecordActivity(session.ID)

	// Check idle sessions
	if err := monitor.checkIdleSessions(ctx); err != nil {
		t.Fatalf("checkIdleSessions failed: %v", err)
	}

	// Verify sandbox was NOT stopped (recent activity)
	if stopCalled.Load() {
		t.Error("Expected sandbox NOT to be stopped when there's recent activity")
	}

	// Verify session status remains ready
	updatedSession, err := testStore.GetSessionByID(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}

	if updatedSession.Status != model.SessionStatusReady {
		t.Errorf("Expected session to stay ready, got %q", updatedSession.Status)
	}
}

// TestSandboxIdleMonitor_IgnoresStoppedSessions verifies that already-stopped
// sessions are not checked or affected.
func TestSandboxIdleMonitor_IgnoresStoppedSessions(t *testing.T) {
	ctx := context.Background()
	testStore := setupTestStoreForIdleMonitor(t)
	logger := slog.Default()

	// Track API calls
	var apiCalled atomic.Bool

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalled.Store(true)
		http.NotFound(w, r)
	})

	mockProvider := &mockSandboxProvider{
		secret:  "test-secret",
		handler: handler,
	}

	cfg := &config.Config{}
	sandboxSvc := NewSandboxService(testStore, mockProvider, cfg, nil, nil, nil, nil)
	sessionSvc := NewSessionService(testStore, nil, mockProvider, sandboxSvc, nil, nil)

	idleTimeout := 1 * time.Second
	checkInterval := 100 * time.Millisecond
	monitor := NewSandboxIdleMonitor(
		testStore,
		sandboxSvc,
		sessionSvc,
		nil,
		logger,
		idleTimeout,
		checkInterval,
	)

	// Create test project and workspace
	project := &model.Project{ID: "test-project", Name: "Test"}
	workspace := &model.Workspace{
		ID:          "test-ws",
		ProjectID:   project.ID,
		Path:        "/test",
		SourceType:  "local",
		DisplayName: func() *string { s := "Test WS"; return &s }(),
	}

	if err := testStore.CreateProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	if err := testStore.CreateWorkspace(ctx, workspace); err != nil {
		t.Fatal(err)
	}

	// Create stopped session with old updated_at
	session := &model.Session{
		ID:          "test-session",
		ProjectID:   project.ID,
		WorkspaceID: workspace.ID,
		Status:      model.SessionStatusStopped, // Already stopped
		UpdatedAt:   time.Now().Add(-2 * time.Second),
	}

	if err := testStore.CreateSession(ctx, session); err != nil {
		t.Fatal(err)
	}

	// Check idle sessions
	if err := monitor.checkIdleSessions(ctx); err != nil {
		t.Fatalf("checkIdleSessions failed: %v", err)
	}

	// Verify no API calls were made (stopped sessions are ignored)
	if apiCalled.Load() {
		t.Error("Expected no API calls for stopped session")
	}
}

// TestSandboxIdleMonitor_GracefulShutdown verifies that the monitor
// shuts down cleanly when requested.
func TestSandboxIdleMonitor_GracefulShutdown(t *testing.T) {
	ctx := context.Background()
	testStore := setupTestStoreForIdleMonitor(t)
	logger := slog.Default()

	mockProvider := &mockSandboxProvider{
		secret: "test-secret",
	}

	cfg := &config.Config{}
	sandboxSvc := NewSandboxService(testStore, mockProvider, cfg, nil, nil, nil, nil)
	sessionSvc := NewSessionService(testStore, nil, mockProvider, sandboxSvc, nil, nil)

	monitor := NewSandboxIdleMonitor(
		testStore,
		sandboxSvc,
		sessionSvc,
		nil,
		logger,
		30*time.Second,
		5*time.Second,
	)

	// Start the monitor
	monitorCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	monitor.Start(monitorCtx)

	// Wait a bit for goroutine to start
	time.Sleep(50 * time.Millisecond)

	// Shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 5*time.Second)
	defer shutdownCancel()

	err := monitor.Shutdown(shutdownCtx)
	if err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}

	// Verify shutdown completed (no deadlock)
	// The test passing means shutdown worked
}

// TestSandboxIdleMonitor_MultipleIdleSessions verifies that the monitor
// correctly handles multiple idle sessions at once.
func TestSandboxIdleMonitor_MultipleIdleSessions(t *testing.T) {
	ctx := context.Background()
	testStore := setupTestStoreForIdleMonitor(t)
	logger := slog.Default()

	// Track stops per session
	var stopCount atomic.Int32

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/chat/status") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"isRunning":false}`)
			return
		}
		http.NotFound(w, r)
	})

	mockProvider := &mockSandboxProvider{
		secret:  "test-secret",
		handler: handler,
		onStop:  func(string) { stopCount.Add(1) },
	}

	cfg := &config.Config{}
	sandboxSvc := NewSandboxService(testStore, mockProvider, cfg, nil, nil, nil, nil)
	sessionSvc := NewSessionService(testStore, nil, mockProvider, sandboxSvc, nil, nil)

	idleTimeout := 1 * time.Second
	checkInterval := 100 * time.Millisecond
	monitor := NewSandboxIdleMonitor(
		testStore,
		sandboxSvc,
		sessionSvc,
		nil,
		logger,
		idleTimeout,
		checkInterval,
	)

	// Create test project and workspace
	project := &model.Project{ID: "test-project", Name: "Test"}
	workspace := &model.Workspace{
		ID:          "test-ws",
		ProjectID:   project.ID,
		Path:        "/test",
		SourceType:  "local",
		DisplayName: func() *string { s := "Test WS"; return &s }(),
	}

	if err := testStore.CreateProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	if err := testStore.CreateWorkspace(ctx, workspace); err != nil {
		t.Fatal(err)
	}

	// Create 3 idle sessions
	for i := 1; i <= 3; i++ {
		sessionID := "idle-session-" + string(rune('0'+i))
		session := &model.Session{
			ID:          sessionID,
			ProjectID:   project.ID,
			WorkspaceID: workspace.ID,
			Status:      model.SessionStatusReady,
			UpdatedAt:   time.Now().Add(-2 * time.Second),
		}

		if err := testStore.CreateSession(ctx, session); err != nil {
			t.Fatal(err)
		}

		if _, err := mockProvider.Create(ctx, session.ID, sandbox.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}

	// Check idle sessions
	if err := monitor.checkIdleSessions(ctx); err != nil {
		t.Fatalf("checkIdleSessions failed: %v", err)
	}

	// Verify all 3 sessions were stopped
	if got := stopCount.Load(); got != 3 {
		t.Errorf("Expected 3 sessions stopped, got %d", got)
	}

	// Verify all sessions marked as stopped
	for i := 1; i <= 3; i++ {
		sessionID := "idle-session-" + string(rune('0'+i))
		updatedSession, err := testStore.GetSessionByID(ctx, sessionID)
		if err != nil {
			t.Fatal(err)
		}

		if updatedSession.Status != model.SessionStatusStopped {
			t.Errorf("Session %s: expected status %q, got %q",
				sessionID, model.SessionStatusStopped, updatedSession.Status)
		}
	}
}
