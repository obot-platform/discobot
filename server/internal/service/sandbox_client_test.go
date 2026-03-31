package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/obot-platform/discobot/server/internal/sandbox"
)

// mockSandboxProvider implements sandbox.Provider for testing SandboxChatClient.
// Only Get, GetSecret, and HTTPClient are used by SandboxChatClient.
type mockSandboxProvider struct {
	secret  string
	handler http.Handler           // Handler for HTTPClient to use
	onStop  func(sessionID string) // Callback when Stop is called
}

func (m *mockSandboxProvider) ImageExists(_ context.Context) bool {
	return true
}

func (m *mockSandboxProvider) Image() string {
	return "test-image"
}

func (m *mockSandboxProvider) Create(_ context.Context, _ string, _ sandbox.CreateOptions) (*sandbox.Sandbox, error) {
	return &sandbox.Sandbox{Status: sandbox.StatusCreated}, nil
}

func (m *mockSandboxProvider) Get(_ context.Context, _ string) (*sandbox.Sandbox, error) {
	return &sandbox.Sandbox{
		Status: sandbox.StatusRunning,
	}, nil
}

func (m *mockSandboxProvider) Start(_ context.Context, _ string) error {
	return nil
}

func (m *mockSandboxProvider) Stop(_ context.Context, sessionID string, _ time.Duration) error {
	if m.onStop != nil {
		m.onStop(sessionID)
	}
	return nil
}

func (m *mockSandboxProvider) Remove(_ context.Context, _ string, _ ...sandbox.RemoveOption) error {
	return nil
}

func (m *mockSandboxProvider) Exec(_ context.Context, _ string, _ []string, _ sandbox.ExecOptions) (*sandbox.ExecResult, error) {
	return &sandbox.ExecResult{ExitCode: 0}, nil
}

func (m *mockSandboxProvider) Attach(_ context.Context, _ string, _ sandbox.AttachOptions) (sandbox.PTY, error) {
	return nil, nil
}

func (m *mockSandboxProvider) ExecStream(_ context.Context, _ string, _ []string, _ sandbox.ExecStreamOptions) (sandbox.Stream, error) {
	return nil, nil
}

func (m *mockSandboxProvider) List(_ context.Context) ([]*sandbox.Sandbox, error) {
	return nil, nil
}

func (m *mockSandboxProvider) GetSecret(_ context.Context, _ string) (string, error) {
	return m.secret, nil
}

func (m *mockSandboxProvider) HTTPClient(_ context.Context, _ string) (*http.Client, error) {
	if m.handler != nil {
		return &http.Client{Transport: &testRoundTripper{handler: m.handler}}, nil
	}
	return &http.Client{}, nil
}

// testRoundTripper implements http.RoundTripper for testing.
type testRoundTripper struct {
	handler http.Handler
}

func (t *testRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	t.handler.ServeHTTP(rec, req)
	return rec.Result(), nil
}

func (m *mockSandboxProvider) Watch(_ context.Context) (<-chan sandbox.StateEvent, error) {
	ch := make(chan sandbox.StateEvent)
	close(ch)
	return ch, nil
}

func (m *mockSandboxProvider) Reconcile(_ context.Context) error {
	return nil
}

func (m *mockSandboxProvider) RemoveProject(_ context.Context, _ string) error {
	return nil
}

func TestSandboxChatClient_SendMessages_Returns202ThenStreams(t *testing.T) {
	// Track request sequence
	var postCalled, getCalled bool

	// Create handler that simulates agent-api behavior
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/chat") {
			postCalled = true
			// Return 202 Accepted (completion started)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]string{
				"completionId": "test-123",
				"status":       "started",
			})
			return
		}

		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/chat/stream") {
			getCalled = true
			// Return SSE stream
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("event: chunk\n"))
			w.Write([]byte("data: {\"type\":\"text\"}\n\n"))
			w.Write([]byte("event: done\n"))
			w.Write([]byte("data: {}\n\n"))
			return
		}

		t.Errorf("Unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	})

	// Create client with mock provider
	provider := &mockSandboxProvider{handler: handler}
	client := NewSandboxChatClient(provider, nil, nil)

	// Send messages
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	messages := json.RawMessage(`[{"role":"user","content":"hello"}]`)
	ch, err := client.SendMessages(ctx, "test-session", "test-session", messages, "", nil)
	if err != nil {
		t.Fatalf("SendMessages failed: %v", err)
	}

	// Verify POST was called first, then GET
	if !postCalled {
		t.Error("POST /chat was not called")
	}
	if !getCalled {
		t.Error("GET /chat was not called after 202")
	}

	// Read SSE events
	var events []SSELine
	for line := range ch {
		events = append(events, line)
	}

	// Should have received data event and done signal
	if len(events) != 2 {
		t.Errorf("Expected 2 events, got %d", len(events))
	}
	if len(events) > 0 && events[0].Data != `{"type":"text"}` {
		t.Errorf("Expected text event, got %s", events[0].Data)
	}
	if len(events) > 1 && !events[1].Done {
		t.Error("Expected Done signal")
	}
}

func TestSandboxChatClient_StartChat_UsesProvidedThreadID(t *testing.T) {
	var requestedPath string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/chat") {
			requestedPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]string{
				"completionId": "test-123",
				"status":       "started",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	provider := &mockSandboxProvider{handler: handler}
	client := NewSandboxChatClient(provider, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	messages := json.RawMessage(`[{"role":"user","content":"hello"}]`)
	_, err := client.StartChat(ctx, "test-session", "thread-custom", messages, "", nil)
	if err != nil {
		t.Fatalf("StartChat failed: %v", err)
	}

	if requestedPath != "/threads/thread-custom/chat" {
		t.Fatalf("expected thread-specific path %q, got %q", "/threads/thread-custom/chat", requestedPath)
	}
}

func TestSandboxChatClient_SendMessages_Non202Error(t *testing.T) {
	// Create handler that returns 400 Bad Request
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/chat") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "messages array required",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	provider := &mockSandboxProvider{handler: handler}
	client := NewSandboxChatClient(provider, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	messages := json.RawMessage(`[]`)
	_, err := client.SendMessages(ctx, "test-session", "test-session", messages, "", nil)
	if err == nil {
		t.Fatal("Expected error for 400 response")
	}

	// Error message should include status code
	if !contains(err.Error(), "400") {
		t.Errorf("Expected error to contain '400', got: %s", err.Error())
	}
}

func TestSandboxChatClient_SendMessages_409Conflict(t *testing.T) {
	// Create handler that returns 409 Conflict (completion already running)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/chat") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{
				"error":        "completion_in_progress",
				"completionId": "existing-456",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	provider := &mockSandboxProvider{handler: handler}
	client := NewSandboxChatClient(provider, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	messages := json.RawMessage(`[{"role":"user","content":"hello"}]`)
	_, err := client.SendMessages(ctx, "test-session", "test-session", messages, "", nil)
	if err == nil {
		t.Fatal("Expected error for 409 response")
	}

	// Error message should include status code and conflict info
	if !contains(err.Error(), "409") {
		t.Errorf("Expected error to contain '409', got: %s", err.Error())
	}
}

func TestSandboxChatClient_StartChat_InterruptedTurnConflictIncludesCompletionID(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/chat") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{
				"error":        "interrupted_turn_requires_resume",
				"message":      "This thread has an interrupted turn that must resume before sending a new message.",
				"completionId": "resume-123",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	provider := &mockSandboxProvider{handler: handler}
	client := NewSandboxChatClient(provider, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	messages := json.RawMessage(`[{"role":"user","content":"hello"}]`)
	_, err := client.StartChat(ctx, "test-session", "test-thread", messages, "", nil)
	if err == nil {
		t.Fatal("Expected error for interrupted turn conflict")
	}

	var startErr *SandboxChatStartError
	if !errors.As(err, &startErr) {
		t.Fatalf("expected SandboxChatStartError, got %T", err)
	}
	if startErr.ErrorCode != "interrupted_turn_requires_resume" {
		t.Fatalf("expected interrupted turn error code, got %#v", startErr)
	}
	if startErr.CompletionID != "resume-123" {
		t.Fatalf("expected completionId resume-123, got %#v", startErr)
	}
}

func TestSandboxChatClient_GetStream_RejectsNoContent(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/chat/stream") {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	provider := &mockSandboxProvider{handler: handler}
	client := NewSandboxChatClient(provider, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.GetStream(ctx, "test-session", "test-session", nil)
	if err == nil {
		t.Fatal("expected GetStream to reject 204 response")
	}
	if !strings.Contains(err.Error(), "sandbox returned status 204") {
		t.Fatalf("expected 204 status error, got %v", err)
	}
}

func TestSandboxChatClient_GetStream_EmptySSEStream(t *testing.T) {
	// Create handler that returns 200 with X-Discobot-Completion-Active: false
	// and then immediately closes the stream without any events.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/chat/stream") {
			w.Header().Set("X-Discobot-Completion-Active", "false")
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	provider := &mockSandboxProvider{handler: handler}
	client := NewSandboxChatClient(provider, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := client.GetStream(ctx, "test-session", "test-session", nil)
	if err != nil {
		t.Fatalf("GetStream failed: %v", err)
	}

	// Channel should be closed immediately with no events.
	var count int
	for range ch {
		count++
	}
	if count != 0 {
		t.Errorf("Expected 0 events for empty SSE stream, got %d", count)
	}
}

func TestSandboxChatClient_GetStream_PreservesEventAndID(t *testing.T) {
	var receivedLastEventID string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/chat/stream") {
			receivedLastEventID = r.Header.Get("Last-Event-ID")
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("event: history-start\n"))
			_, _ = w.Write([]byte("data: {}\n\n"))
			_, _ = w.Write([]byte("id: completion-1:0\n"))
			_, _ = w.Write([]byte("event: history-message\n"))
			_, _ = w.Write([]byte("data: {\"id\":\"msg-1\"}\n\n"))
			_, _ = w.Write([]byte("event: done\n"))
			_, _ = w.Write([]byte("data: {}\n\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	provider := &mockSandboxProvider{handler: handler}
	client := NewSandboxChatClient(provider, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := client.GetStream(ctx, "test-session", "test-session", &RequestOptions{LastEventID: "completion-1:0"})
	if err != nil {
		t.Fatalf("GetStream failed: %v", err)
	}

	var events []SSELine
	for line := range ch {
		events = append(events, line)
	}

	if receivedLastEventID != "completion-1:0" {
		t.Fatalf("expected Last-Event-ID to be forwarded, got %q", receivedLastEventID)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 SSE events, got %d", len(events))
	}
	if events[0].Event != "history-start" || events[0].Data != "{}" {
		t.Fatalf("unexpected history-start event: %+v", events[0])
	}
	if events[1].ID != "completion-1:0" || events[1].Event != "history-message" || events[1].Data != `{"id":"msg-1"}` {
		t.Fatalf("unexpected message event: %+v", events[1])
	}
	if events[2].Event != "done" || !events[2].Done {
		t.Fatalf("expected final DONE event, got %+v", events[2])
	}
}

func TestSandboxChatClient_GetStream_AllowsLargeSSEDataLine(t *testing.T) {
	largeDelta := strings.Repeat("x", 70*1024)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/chat/stream") {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("data: " + largeDelta + "\n\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	provider := &mockSandboxProvider{handler: handler}
	client := NewSandboxChatClient(provider, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := client.GetStream(ctx, "test-session", "test-session", nil)
	if err != nil {
		t.Fatalf("GetStream failed: %v", err)
	}

	var events []SSELine
	for line := range ch {
		events = append(events, line)
	}

	if len(events) != 1 {
		t.Fatalf("Expected 1 SSE data event, got %d", len(events))
	}
	if events[0].Done {
		t.Fatal("Expected data event, got done signal")
	}
	if events[0].Data != largeDelta {
		t.Fatalf("Expected large delta to pass through unchanged, got %d bytes", len(events[0].Data))
	}
}

func TestSandboxChatClient_GetStream_AllowsVeryLargeSSEDataLine(t *testing.T) {
	largeDelta := strings.Repeat("x", 2*1024*1024)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/chat/stream") {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("data: " + largeDelta + "\n\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	provider := &mockSandboxProvider{handler: handler}
	client := NewSandboxChatClient(provider, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := client.GetStream(ctx, "test-session", "test-session", nil)
	if err != nil {
		t.Fatalf("GetStream failed: %v", err)
	}

	var events []SSELine
	for line := range ch {
		events = append(events, line)
	}

	if len(events) != 1 {
		t.Fatalf("Expected 1 SSE data event, got %d", len(events))
	}
	if events[0].Done {
		t.Fatal("Expected data event, got done signal")
	}
	if events[0].Data != largeDelta {
		t.Fatalf("Expected very large delta to pass through unchanged, got %d bytes", len(events[0].Data))
	}
}

func TestSandboxChatClient_GetServiceOutput_AllowsVeryLargeSSEDataLine(t *testing.T) {
	largeDelta := strings.Repeat("x", 2*1024*1024)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/services/test-service/output") {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("data: " + largeDelta + "\n\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	provider := &mockSandboxProvider{handler: handler}
	client := NewSandboxChatClient(provider, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := client.GetServiceOutput(ctx, "test-session", "test-service")
	if err != nil {
		t.Fatalf("GetServiceOutput failed: %v", err)
	}

	var events []SSELine
	for line := range ch {
		events = append(events, line)
	}

	if len(events) != 1 {
		t.Fatalf("Expected 1 SSE data event, got %d", len(events))
	}
	if events[0].Done {
		t.Fatal("Expected data event, got done signal")
	}
	if events[0].Data != largeDelta {
		t.Fatalf("Expected very large service output to pass through unchanged, got %d bytes", len(events[0].Data))
	}
}

func TestSandboxChatClient_SendMessages_WithCredentials(t *testing.T) {
	var receivedCredentials string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/chat") {
			receivedCredentials = r.Header.Get("X-Discobot-Credentials")
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]string{
				"completionId": "test-123",
				"status":       "started",
			})
			return
		}
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/chat/stream") {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	provider := &mockSandboxProvider{handler: handler}

	// Create client with credential fetcher that returns test credentials
	fetcher := func(_ context.Context, _ string) ([]CredentialEnvVar, error) {
		return []CredentialEnvVar{
			{EnvVar: "API_KEY", Value: "secret123"},
		}, nil
	}
	client := NewSandboxChatClient(provider, fetcher, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	messages := json.RawMessage(`[{"role":"user","content":"hello"}]`)

	// Credentials are automatically fetched by the client
	ch, err := client.SendMessages(ctx, "test-session", "test-session", messages, "", nil)
	if err != nil {
		t.Fatalf("SendMessages failed: %v", err)
	}

	// Drain channel to completion
	for range ch { //nolint:revive // empty block intentionally drains channel
	}

	// Verify credentials were sent
	if receivedCredentials == "" {
		t.Error("Expected credentials header to be set")
	}
	if !contains(receivedCredentials, "API_KEY") {
		t.Errorf("Expected credentials to contain API_KEY, got: %s", receivedCredentials)
	}
}

func TestSandboxChatClient_SendMessages_WithAuthorization(t *testing.T) {
	var receivedAuth string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/chat") {
			receivedAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]string{
				"completionId": "test-123",
				"status":       "started",
			})
			return
		}
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/chat/stream") {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	provider := &mockSandboxProvider{handler: handler, secret: "my-secret-token"}
	client := NewSandboxChatClient(provider, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	messages := json.RawMessage(`[{"role":"user","content":"hello"}]`)
	ch, err := client.SendMessages(ctx, "test-session", "test-session", messages, "", nil)
	if err != nil {
		t.Fatalf("SendMessages failed: %v", err)
	}

	// Drain channel to completion
	for range ch { //nolint:revive // empty block intentionally drains channel
	}

	// Verify authorization header was set
	expected := "Bearer my-secret-token"
	if receivedAuth != expected {
		t.Errorf("Expected Authorization: %s, got: %s", expected, receivedAuth)
	}
}

func TestSandboxChatClient_SendMessages_RetriesOnEOF(t *testing.T) {
	var attempts atomic.Int32

	// Create a round tripper that fails with EOF twice, then succeeds
	failingTransport := &eofThenSuccessTransport{
		failCount: 2,
		attempts:  &attempts,
	}

	provider := &mockSandboxProviderWithTransport{
		transport: failingTransport,
	}
	client := NewSandboxChatClient(provider, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	messages := json.RawMessage(`[{"role":"user","content":"hello"}]`)
	ch, err := client.SendMessages(ctx, "test-session", "test-session", messages, "", nil)
	if err != nil {
		t.Fatalf("SendMessages failed: %v", err)
	}

	// Drain channel
	for range ch { //nolint:revive // empty block intentionally drains channel
	}

	// Should have retried: 2 EOF failures + 1 success for POST + 1 for GET = 4 total
	// But we only count POST attempts in our transport
	totalAttempts := attempts.Load()
	if totalAttempts < 3 {
		t.Errorf("Expected at least 3 attempts (2 EOF + 1 success), got %d", totalAttempts)
	}
}

func TestIsRetryableError_EOF(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"io.EOF", io.EOF, true},
		{"io.ErrUnexpectedEOF", io.ErrUnexpectedEOF, true},
		{"wrapped EOF", fmt.Errorf("request failed: %w", io.EOF), true},
		{"EOF in string", fmt.Errorf("connection closed: EOF"), true},
		{"unrelated error", fmt.Errorf("some other error"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRetryableError(tt.err)
			if result != tt.expected {
				t.Errorf("isRetryableError(%v) = %v, expected %v", tt.err, result, tt.expected)
			}
		})
	}
}

// eofThenSuccessTransport returns EOF errors for the first N requests, then succeeds.
type eofThenSuccessTransport struct {
	failCount int
	attempts  *atomic.Int32
}

func (t *eofThenSuccessTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	attempt := t.attempts.Add(1)

	// Fail with EOF for the first failCount attempts
	if int(attempt) <= t.failCount {
		return nil, io.EOF
	}

	// After failures, return success responses
	rec := httptest.NewRecorder()
	if req.Method == "POST" {
		rec.Header().Set("Content-Type", "application/json")
		rec.WriteHeader(http.StatusAccepted)
		json.NewEncoder(rec).Encode(map[string]string{"status": "started"})
	} else {
		// GET request for stream
		rec.Header().Set("Content-Type", "text/event-stream")
		rec.WriteHeader(http.StatusOK)
		rec.Write([]byte("data: [DONE]\n\n"))
	}
	return rec.Result(), nil
}

// mockSandboxProviderWithTransport allows injecting a custom transport.
type mockSandboxProviderWithTransport struct {
	transport http.RoundTripper
}

func (m *mockSandboxProviderWithTransport) ImageExists(_ context.Context) bool { return true }
func (m *mockSandboxProviderWithTransport) Image() string                      { return "test-image" }
func (m *mockSandboxProviderWithTransport) Create(_ context.Context, _ string, _ sandbox.CreateOptions) (*sandbox.Sandbox, error) {
	return &sandbox.Sandbox{Status: sandbox.StatusCreated}, nil
}
func (m *mockSandboxProviderWithTransport) Get(_ context.Context, _ string) (*sandbox.Sandbox, error) {
	return &sandbox.Sandbox{Status: sandbox.StatusRunning}, nil
}
func (m *mockSandboxProviderWithTransport) Start(_ context.Context, _ string) error { return nil }
func (m *mockSandboxProviderWithTransport) Stop(_ context.Context, _ string, _ time.Duration) error {
	return nil
}
func (m *mockSandboxProviderWithTransport) Remove(_ context.Context, _ string, _ ...sandbox.RemoveOption) error {
	return nil
}
func (m *mockSandboxProviderWithTransport) Exec(_ context.Context, _ string, _ []string, _ sandbox.ExecOptions) (*sandbox.ExecResult, error) {
	return &sandbox.ExecResult{ExitCode: 0}, nil
}
func (m *mockSandboxProviderWithTransport) Attach(_ context.Context, _ string, _ sandbox.AttachOptions) (sandbox.PTY, error) {
	return nil, nil
}
func (m *mockSandboxProviderWithTransport) ExecStream(_ context.Context, _ string, _ []string, _ sandbox.ExecStreamOptions) (sandbox.Stream, error) {
	return nil, nil
}
func (m *mockSandboxProviderWithTransport) List(_ context.Context) ([]*sandbox.Sandbox, error) {
	return nil, nil
}
func (m *mockSandboxProviderWithTransport) GetSecret(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (m *mockSandboxProviderWithTransport) HTTPClient(_ context.Context, _ string) (*http.Client, error) {
	return &http.Client{Transport: m.transport}, nil
}
func (m *mockSandboxProviderWithTransport) Watch(_ context.Context) (<-chan sandbox.StateEvent, error) {
	ch := make(chan sandbox.StateEvent)
	close(ch)
	return ch, nil
}

func (m *mockSandboxProviderWithTransport) Reconcile(_ context.Context) error {
	return nil
}

func (m *mockSandboxProviderWithTransport) RemoveProject(_ context.Context, _ string) error {
	return nil
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestSandboxChatClient_SendMessages_WithGitConfig(t *testing.T) {
	var receivedGitUserName, receivedGitUserEmail string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/chat") {
			receivedGitUserName = r.Header.Get("X-Discobot-Git-User-Name")
			receivedGitUserEmail = r.Header.Get("X-Discobot-Git-User-Email")
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]string{
				"completionId": "test-123",
				"status":       "started",
			})
			return
		}
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/chat/stream") {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	provider := &mockSandboxProvider{handler: handler}
	client := NewSandboxChatClient(provider, nil, &SandboxChatClientConfig{
		GitUserName:  "Test User",
		GitUserEmail: "test@example.com",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	messages := json.RawMessage(`[{"role":"user","content":"hello"}]`)

	ch, err := client.SendMessages(ctx, "test-session", "test-session", messages, "", nil)
	if err != nil {
		t.Fatalf("SendMessages failed: %v", err)
	}

	// Drain channel to completion
	for range ch { //nolint:revive // empty block intentionally drains channel
	}

	// Verify git config headers were sent
	if receivedGitUserName != "Test User" {
		t.Errorf("Expected X-Discobot-Git-User-Name: Test User, got: %s", receivedGitUserName)
	}
	if receivedGitUserEmail != "test@example.com" {
		t.Errorf("Expected X-Discobot-Git-User-Email: test@example.com, got: %s", receivedGitUserEmail)
	}
}

func TestSandboxChatClient_SendMessages_WithPartialGitConfig(t *testing.T) {
	var receivedGitUserName, receivedGitUserEmail string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/chat") {
			receivedGitUserName = r.Header.Get("X-Discobot-Git-User-Name")
			receivedGitUserEmail = r.Header.Get("X-Discobot-Git-User-Email")
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]string{
				"completionId": "test-123",
				"status":       "started",
			})
			return
		}
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/chat/stream") {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	provider := &mockSandboxProvider{handler: handler}
	client := NewSandboxChatClient(provider, nil, &SandboxChatClientConfig{
		GitUserName: "Name Only User",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	messages := json.RawMessage(`[{"role":"user","content":"hello"}]`)

	ch, err := client.SendMessages(ctx, "test-session", "test-session", messages, "", nil)
	if err != nil {
		t.Fatalf("SendMessages failed: %v", err)
	}

	// Drain channel to completion
	for range ch { //nolint:revive // empty block intentionally drains channel
	}

	// Verify only name header was sent
	if receivedGitUserName != "Name Only User" {
		t.Errorf("Expected X-Discobot-Git-User-Name: Name Only User, got: %s", receivedGitUserName)
	}
	if receivedGitUserEmail != "" {
		t.Errorf("Expected no X-Discobot-Git-User-Email header, got: %s", receivedGitUserEmail)
	}
}

func TestSandboxChatClient_GetDiff_ReturnsCorrectResponseType(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		format       string
		responseBody string
		checkResult  func(t *testing.T, result any)
	}{
		{
			name:   "full diff response",
			path:   "",
			format: "",
			responseBody: `{
				"files": [{"path": "test.txt", "status": "modified", "additions": 1, "deletions": 0, "binary": false}],
				"stats": {"filesChanged": 1, "additions": 1, "deletions": 0}
			}`,
			checkResult: func(t *testing.T, result any) {
				t.Helper()
				// Just verify result is non-nil for full diff
				if result == nil {
					t.Error("Expected non-nil result for full diff response")
				}
			},
		},
		{
			name:   "single file response",
			path:   "test.txt",
			format: "",
			responseBody: `{
				"path": "test.txt",
				"status": "modified",
				"additions": 5,
				"deletions": 2,
				"binary": false,
				"patch": "@@ -1 +1 @@\n-old\n+new"
			}`,
			checkResult: func(t *testing.T, result any) {
				t.Helper()
				// Should have path field
				if result == nil {
					t.Error("Expected non-nil result")
				}
			},
		},
		{
			name:   "files format response",
			path:   "",
			format: "files",
			responseBody: `{
				"files": [{"path": "test.txt", "status": "modified"}],
				"stats": {"filesChanged": 1, "additions": 1, "deletions": 0}
			}`,
			checkResult: func(t *testing.T, result any) {
				t.Helper()
				if result == nil {
					t.Error("Expected non-nil result")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "GET" && r.URL.Path == "/diff" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(tt.responseBody))
					return
				}
				w.WriteHeader(http.StatusNotFound)
			})

			provider := &mockSandboxProvider{handler: handler}
			client := NewSandboxChatClient(provider, nil, nil)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			result, err := client.GetDiff(ctx, "test-session", tt.path, tt.format)
			if err != nil {
				t.Fatalf("GetDiff failed: %v", err)
			}

			if result == nil {
				t.Error("Expected non-nil result")
			}

			if tt.checkResult != nil {
				tt.checkResult(t, result)
			}
		})
	}
}

func TestSandboxChatClient_GetDiff_EncodesPathQuery(t *testing.T) {
	var gotPath string
	var gotFormat string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/diff" {
			gotPath = r.URL.Query().Get("path")
			gotFormat = r.URL.Query().Get("format")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"path":"ui/src/routes/+layout.svelte","status":"modified","patch":""}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	provider := &mockSandboxProvider{handler: handler}
	client := NewSandboxChatClient(provider, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	path := "ui/src/routes/+layout.svelte"
	_, err := client.GetDiff(ctx, "test-session", path, "files")
	if err != nil {
		t.Fatalf("GetDiff failed: %v", err)
	}

	if gotPath != path {
		t.Fatalf("expected path %q, got %q", path, gotPath)
	}
	if gotFormat != "files" {
		t.Fatalf("expected format %q, got %q", "files", gotFormat)
	}
}

func TestSandboxChatClient_GetQuestion_PreservesQuestionNotes(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/threads/test-session/chat/question/tool-123" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"status": "pending",
				"question": {
					"toolUseID": "tool-123",
					"questions": [
						{
							"question": "How should I proceed?",
							"header": "Mode",
							"options": [
								{"label": "Fast", "description": "Move quickly"}
							],
							"multiSelect": false,
							"notes": "Use the staged migration steps."
						}
					]
				}
			}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	provider := &mockSandboxProvider{handler: handler}
	client := NewSandboxChatClient(provider, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.GetQuestion(ctx, "test-session", "test-session", "tool-123")
	if err != nil {
		t.Fatalf("GetQuestion failed: %v", err)
	}

	if result == nil || result.Question == nil {
		t.Fatal("Expected pending question response")
	}

	if len(result.Question.Questions) != 1 {
		t.Fatalf("Expected 1 question, got %d", len(result.Question.Questions))
	}

	if result.Question.Questions[0].Notes != "Use the staged migration steps." {
		t.Fatalf("Expected notes to be preserved, got %q", result.Question.Questions[0].Notes)
	}
}

func TestSandboxChatClient_ListFiles_EncodesPathQuery(t *testing.T) {
	var gotPath string
	var gotHidden string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/files" {
			gotPath = r.URL.Query().Get("path")
			gotHidden = r.URL.Query().Get("hidden")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"path":".","entries":[]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	provider := &mockSandboxProvider{handler: handler}
	client := NewSandboxChatClient(provider, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	path := "ui/src/routes/+layout.svelte"
	_, err := client.ListFiles(ctx, "test-session", path, true)
	if err != nil {
		t.Fatalf("ListFiles failed: %v", err)
	}

	if gotPath != path {
		t.Fatalf("expected path %q, got %q", path, gotPath)
	}
	if gotHidden != "true" {
		t.Fatalf("expected hidden=true, got %q", gotHidden)
	}
}

func TestSandboxChatClient_SearchFiles_EncodesQuery(t *testing.T) {
	var gotQuery string
	var gotLimit string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/files/search" {
			gotQuery = r.URL.Query().Get("q")
			gotLimit = r.URL.Query().Get("limit")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"query":"","results":[]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	provider := &mockSandboxProvider{handler: handler}
	client := NewSandboxChatClient(provider, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := "+layout.svelte"
	_, err := client.SearchFiles(ctx, "test-session", query, 25)
	if err != nil {
		t.Fatalf("SearchFiles failed: %v", err)
	}

	if gotQuery != query {
		t.Fatalf("expected query %q, got %q", query, gotQuery)
	}
	if gotLimit != "25" {
		t.Fatalf("expected limit 25, got %q", gotLimit)
	}
}

func TestSandboxChatClient_ReadFile_EncodesPathQuery(t *testing.T) {
	var gotPath string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" && r.URL.Path == "/files/read" {
			gotPath = r.URL.Query().Get("path")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"path":"ui/src/routes/+layout.svelte","content":"<script></script>","encoding":"utf8","size":17}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	provider := &mockSandboxProvider{handler: handler}
	client := NewSandboxChatClient(provider, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	path := "ui/src/routes/+layout.svelte"
	result, err := client.ReadFile(ctx, "test-session", path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if gotPath != path {
		t.Fatalf("expected path %q, got %q", path, gotPath)
	}
	if result == nil || result.Path != path {
		t.Fatalf("expected response path %q, got %#v", path, result)
	}
}
