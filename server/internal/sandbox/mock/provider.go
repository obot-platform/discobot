// Package mock provides a mock implementation of sandbox.Provider for testing.
package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/obot-platform/discobot/server/internal/sandbox"
)

// eventSubscriber represents a subscriber to sandbox events.
type eventSubscriber struct {
	ch   chan sandbox.StateEvent
	done chan struct{}
}

// DefaultMockImage is the default image used by the mock provider.
const DefaultMockImage = "mock:latest"

// Provider is a mock sandbox provider for testing.
type Provider struct {
	mu        sync.RWMutex
	sandboxes map[string]*sandbox.Sandbox
	secrets   map[string]string // sessionID -> raw secret
	image     string            // configured sandbox image

	// Event subscribers for Watch functionality
	subscribersMu sync.RWMutex
	subscribers   []*eventSubscriber

	// HTTPHandler is used by HTTPClient to handle requests without network.
	// If nil, a default handler that returns 202/200 is used.
	HTTPHandler        http.Handler
	defaultHTTPHandler http.Handler

	// Configurable behaviors for testing
	CreateFunc     func(ctx context.Context, sessionID string, opts sandbox.CreateOptions) (*sandbox.Sandbox, error)
	StartFunc      func(ctx context.Context, sessionID string) error
	StopFunc       func(ctx context.Context, sessionID string, timeout time.Duration) error
	RemoveFunc     func(ctx context.Context, sessionID string, opts ...sandbox.RemoveOption) error
	GetFunc        func(ctx context.Context, sessionID string) (*sandbox.Sandbox, error)
	GetSecretFunc  func(ctx context.Context, sessionID string) (string, error)
	ExecFunc       func(ctx context.Context, sessionID string, cmd []string, opts sandbox.ExecOptions) (*sandbox.ExecResult, error)
	AttachFunc     func(ctx context.Context, sessionID string, opts sandbox.AttachOptions) (sandbox.PTY, error)
	ExecStreamFunc func(ctx context.Context, sessionID string, cmd []string, opts sandbox.ExecStreamOptions) (sandbox.Stream, error)
	WatchFunc      func(ctx context.Context) (<-chan sandbox.StateEvent, error)
}

// NewProvider creates a new mock provider with default behavior.
func NewProvider() *Provider {
	return &Provider{
		sandboxes: make(map[string]*sandbox.Sandbox),
		secrets:   make(map[string]string),
		image:     DefaultMockImage,
	}
}

// NewProviderWithImage creates a new mock provider with a specific image.
func NewProviderWithImage(image string) *Provider {
	return &Provider{
		sandboxes: make(map[string]*sandbox.Sandbox),
		secrets:   make(map[string]string),
		image:     image,
	}
}

// ImageExists always returns true for mock provider (no pulling needed).
func (p *Provider) ImageExists(_ context.Context) bool {
	return true
}

// Image returns the configured sandbox image name.
func (p *Provider) Image() string {
	return p.image
}

// Create creates a mock sandbox.
func (p *Provider) Create(ctx context.Context, sessionID string, opts sandbox.CreateOptions) (*sandbox.Sandbox, error) {
	if p.CreateFunc != nil {
		return p.CreateFunc(ctx, sessionID, opts)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.sandboxes[sessionID]; exists {
		return nil, sandbox.ErrAlreadyExists
	}

	// Store the secret
	if opts.SharedSecret != "" {
		p.secrets[sessionID] = opts.SharedSecret
	}

	// Always simulate port 3002 assignment (deterministic for testing)
	ports := []sandbox.AssignedPort{
		{
			ContainerPort: 3002,
			HostPort:      40888, // Predictable for testing
			HostIP:        "0.0.0.0",
			Protocol:      "tcp",
		},
	}

	now := time.Now()
	s := &sandbox.Sandbox{
		ID:        "mock-" + sessionID,
		SessionID: sessionID,
		Status:    sandbox.StatusCreated,
		Image:     p.image,
		CreatedAt: now,
		Metadata:  map[string]string{"mock": "true"},
		Ports:     ports,
	}
	p.sandboxes[sessionID] = s

	// Emit state event
	p.emitEvent(sandbox.StateEvent{
		SessionID: sessionID,
		Status:    sandbox.StatusCreated,
		Timestamp: now,
	})

	return s, nil
}

// Start starts a mock sandbox.
func (p *Provider) Start(ctx context.Context, sessionID string) error {
	if p.StartFunc != nil {
		return p.StartFunc(ctx, sessionID)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	s, exists := p.sandboxes[sessionID]
	if !exists {
		return sandbox.ErrNotFound
	}

	if s.Status == sandbox.StatusRunning {
		return sandbox.ErrAlreadyRunning
	}

	s.Status = sandbox.StatusRunning
	now := time.Now()
	s.StartedAt = &now

	// Emit state event
	p.emitEvent(sandbox.StateEvent{
		SessionID: sessionID,
		Status:    sandbox.StatusRunning,
		Timestamp: now,
	})

	return nil
}

// Stop stops a mock sandbox.
func (p *Provider) Stop(ctx context.Context, sessionID string, timeout time.Duration) error {
	if p.StopFunc != nil {
		return p.StopFunc(ctx, sessionID, timeout)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	s, exists := p.sandboxes[sessionID]
	if !exists {
		return sandbox.ErrNotFound
	}

	if s.Status != sandbox.StatusRunning {
		return sandbox.ErrNotRunning
	}

	s.Status = sandbox.StatusStopped
	now := time.Now()
	s.StoppedAt = &now

	// Emit state event
	p.emitEvent(sandbox.StateEvent{
		SessionID: sessionID,
		Status:    sandbox.StatusStopped,
		Timestamp: now,
	})

	return nil
}

// Remove removes a mock sandbox and optionally its associated data.
// By default, secrets are preserved (simulates Docker volume preservation).
// Pass sandbox.RemoveVolumes() to delete secrets (simulates complete cleanup).
func (p *Provider) Remove(ctx context.Context, sessionID string, opts ...sandbox.RemoveOption) error {
	if p.RemoveFunc != nil {
		return p.RemoveFunc(ctx, sessionID, opts...)
	}

	cfg := sandbox.ParseRemoveOptions(opts)

	p.mu.Lock()
	defer p.mu.Unlock()

	if _, exists := p.sandboxes[sessionID]; !exists {
		return nil // Idempotent
	}

	delete(p.sandboxes, sessionID)

	// Clean up secrets if removeVolumes is true
	if cfg.RemoveVolumes {
		delete(p.secrets, sessionID)
	}

	// Emit state event
	p.emitEvent(sandbox.StateEvent{
		SessionID: sessionID,
		Status:    sandbox.StatusRemoved,
		Timestamp: time.Now(),
	})

	return nil
}

// Get returns a mock sandbox.
func (p *Provider) Get(ctx context.Context, sessionID string) (*sandbox.Sandbox, error) {
	if p.GetFunc != nil {
		return p.GetFunc(ctx, sessionID)
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	s, exists := p.sandboxes[sessionID]
	if !exists {
		return nil, sandbox.ErrNotFound
	}

	// Return a copy
	cpy := *s
	return &cpy, nil
}

// GetSecret returns the raw shared secret for the sandbox.
func (p *Provider) GetSecret(ctx context.Context, sessionID string) (string, error) {
	if p.GetSecretFunc != nil {
		return p.GetSecretFunc(ctx, sessionID)
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	if _, exists := p.sandboxes[sessionID]; !exists {
		return "", sandbox.ErrNotFound
	}

	secret, exists := p.secrets[sessionID]
	if !exists || secret == "" {
		return "", fmt.Errorf("shared secret not found for sandbox")
	}

	return secret, nil
}

// Exec runs a mock command.
func (p *Provider) Exec(ctx context.Context, sessionID string, cmd []string, opts sandbox.ExecOptions) (*sandbox.ExecResult, error) {
	if p.ExecFunc != nil {
		return p.ExecFunc(ctx, sessionID, cmd, opts)
	}

	p.mu.RLock()
	_, exists := p.sandboxes[sessionID]
	p.mu.RUnlock()

	if !exists {
		return nil, sandbox.ErrNotFound
	}

	return &sandbox.ExecResult{
		ExitCode: 0,
		Stdout:   []byte("mock output\n"),
		Stderr:   []byte{},
	}, nil
}

// Attach creates a mock PTY.
func (p *Provider) Attach(ctx context.Context, sessionID string, opts sandbox.AttachOptions) (sandbox.PTY, error) {
	if p.AttachFunc != nil {
		return p.AttachFunc(ctx, sessionID, opts)
	}

	p.mu.RLock()
	s, exists := p.sandboxes[sessionID]
	p.mu.RUnlock()

	if !exists {
		return nil, sandbox.ErrNotFound
	}

	if s.Status != sandbox.StatusRunning {
		return nil, sandbox.ErrNotRunning
	}

	return &PTY{}, nil
}

// ExecStream creates a mock stream for bidirectional I/O.
func (p *Provider) ExecStream(ctx context.Context, sessionID string, cmd []string, opts sandbox.ExecStreamOptions) (sandbox.Stream, error) {
	if p.ExecStreamFunc != nil {
		return p.ExecStreamFunc(ctx, sessionID, cmd, opts)
	}

	p.mu.RLock()
	s, exists := p.sandboxes[sessionID]
	p.mu.RUnlock()

	if !exists {
		return nil, sandbox.ErrNotFound
	}

	if s.Status != sandbox.StatusRunning {
		return nil, sandbox.ErrNotRunning
	}

	return &Stream{}, nil
}

// List returns all sandboxes managed by this mock provider.
func (p *Provider) List(_ context.Context) ([]*sandbox.Sandbox, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make([]*sandbox.Sandbox, 0, len(p.sandboxes))
	for _, v := range p.sandboxes {
		cpy := *v
		result = append(result, &cpy)
	}
	return result, nil
}

// HTTPClient returns an HTTP client configured to communicate with the sandbox.
// For mock provider, this returns a client that uses the configured HTTPHandler
// without making real network connections.
func (p *Provider) HTTPClient(_ context.Context, sessionID string) (*http.Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	s, exists := p.sandboxes[sessionID]
	if !exists {
		return nil, sandbox.ErrNotFound
	}

	if s.Status != sandbox.StatusRunning {
		return nil, sandbox.ErrNotRunning
	}

	// Use mock transport that calls the handler directly.
	handler := p.HTTPHandler
	if handler == nil {
		if p.defaultHTTPHandler == nil {
			p.defaultHTTPHandler = defaultMockHandler()
		}
		handler = p.defaultHTTPHandler
	}

	return &http.Client{
		Transport: &mockRoundTripper{handler: handler},
	}, nil
}

// GetSandboxes returns all sandboxes (for test assertions).
func (p *Provider) GetSandboxes() map[string]*sandbox.Sandbox {
	p.mu.RLock()
	defer p.mu.RUnlock()

	result := make(map[string]*sandbox.Sandbox)
	for k, v := range p.sandboxes {
		cpy := *v
		result[k] = &cpy
	}
	return result
}

// SetSandboxPort overrides the port mapping for a sandbox to point to a mock server.
// This is useful for testing to redirect sandbox traffic to a test HTTP server.
func (p *Provider) SetSandboxPort(sessionID string, host string, port int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if s, exists := p.sandboxes[sessionID]; exists {
		s.Ports = []sandbox.AssignedPort{
			{
				ContainerPort: 3002,
				HostPort:      port,
				HostIP:        host,
				Protocol:      "tcp",
			},
		}
	}
}

// PTY is a mock PTY for testing.
type PTY struct {
	InputBuffer  []byte
	OutputBuffer []byte
	Closed       bool
	ResizeCalls  []struct{ Rows, Cols int }
	mu           sync.Mutex
}

func (p *PTY) Read(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.Closed {
		return 0, io.EOF
	}

	if len(p.OutputBuffer) == 0 {
		// Simulate some output
		p.OutputBuffer = []byte("$ ")
	}

	n := copy(b, p.OutputBuffer)
	p.OutputBuffer = p.OutputBuffer[n:]
	return n, nil
}

func (p *PTY) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.Closed {
		return 0, io.ErrClosedPipe
	}

	p.InputBuffer = append(p.InputBuffer, b...)
	// Echo input to output
	p.OutputBuffer = append(p.OutputBuffer, b...)
	return len(b), nil
}

func (p *PTY) Resize(_ context.Context, rows, cols int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.ResizeCalls = append(p.ResizeCalls, struct{ Rows, Cols int }{rows, cols})
	return nil
}

// GetResizeCalls returns a copy of the resize calls for test assertions.
func (p *PTY) GetResizeCalls() []struct{ Rows, Cols int } {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]struct{ Rows, Cols int }{}, p.ResizeCalls...)
}

func (p *PTY) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.Closed = true
	return nil
}

func (p *PTY) Wait(_ context.Context) (int, error) {
	return 0, nil
}

// Stream is a mock Stream for testing.
type Stream struct {
	InputBuffer  []byte
	OutputBuffer []byte
	StderrBuffer []byte
	Closed       bool
	WritesClosed bool
	ResizeCalls  []struct{ Rows, Cols int }
	mu           sync.Mutex
}

func (s *Stream) Read(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Closed {
		return 0, io.EOF
	}

	if len(s.OutputBuffer) == 0 {
		return 0, io.EOF
	}

	n := copy(b, s.OutputBuffer)
	s.OutputBuffer = s.OutputBuffer[n:]
	return n, nil
}

func (s *Stream) Stderr() io.Reader {
	return &stderrReader{stream: s}
}

// stderrReader provides a reader for the mock stream's stderr buffer.
type stderrReader struct {
	stream *Stream
}

func (r *stderrReader) Read(b []byte) (int, error) {
	r.stream.mu.Lock()
	defer r.stream.mu.Unlock()

	if r.stream.Closed {
		return 0, io.EOF
	}

	if len(r.stream.StderrBuffer) == 0 {
		return 0, io.EOF
	}

	n := copy(b, r.stream.StderrBuffer)
	r.stream.StderrBuffer = r.stream.StderrBuffer[n:]
	return n, nil
}

func (s *Stream) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Closed || s.WritesClosed {
		return 0, io.ErrClosedPipe
	}

	s.InputBuffer = append(s.InputBuffer, b...)
	return len(b), nil
}

func (s *Stream) Resize(_ context.Context, rows, cols int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ResizeCalls = append(s.ResizeCalls, struct{ Rows, Cols int }{rows, cols})
	return nil
}

func (s *Stream) CloseWrite() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.WritesClosed = true
	return nil
}

func (s *Stream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Closed = true
	return nil
}

func (s *Stream) Wait(_ context.Context) (int, error) {
	return 0, nil
}

// Watch returns a channel that receives sandbox state change events.
// For the mock provider, this replays current state and then streams events
// as sandbox state changes occur through the Create/Start/Stop/Remove methods.
func (p *Provider) Watch(ctx context.Context) (<-chan sandbox.StateEvent, error) {
	if p.WatchFunc != nil {
		return p.WatchFunc(ctx)
	}

	eventCh := make(chan sandbox.StateEvent, 100)
	done := make(chan struct{})

	sub := &eventSubscriber{
		ch:   eventCh,
		done: done,
	}

	// Register subscriber
	p.subscribersMu.Lock()
	p.subscribers = append(p.subscribers, sub)
	p.subscribersMu.Unlock()

	// Start goroutine to handle replay and context cancellation
	go func() {
		defer func() {
			// Unregister subscriber on exit
			p.subscribersMu.Lock()
			for i, s := range p.subscribers {
				if s == sub {
					p.subscribers = append(p.subscribers[:i], p.subscribers[i+1:]...)
					break
				}
			}
			p.subscribersMu.Unlock()
			close(eventCh)
		}()

		// Replay current state
		p.mu.RLock()
		sandboxes := make([]*sandbox.Sandbox, 0, len(p.sandboxes))
		for _, sb := range p.sandboxes {
			cpy := *sb
			sandboxes = append(sandboxes, &cpy)
		}
		p.mu.RUnlock()

		for _, sb := range sandboxes {
			select {
			case <-ctx.Done():
				return
			case eventCh <- sandbox.StateEvent{
				SessionID: sb.SessionID,
				Status:    sb.Status,
				Timestamp: time.Now(),
				Error:     sb.Error,
			}:
			}
		}

		// Wait for context cancellation or done signal
		select {
		case <-ctx.Done():
		case <-done:
		}
	}()

	return eventCh, nil
}

// emitEvent sends an event to all subscribers.
func (p *Provider) emitEvent(event sandbox.StateEvent) {
	p.subscribersMu.RLock()
	defer p.subscribersMu.RUnlock()

	for _, sub := range p.subscribers {
		select {
		case sub.ch <- event:
		default:
			// Channel full, skip (non-blocking)
		}
	}
}

// EmitEvent is a test helper to manually emit an event to all watchers.
// This is useful for testing the Watch functionality.
func (p *Provider) EmitEvent(event sandbox.StateEvent) {
	p.emitEvent(event)
}

// Reconcile is a no-op for the mock provider.
func (p *Provider) Reconcile(_ context.Context) error {
	return nil
}

// RemoveProject is a no-op for the mock provider.
func (p *Provider) RemoveProject(_ context.Context, _ string) error {
	return nil
}

// CloseWatchers closes all active Watch channels.
// This is useful for testing cleanup.
func (p *Provider) CloseWatchers() {
	p.subscribersMu.Lock()
	defer p.subscribersMu.Unlock()

	for _, sub := range p.subscribers {
		close(sub.done)
	}
	p.subscribers = nil
}

// mockRoundTripper implements http.RoundTripper using an http.Handler.
// This allows HTTPClient to work without real network connections.
type mockRoundTripper struct {
	handler http.Handler
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Create a pipe to capture the response
	pr, pw := io.Pipe()

	// Channel to signal when headers are ready
	headerReady := make(chan struct{})

	// Create a ResponseRecorder-like structure
	rec := &pipeResponseWriter{
		header:      make(http.Header),
		statusCode:  http.StatusOK,
		pipe:        pw,
		headerReady: headerReady,
	}

	// Call the handler in a goroutine since it may write streaming data
	go func() {
		defer pw.Close()
		m.handler.ServeHTTP(rec, req)
		// Ensure headerReady is closed even if handler didn't write anything
		rec.ensureHeaderReady()
	}()

	// Wait for headers to be ready before returning response
	<-headerReady

	return &http.Response{
		StatusCode: rec.statusCode,
		Header:     rec.header,
		Body:       pr,
		Request:    req,
	}, nil
}

// pipeResponseWriter implements http.ResponseWriter writing to a pipe.
type pipeResponseWriter struct {
	header      http.Header
	statusCode  int
	pipe        *io.PipeWriter
	wroteHeader bool
	headerReady chan struct{}
}

func (w *pipeResponseWriter) Header() http.Header {
	return w.header
}

func (w *pipeResponseWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.statusCode = code
		w.wroteHeader = true
		close(w.headerReady)
	}
}

func (w *pipeResponseWriter) Write(b []byte) (int, error) {
	w.ensureHeaderReady()
	return w.pipe.Write(b)
}

func (w *pipeResponseWriter) ensureHeaderReady() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
}

// defaultMockHandler returns a handler that responds like agent-go.
// POST /threads/{id}/chat returns 202 Accepted.
// GET /threads/{id}/chat/status returns {"isRunning":false}.
// GET /threads/{id}/chat/stream returns 200 with an explicit SSE done event.
// GET /threads/{id}/messages returns empty message list.
// Also supports thread CRUD, file, and diff endpoints for testing.
func defaultMockHandler() http.Handler {
	var (
		mu      sync.Mutex
		threads = map[string]string{}
	)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/chat") && r.Method == "POST":
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "started", "completionId": "mock-completion"})
			return

		case strings.HasSuffix(r.URL.Path, "/chat/status") && r.Method == "GET":
			_, _ = w.Write([]byte(`{"isRunning":false}`))
			return

		case strings.HasSuffix(r.URL.Path, "/chat/stream") && r.Method == "GET":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, "event: done\n")
			_, _ = fmt.Fprintf(w, "data: {}\n\n")
			return

		case strings.HasSuffix(r.URL.Path, "/messages") && r.Method == "GET":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"messages":[]}`))
			return

		case strings.HasSuffix(r.URL.Path, "/chat") && r.Method == "GET":
			// Legacy: return empty messages
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"messages":[]}`))
			return

		case r.URL.Path == "/threads" && r.Method == "GET":
			mu.Lock()
			ids := make([]string, 0, len(threads))
			for id := range threads {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			result := make([]map[string]string, 0, len(ids))
			for _, id := range ids {
				result = append(result, map[string]string{"id": id, "name": threads[id]})
			}
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"threads": result})
			return

		case r.URL.Path == "/threads" && r.Method == "POST":
			var req struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid request body"}`))
				return
			}
			if strings.TrimSpace(req.ID) == "" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"id is required"}`))
				return
			}
			if strings.TrimSpace(req.Name) == "" {
				req.Name = req.ID
			}

			mu.Lock()
			if _, exists := threads[req.ID]; exists {
				mu.Unlock()
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"error":"thread already exists"}`))
				return
			}
			threads[req.ID] = req.Name
			mu.Unlock()

			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"id": req.ID, "name": req.Name})
			return

		case strings.HasPrefix(r.URL.Path, "/threads/") && !strings.Contains(strings.TrimPrefix(r.URL.Path, "/threads/"), "/"):
			threadID := strings.TrimPrefix(r.URL.Path, "/threads/")
			if threadID == "" {
				http.NotFound(w, r)
				return
			}

			switch r.Method {
			case "GET":
				mu.Lock()
				name, ok := threads[threadID]
				mu.Unlock()
				if !ok {
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte(`{"error":"thread not found"}`))
					return
				}
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]string{"id": threadID, "name": name})
				return
			case "PUT", "PATCH":
				var req struct {
					Name string `json:"name"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":"invalid request body"}`))
					return
				}
				if strings.TrimSpace(req.Name) == "" {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":"name is required"}`))
					return
				}

				mu.Lock()
				if _, ok := threads[threadID]; !ok {
					mu.Unlock()
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte(`{"error":"thread not found"}`))
					return
				}
				threads[threadID] = req.Name
				mu.Unlock()

				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]string{"id": threadID, "name": req.Name})
				return
			case "DELETE":
				mu.Lock()
				if _, ok := threads[threadID]; !ok {
					mu.Unlock()
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte(`{"error":"thread not found"}`))
					return
				}
				delete(threads, threadID)
				mu.Unlock()

				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
				return
			}

		case r.URL.Path == "/files" && r.Method == "GET":
			// List files - return mock directory listing
			path := r.URL.Query().Get("path")
			if path == "" {
				path = "."
			}
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, `{"path":"%s","entries":[{"name":"README.md","type":"file","size":100},{"name":"src","type":"directory"}]}`, path)
			return

		case r.URL.Path == "/files/read" && r.Method == "GET":
			// Read file - return mock content
			path := r.URL.Query().Get("path")
			if path == "" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"path query parameter required"}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintf(w, `{"path":"%s","content":"# Mock Content","encoding":"utf8","size":14}`, path)
			return

		case r.URL.Path == "/files/write" && r.Method == "POST":
			// Write file - return success
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"path":"test.txt","size":100}`))
			return

		case r.URL.Path == "/diff" && r.Method == "GET":
			// Diff - return empty diff
			format := r.URL.Query().Get("format")
			if format == "files" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"files":[],"stats":{"filesChanged":0,"additions":0,"deletions":0}}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"files":[],"stats":{"filesChanged":0,"additions":0,"deletions":0}}`))
			return
		}

		http.NotFound(w, r)
	})
}
