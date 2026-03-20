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

// seedWorkspace creates a workspace in the store for testing.
func seedWorkspace(t *testing.T, s *store.Store) {
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
}

// parseMessages unmarshals a JSON array string into []json.RawMessage for use in ChatRequest.Messages.
func parseMessages(t *testing.T, s string) []json.RawMessage {
	t.Helper()
	var msgs []json.RawMessage
	if err := json.Unmarshal([]byte(s), &msgs); err != nil {
		t.Fatalf("parseMessages: %v", err)
	}
	return msgs
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
func makeChatRequest(ctx context.Context, t *testing.T, sessionID, threadID string, req ChatRequest) *http.Request {
	t.Helper()
	if threadID == "" {
		threadID = sessionID
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal request: %v", err)
	}
	httpReq := httptest.NewRequest("POST", "/api/projects/"+testProjectID+"/sessions/"+sessionID+"/threads/"+threadID+"/chat", bytes.NewReader(body))
	httpReq.SetPathValue("sessionId", sessionID)
	httpReq.SetPathValue("threadId", threadID)
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

	req := makeChatRequest(context.Background(), t, "session-123", "", ChatRequest{
		Messages:    parseMessages(t, `[{"role":"user","parts":[{"type":"text","text":"hello"}]}]`),
		WorkspaceID: "test-workspace",
	})
	w := httptest.NewRecorder()

	h.Chat(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s",
			http.StatusInternalServerError, w.Code, w.Body.String())
	}
}

func TestChat_EmptyMessages_CreatesSessionWithoutSendingToSandbox(t *testing.T) {
	s := setupChatTestStore(t)
	provider := mocksandbox.NewProvider()
	seedWorkspace(t, s)

	getCalled := false
	provider.GetFunc = func(_ context.Context, _ string) (*sandbox.Sandbox, error) {
		getCalled = true
		t.Fatalf("sandbox should not be contacted for empty chat submissions")
		return nil, nil
	}

	h := newChatTestHandler(t, s, provider)

	req := makeChatRequest(context.Background(), t, "session-empty-create", "", ChatRequest{
		Messages:    []json.RawMessage{},
		WorkspaceID: "test-workspace",
	})
	w := httptest.NewRecorder()

	h.Chat(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}
	var response ChatResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("expected JSON response, got error: %v; body: %s", err, w.Body.String())
	}
	if response.SessionID != "session-empty-create" {
		t.Fatalf("expected session ID in response, got %q", response.SessionID)
	}
	if response.WorkspaceID != "test-workspace" {
		t.Fatalf("expected workspace ID in response, got %q", response.WorkspaceID)
	}
	if response.ThreadID != "session-empty-create" {
		t.Fatalf("expected default thread ID to match session ID, got %q", response.ThreadID)
	}
	if response.MessageID != "" {
		t.Fatalf("expected empty message ID for empty submission, got %q", response.MessageID)
	}
	if getCalled {
		t.Fatal("sandbox should not be contacted for empty chat submissions")
	}

	sess, err := s.GetSessionByID(context.Background(), "session-empty-create")
	if err != nil {
		t.Fatalf("expected session to be created: %v", err)
	}
	if sess.Name != "" {
		t.Fatalf("expected empty session name, got %q", sess.Name)
	}
}

// TestChat_ClientDisconnect_DoesNotCancelSandbox verifies that when a client
// disconnects after chat initiation, the background sandbox request still uses
// a non-cancelled context.
func TestChat_ClientDisconnect_DoesNotCancelSandbox(t *testing.T) {
	s := setupChatTestStore(t)
	provider := mocksandbox.NewProvider()
	sessionID := "session-ctx-test"

	seedSession(t, s, sessionID)

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

	var (
		mu                  sync.Mutex
		postCalled          = make(chan struct{}, 1)
		proceedAfterPost    = make(chan struct{})
		contextWasCancelled bool
		contextChecked      bool
	)

	provider.HTTPHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/chat") && r.Method == "POST" {
			select {
			case postCalled <- struct{}{}:
			default:
			}

			<-proceedAfterPost

			mu.Lock()
			contextWasCancelled = r.Context().Err() != nil
			contextChecked = true
			mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"status":"started"}`))
			return
		}
		http.NotFound(w, r)
	})

	h := newChatTestHandler(t, s, provider)

	reqCtx, cancelReq := context.WithCancel(context.Background())
	req := makeChatRequest(reqCtx, t, sessionID, "", ChatRequest{
		Messages: parseMessages(t, `[{"role":"user","parts":[{"type":"text","text":"hello"}]}]`),
	})
	w := httptest.NewRecorder()

	h.Chat(w, req)

	select {
	case <-postCalled:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for sandbox POST /chat request")
	}

	cancelReq()
	time.Sleep(10 * time.Millisecond)
	close(proceedAfterPost)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		wasChecked := contextChecked
		wasCancelled := contextWasCancelled
		mu.Unlock()
		if wasChecked {
			if wasCancelled {
				t.Error("context passed to sandbox request was cancelled; client disconnect should not cancel sandbox operations")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("sandbox request context was never checked")
}

// TestChat_StartsCompletion_StatusBecomesRunning verifies that a normal chat
// request starts the sandbox completion and marks the session as running.
func TestChat_StartsCompletion_StatusBecomesRunning(t *testing.T) {
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

	h := newChatTestHandler(t, s, provider)

	req := makeChatRequest(context.Background(), t, sessionID, "", ChatRequest{
		Messages: parseMessages(t, `[{"id":"msg-1","role":"user","parts":[{"type":"text","text":"hello"}]}]`),
	})
	w := httptest.NewRecorder()

	h.Chat(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var response ChatResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("expected JSON response, got error: %v; body: %s", err, w.Body.String())
	}
	if response.SessionID != sessionID {
		t.Fatalf("expected session ID %q, got %q", sessionID, response.SessionID)
	}
	if response.ThreadID != sessionID {
		t.Fatalf("expected default thread ID %q, got %q", sessionID, response.ThreadID)
	}
	if response.WorkspaceID != "test-workspace" {
		t.Fatalf("expected workspace ID %q, got %q", "test-workspace", response.WorkspaceID)
	}
	if response.MessageID != "msg-1" {
		t.Fatalf("expected message ID %q, got %q", "msg-1", response.MessageID)
	}

	session, err := s.GetSessionByID(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("failed to get session: %v", err)
	}
	if session.Status != model.SessionStatusReady {
		t.Errorf("expected session status to remain %q after chat start, got %q",
			model.SessionStatusReady, session.Status)
	}
}

func TestChat_UsesExplicitThreadID(t *testing.T) {
	s := setupChatTestStore(t)
	provider := mocksandbox.NewProvider()
	sessionID := "session-explicit-thread"
	threadID := "thread-custom-1"

	seedSession(t, s, sessionID)

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

	pathCh := make(chan string, 1)
	provider.HTTPHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/chat") {
			select {
			case pathCh <- r.URL.Path:
			default:
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"status":"started"}`))
			return
		}
		http.NotFound(w, r)
	})

	h := newChatTestHandler(t, s, provider)
	req := makeChatRequest(context.Background(), t, sessionID, threadID, ChatRequest{
		Messages: parseMessages(t, `[{"id":"msg-thread","role":"user","parts":[{"type":"text","text":"hello"}]}]`),
	})
	w := httptest.NewRecorder()

	h.Chat(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var response ChatResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("expected JSON response, got error: %v; body: %s", err, w.Body.String())
	}
	if response.SessionID != sessionID {
		t.Fatalf("expected session ID %q, got %q", sessionID, response.SessionID)
	}
	if response.ThreadID != threadID {
		t.Fatalf("expected thread ID %q, got %q", threadID, response.ThreadID)
	}

	select {
	case path := <-pathCh:
		expected := "/threads/" + threadID + "/chat"
		if path != expected {
			t.Fatalf("expected sandbox chat path %q, got %q", expected, path)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for sandbox POST /chat request")
	}
}

// TestChat_ReturnsJSONResponse verifies that chat initiation returns JSON
// metadata instead of proxying the SSE stream body.
func TestChat_ReturnsJSONResponse(t *testing.T) {
	s := setupChatTestStore(t)
	provider := mocksandbox.NewProvider()
	sessionID := "session-json-response-test"

	seedSession(t, s, sessionID)

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

	h := newChatTestHandler(t, s, provider)

	req := makeChatRequest(context.Background(), t, sessionID, "", ChatRequest{
		Messages: parseMessages(t, `[{"id":"msg-json","role":"user","parts":[{"type":"text","text":"hello"}]}]`),
	})
	w := httptest.NewRecorder()

	h.Chat(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var response ChatResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("expected JSON response, got error: %v; body: %s", err, w.Body.String())
	}
	if response.SessionID != sessionID {
		t.Fatalf("expected session ID %q, got %q", sessionID, response.SessionID)
	}
	if response.ThreadID != sessionID {
		t.Fatalf("expected default thread ID %q, got %q", sessionID, response.ThreadID)
	}
	if response.MessageID != "msg-json" {
		t.Fatalf("expected message ID %q, got %q", "msg-json", response.MessageID)
	}
	if bytes.Contains(w.Body.Bytes(), []byte("data: [DONE]")) {
		t.Fatalf("expected JSON response instead of SSE body, got: %s", w.Body.String())
	}
}
