package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/jobs"
	"github.com/obot-platform/discobot/server/internal/middleware"
	"github.com/obot-platform/discobot/server/internal/model"
	"github.com/obot-platform/discobot/server/internal/sandbox"
	mocksandbox "github.com/obot-platform/discobot/server/internal/sandbox/mock"
	"github.com/obot-platform/discobot/server/internal/service"
	"github.com/obot-platform/discobot/server/internal/store"
)

const testProjectID = "test-project"

// setupChatTestStore creates an in-memory SQLite database for testing.
func setupChatTestStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to create test database: %v", err)
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}
	return store.New(db, nil)
}

// seedSession creates a workspace and session in the store for testing.
func seedSession(t *testing.T, s *store.Store, sessionID string) {
	t.Helper()
	ctx := context.Background()

	workspace := &model.Workspace{
		ID:         "test-workspace",
		ProjectID:  testProjectID,
		Path:       "/workspace",
		SourceType: "local",
		Status:     "ready",
	}
	if err := s.CreateWorkspace(ctx, workspace); err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	workspacePath := "/workspace"
	session := &model.Session{
		ID:            sessionID,
		ProjectID:     testProjectID,
		WorkspaceID:   "test-workspace",
		Name:          "Test Session",
		Status:        model.SessionStatusReady,
		WorkspacePath: &workspacePath,
	}
	if err := s.CreateSession(ctx, session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
}

// newChatTestHandler creates a handler wired up with real services
// backed by the given store and mock sandbox provider.
func newChatTestHandler(t *testing.T, s *store.Store, provider *mocksandbox.Provider) *Handler {
	t.Helper()

	cfg := &config.Config{
		SandboxIdleTimeout: 30 * time.Minute,
		WorkspaceDir:       t.TempDir(),
	}

	jobQueue := jobs.NewQueue(s, cfg)
	workspaceSvc := service.NewWorkspaceService(s, nil, nil)
	sandboxSvc := service.NewSandboxService(s, provider, cfg, nil, nil, jobQueue, nil)
	sessionSvc := service.NewSessionService(s, nil, provider, sandboxSvc, nil, jobQueue)
	sandboxSvc.SetSessionInitializer(sessionSvc)
	chatSvc := service.NewChatService(s, sessionSvc, jobQueue, nil, sandboxSvc, nil)

	return &Handler{
		store:            s,
		cfg:              cfg,
		chatService:      chatSvc,
		sessionService:   sessionSvc,
		sandboxService:   sandboxSvc,
		workspaceService: workspaceSvc,
		jobQueue:         jobQueue,
	}
}

// makeChatRequest builds an http.Request for the Chat endpoint with the project ID set in context.
func makeChatRequest(ctx context.Context, t *testing.T, req ChatRequest) *http.Request {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}
	httpReq := httptest.NewRequest("POST", "/api/chat", bytes.NewReader(body))
	ctx = context.WithValue(ctx, middleware.ProjectIDKey, testProjectID)
	return httpReq.WithContext(ctx)
}

// TestChat_GetSessionByID_UnexpectedError verifies that the Chat handler returns
// a 500 Internal Server Error when GetSessionByID fails with a non-ErrNotFound error
// (e.g., a database failure), rather than falling through to create a new session.
func TestChat_GetSessionByID_UnexpectedError(t *testing.T) {
	s := setupChatTestStore(t)
	provider := mocksandbox.NewProvider()
	h := newChatTestHandler(t, s, provider)

	// Close the underlying DB to cause all queries to fail with a non-ErrNotFound error
	sqlDB, err := s.DB().DB()
	if err != nil {
		t.Fatalf("failed to get underlying DB: %v", err)
	}
	sqlDB.Close()

	req := makeChatRequest(context.Background(), t, ChatRequest{
		ID:          "session-123",
		Messages:    json.RawMessage(`[{"role":"user","parts":[{"type":"text","text":"hello"}]}]`),
		WorkspaceID: "test-workspace",
		AgentID:     "test-agent",
	})
	w := httptest.NewRecorder()

	h.Chat(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s",
			http.StatusInternalServerError, w.Code, w.Body.String())
	}
}

// TestChat_ClientDisconnect_DoesNotCancelSandbox verifies that when a client
// disconnects (cancels the request context) while the sandbox is being set up,
// the sandbox operation still receives a non-cancelled context. This ensures
// the chat request is always delivered to the sandbox; the only way to cancel
// a chat is via the explicit cancel endpoint.
func TestChat_ClientDisconnect_DoesNotCancelSandbox(t *testing.T) {
	s := setupChatTestStore(t)
	provider := mocksandbox.NewProvider()
	sessionID := "session-ctx-test"

	// Seed a session so we skip the NewSession path and go straight to SendToSandbox
	seedSession(t, s, sessionID)

	// Create the sandbox upfront so ensureSandboxReady's provider.Get will find it
	ctx := context.Background()
	workspacePath := "/workspace"
	_, err := provider.Create(ctx, sessionID, sandbox.CreateOptions{
		SharedSecret:  "test-secret",
		WorkspacePath: workspacePath,
	})
	if err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}
	if err := provider.Start(ctx, sessionID); err != nil {
		t.Fatalf("failed to start sandbox: %v", err)
	}

	// Track what happens inside provider.Get — this is the call that would fail
	// if the request context cancellation leaked into the sandbox operations.
	// We check the context state INSIDE the mock (not after handler exit, since
	// the handler's defer calls streamCancel which would cancel it later).
	var (
		mu                  sync.Mutex
		getCalled           = make(chan struct{}, 1)
		proceedAfterGet     = make(chan struct{})
		contextWasCancelled bool
		contextChecked      bool
	)

	provider.GetFunc = func(ctx context.Context, id string) (*sandbox.Sandbox, error) {
		// Signal that Get was called so the test can cancel the request context
		select {
		case getCalled <- struct{}{}:
		default:
		}

		// Block until the test tells us to proceed (after request context is cancelled)
		<-proceedAfterGet

		// Check context state NOW, while still inside the sandbox operation.
		// After the handler exits, its defer will call streamCancel(), so we
		// must capture the state here to test the right thing.
		mu.Lock()
		contextWasCancelled = ctx.Err() != nil
		contextChecked = true
		mu.Unlock()

		// Return the sandbox
		provider.GetFunc = nil // Reset so subsequent calls use default behavior
		return provider.Get(ctx, id)
	}

	h := newChatTestHandler(t, s, provider)

	// Create a cancellable request context (simulates client disconnect)
	reqCtx, cancelReq := context.WithCancel(context.Background())
	req := makeChatRequest(reqCtx, t, ChatRequest{
		ID:       sessionID,
		Messages: json.RawMessage(`[{"role":"user","parts":[{"type":"text","text":"hello"}]}]`),
	})
	w := httptest.NewRecorder()

	// Run the handler in a goroutine since it blocks
	handlerDone := make(chan struct{})
	go func() {
		defer close(handlerDone)
		h.Chat(w, req)
	}()

	// Wait for provider.Get to be called (handler is now inside SendToSandbox)
	select {
	case <-getCalled:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for provider.Get to be called")
	}

	// Cancel the request context — this simulates the client disconnecting
	cancelReq()

	// Give a moment for the cancellation to propagate
	time.Sleep(10 * time.Millisecond)

	// Let the mock provider.Get proceed
	close(proceedAfterGet)

	// Wait for the handler to finish
	select {
	case <-handlerDone:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for handler to finish")
	}

	// The key assertion: the context passed to provider.Get must NOT have been
	// cancelled at the time of the call, even though the request context was
	// cancelled before Get returned.
	mu.Lock()
	wasCancelled := contextWasCancelled
	wasChecked := contextChecked
	mu.Unlock()

	if !wasChecked {
		t.Fatal("provider.Get was never called")
	}
	if wasCancelled {
		t.Error("context passed to sandbox provider was cancelled during the call; " +
			"client disconnect should not cancel sandbox operations")
	}
}

// TestChat_ClientDisconnect_StatusRemainsRunning verifies that when a client
// disconnects before the completion finishes, the session status is NOT reset
// to "ready" — it stays "running" because the sandbox is still processing.
func TestChat_ClientDisconnect_StatusRemainsRunning(t *testing.T) {
	s := setupChatTestStore(t)
	provider := mocksandbox.NewProvider()
	sessionID := "session-status-test"

	seedSession(t, s, sessionID)

	// Create and start the sandbox
	ctx := context.Background()
	_, err := provider.Create(ctx, sessionID, sandbox.CreateOptions{
		SharedSecret:  "test-secret",
		WorkspacePath: "/workspace",
	})
	if err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}
	if err := provider.Start(ctx, sessionID); err != nil {
		t.Fatalf("failed to start sandbox: %v", err)
	}

	// Use a custom HTTP handler that streams SSE slowly so the handler
	// is in the streaming loop when we cancel the request context.
	sseStarted := make(chan struct{})
	provider.HTTPHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/chat") && r.Method == "POST" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/chat/stream") && r.Method == "GET" {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			// Signal that SSE streaming has started
			close(sseStarted)
			// Block until request context is done (simulates a long-running completion)
			<-r.Context().Done()
			return
		}
		http.NotFound(w, r)
	})

	h := newChatTestHandler(t, s, provider)

	reqCtx, cancelReq := context.WithCancel(context.Background())
	req := makeChatRequest(reqCtx, t, ChatRequest{
		ID:       sessionID,
		Messages: json.RawMessage(`[{"role":"user","parts":[{"type":"text","text":"hello"}]}]`),
	})
	w := httptest.NewRecorder()

	handlerDone := make(chan struct{})
	go func() {
		defer close(handlerDone)
		h.Chat(w, req)
	}()

	// Wait for the handler to reach the SSE streaming phase
	select {
	case <-sseStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for SSE stream to start")
	}

	// Cancel the request — simulates client disconnect mid-stream
	cancelReq()

	// Wait for handler to exit
	select {
	case <-handlerDone:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for handler to finish")
	}

	// Check that session status is still "running", NOT "ready"
	session, err := s.GetSessionByID(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}
	if session.Status != model.SessionStatusRunning {
		t.Errorf("expected session status %q after client disconnect, got %q",
			model.SessionStatusRunning, session.Status)
	}
}

// TestChat_CompletionFinishes_StatusResetsToReady verifies that when a completion
// finishes normally (DONE signal), the session status is reset to "ready".
func TestChat_CompletionFinishes_StatusResetsToReady(t *testing.T) {
	s := setupChatTestStore(t)
	provider := mocksandbox.NewProvider()
	sessionID := "session-done-test"

	seedSession(t, s, sessionID)

	// Create and start the sandbox
	ctx := context.Background()
	_, err := provider.Create(ctx, sessionID, sandbox.CreateOptions{
		SharedSecret:  "test-secret",
		WorkspacePath: "/workspace",
	})
	if err != nil {
		t.Fatalf("failed to create sandbox: %v", err)
	}
	if err := provider.Start(ctx, sessionID); err != nil {
		t.Fatalf("failed to start sandbox: %v", err)
	}

	// Default mock handler sends [DONE] immediately, which is what we want
	h := newChatTestHandler(t, s, provider)

	req := makeChatRequest(context.Background(), t, ChatRequest{
		ID:       sessionID,
		Messages: json.RawMessage(`[{"role":"user","parts":[{"type":"text","text":"hello"}]}]`),
	})
	w := httptest.NewRecorder()

	h.Chat(w, req)

	// Verify we got a 200 with SSE content
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Check that session status was reset to "ready" after completion
	session, err := s.GetSessionByID(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}
	if session.Status != model.SessionStatusReady {
		t.Errorf("expected session status %q after completion, got %q",
			model.SessionStatusReady, session.Status)
	}

	// Verify the response body contains [DONE]
	body := w.Body.String()
	if !bytes.Contains([]byte(body), []byte("data: [DONE]")) {
		t.Errorf("expected response to contain 'data: [DONE]', got: %s", body)
	}
}
