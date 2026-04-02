// Package local provides a local directory-based implementation of the sandbox.Provider interface.
// Instead of running in containers, this provider runs the agent API directly in the workspace directory.
package local

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/sandbox"
)

// Provider implements the sandbox.Provider interface using local processes.
type Provider struct {
	cfg        *config.Config
	binaryPath string // Path to agent API binary

	// processes maps sessionID -> process info
	processes   map[string]*processInfo
	processesMu sync.RWMutex

	// eventCh broadcasts state change events
	eventCh   chan sandbox.StateEvent
	eventSubs []chan sandbox.StateEvent
	eventMu   sync.RWMutex
}

// processInfo stores information about a running agent API process.
type processInfo struct {
	cmd           *exec.Cmd
	port          int
	workspacePath string
	secret        string
	status        sandbox.Status
	createdAt     time.Time
	startedAt     *time.Time
	stoppedAt     *time.Time
	error         string
	metadata      map[string]string
	env           map[string]string
}

// NewProvider creates a new local sandbox provider.
func NewProvider(cfg *config.Config) (*Provider, error) {
	// Use configured binary path, default to "obot-agent-api" in PATH
	binaryPath := cfg.LocalAgentBinary
	if binaryPath == "" {
		binaryPath = "obot-agent-api"
	}

	// Verify that the agent API binary exists
	resolvedPath, err := exec.LookPath(binaryPath)
	if err != nil {
		return nil, fmt.Errorf("agent API binary not found: %w (looking for: %s)", err, binaryPath)
	}

	log.Printf("Local provider using agent API binary: %s", resolvedPath)

	p := &Provider{
		cfg:        cfg,
		binaryPath: resolvedPath,
		processes:  make(map[string]*processInfo),
		eventCh:    make(chan sandbox.StateEvent, 100),
	}

	return p, nil
}

// ImageExists always returns true for local provider (no image needed).
func (p *Provider) ImageExists(_ context.Context) bool {
	return true
}

// Image returns "local" as the image name.
func (p *Provider) Image() string {
	return "local"
}

// Create creates a new sandbox for the given session by preparing the process info.
// The process is not started yet.
func (p *Provider) Create(_ context.Context, sessionID string, opts sandbox.CreateOptions) (*sandbox.Sandbox, error) {
	p.processesMu.Lock()
	defer p.processesMu.Unlock()

	// Check if already exists
	if _, exists := p.processes[sessionID]; exists {
		return nil, sandbox.ErrAlreadyExists
	}

	// Validate workspace path
	if opts.WorkspacePath == "" {
		return nil, fmt.Errorf("%w: workspace path is required", sandbox.ErrStartFailed)
	}

	// Ensure workspace path is absolute
	workspacePath := opts.WorkspacePath
	if !filepath.IsAbs(workspacePath) {
		absPath, err := filepath.Abs(workspacePath)
		if err != nil {
			return nil, fmt.Errorf("%w: failed to resolve absolute path for workspace: %v", sandbox.ErrStartFailed, err)
		}
		workspacePath = absPath
	}

	// Verify workspace exists
	if stat, err := os.Stat(workspacePath); err != nil {
		return nil, fmt.Errorf("%w: workspace path does not exist: %v", sandbox.ErrStartFailed, err)
	} else if !stat.IsDir() {
		return nil, fmt.Errorf("%w: workspace path is not a directory", sandbox.ErrStartFailed)
	}

	// Build metadata
	metadata := map[string]string{
		"session_id": sessionID,
		"managed":    "true",
	}
	for k, v := range opts.Labels {
		metadata[k] = v
	}

	// Build environment variables
	env := map[string]string{
		"DISCOBOT_SESSION_ID":   sessionID,
		"WORKSPACE_ORIGIN_PATH": workspacePath,
	}

	// Add hashed secret if provided
	if opts.SharedSecret != "" {
		hashedSecret := hashSecret(opts.SharedSecret)
		env["DISCOBOT_SECRET"] = hashedSecret
	}

	// Add workspace source and commit
	if opts.WorkspaceSource != "" {
		env["WORKSPACE_SOURCE"] = opts.WorkspaceSource
	}
	if opts.WorkspaceCommit != "" {
		env["WORKSPACE_COMMIT"] = opts.WorkspaceCommit
	}

	// MCP OAuth env vars (agent uses these to persist tokens back to the server)
	if opts.ProjectID != "" {
		env["DISCOBOT_PROJECT_ID"] = opts.ProjectID
	}
	if opts.MCPOAuthRedirectBase != "" {
		env["DISCOBOT_MCP_OAUTH_REDIRECT_BASE"] = opts.MCPOAuthRedirectBase
	}
	if opts.AgentServerURL != "" {
		env["DISCOBOT_SERVER_URL"] = opts.AgentServerURL
	}

	// Create process info (not started yet)
	now := time.Now()
	info := &processInfo{
		port:          0, // Will be assigned on start
		workspacePath: workspacePath,
		secret:        opts.SharedSecret,
		status:        sandbox.StatusCreated,
		createdAt:     now,
		metadata:      metadata,
		env:           env,
	}

	p.processes[sessionID] = info

	// Broadcast creation event
	p.broadcastEvent(sandbox.StateEvent{
		SessionID: sessionID,
		Status:    sandbox.StatusCreated,
		Timestamp: now,
	})

	return &sandbox.Sandbox{
		ID:        sessionID,
		SessionID: sessionID,
		Status:    sandbox.StatusCreated,
		Image:     "local",
		CreatedAt: now,
		Metadata:  metadata,
		Env:       env,
	}, nil
}

// Start starts the agent API process for the given session.
func (p *Provider) Start(_ context.Context, sessionID string) error {
	p.processesMu.Lock()
	defer p.processesMu.Unlock()

	info, exists := p.processes[sessionID]
	if !exists {
		return sandbox.ErrNotFound
	}

	// Check if already running
	if info.status == sandbox.StatusRunning {
		return nil // Already running
	}

	// Allocate a random port by binding to port 0
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		info.status = sandbox.StatusFailed
		info.error = fmt.Sprintf("failed to allocate port: %v", err)
		p.broadcastEvent(sandbox.StateEvent{
			SessionID: sessionID,
			Status:    sandbox.StatusFailed,
			Timestamp: time.Now(),
			Error:     info.error,
		})
		return fmt.Errorf("%w: failed to allocate port: %v", sandbox.ErrStartFailed, err)
	}

	// Get the assigned port
	addr := listener.Addr().(*net.TCPAddr)
	port := addr.Port
	listener.Close() // Close the listener so the agent API can bind to it

	log.Printf("Allocated port %d for session %s", port, sessionID)

	// Build command using configured binary path
	cmd := exec.Command(p.binaryPath)
	cmd.Dir = info.workspacePath

	// Set environment variables
	cmd.Env = os.Environ() // Start with current environment
	cmd.Env = append(cmd.Env, fmt.Sprintf("DISCOBOT_PORT=%d", port))
	for k, v := range info.env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		info.status = sandbox.StatusFailed
		info.error = fmt.Sprintf("failed to start agent API: %v", err)
		p.broadcastEvent(sandbox.StateEvent{
			SessionID: sessionID,
			Status:    sandbox.StatusFailed,
			Timestamp: time.Now(),
			Error:     info.error,
		})
		return fmt.Errorf("%w: failed to start agent API: %v", sandbox.ErrStartFailed, err)
	}

	// Update process info
	now := time.Now()
	info.cmd = cmd
	info.port = port
	info.status = sandbox.StatusRunning
	info.startedAt = &now

	// Monitor process in background
	go p.monitorProcess(sessionID, cmd)

	// Broadcast running event
	p.broadcastEvent(sandbox.StateEvent{
		SessionID: sessionID,
		Status:    sandbox.StatusRunning,
		Timestamp: now,
	})

	log.Printf("Started agent API for session %s on port %d (PID %d)", sessionID, port, cmd.Process.Pid)

	return nil
}

// monitorProcess monitors a process and updates status when it exits.
func (p *Provider) monitorProcess(sessionID string, cmd *exec.Cmd) {
	err := cmd.Wait()

	p.processesMu.Lock()
	defer p.processesMu.Unlock()

	info, exists := p.processes[sessionID]
	if !exists {
		return
	}

	now := time.Now()
	info.stoppedAt = &now

	if err != nil {
		info.status = sandbox.StatusFailed
		info.error = fmt.Sprintf("process exited with error: %v", err)
		p.broadcastEvent(sandbox.StateEvent{
			SessionID: sessionID,
			Status:    sandbox.StatusFailed,
			Timestamp: now,
			Error:     info.error,
		})
		log.Printf("Agent API process for session %s exited with error: %v", sessionID, err)
	} else {
		info.status = sandbox.StatusStopped
		p.broadcastEvent(sandbox.StateEvent{
			SessionID: sessionID,
			Status:    sandbox.StatusStopped,
			Timestamp: now,
		})
		log.Printf("Agent API process for session %s stopped", sessionID)
	}
}

// Stop stops the agent API process gracefully.
func (p *Provider) Stop(_ context.Context, sessionID string, timeout time.Duration) error {
	p.processesMu.Lock()
	defer p.processesMu.Unlock()

	info, exists := p.processes[sessionID]
	if !exists {
		return sandbox.ErrNotFound
	}

	if info.status != sandbox.StatusRunning || info.cmd == nil {
		return nil // Already stopped
	}

	// Send SIGTERM for graceful shutdown
	if err := info.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		log.Printf("Failed to send SIGTERM to session %s: %v", sessionID, err)
	}

	// Wait for process to exit with timeout
	done := make(chan error, 1)
	go func() {
		done <- info.cmd.Wait()
	}()

	select {
	case <-done:
		// Process exited gracefully
		now := time.Now()
		info.status = sandbox.StatusStopped
		info.stoppedAt = &now
		p.broadcastEvent(sandbox.StateEvent{
			SessionID: sessionID,
			Status:    sandbox.StatusStopped,
			Timestamp: now,
		})
		log.Printf("Stopped agent API for session %s", sessionID)
	case <-time.After(timeout):
		// Timeout - force kill
		if err := info.cmd.Process.Kill(); err != nil {
			log.Printf("Failed to kill process for session %s: %v", sessionID, err)
		}
		now := time.Now()
		info.status = sandbox.StatusStopped
		info.stoppedAt = &now
		p.broadcastEvent(sandbox.StateEvent{
			SessionID: sessionID,
			Status:    sandbox.StatusStopped,
			Timestamp: now,
		})
		log.Printf("Force killed agent API for session %s after timeout", sessionID)
	}

	return nil
}

// Remove removes the sandbox (stops the process if running).
func (p *Provider) Remove(ctx context.Context, sessionID string, _ ...sandbox.RemoveOption) error {
	// Stop the process first if running
	if err := p.Stop(ctx, sessionID, 5*time.Second); err != nil && err != sandbox.ErrNotFound {
		return err
	}

	p.processesMu.Lock()
	defer p.processesMu.Unlock()

	if _, exists := p.processes[sessionID]; !exists {
		return sandbox.ErrNotFound
	}

	// Remove from map
	delete(p.processes, sessionID)

	// Broadcast removed event
	p.broadcastEvent(sandbox.StateEvent{
		SessionID: sessionID,
		Status:    sandbox.StatusRemoved,
		Timestamp: time.Now(),
	})

	log.Printf("Removed sandbox for session %s", sessionID)

	return nil
}

// Get returns the current state of a sandbox.
func (p *Provider) Get(_ context.Context, sessionID string) (*sandbox.Sandbox, error) {
	p.processesMu.RLock()
	defer p.processesMu.RUnlock()

	info, exists := p.processes[sessionID]
	if !exists {
		return nil, sandbox.ErrNotFound
	}

	ports := []sandbox.AssignedPort{}
	if info.port > 0 {
		ports = append(ports, sandbox.AssignedPort{
			ContainerPort: info.port,
			HostPort:      info.port,
			HostIP:        "127.0.0.1",
			Protocol:      "tcp",
		})
	}

	return &sandbox.Sandbox{
		ID:        sessionID,
		SessionID: sessionID,
		Status:    info.status,
		Image:     "local",
		CreatedAt: info.createdAt,
		StartedAt: info.startedAt,
		StoppedAt: info.stoppedAt,
		Error:     info.error,
		Metadata:  info.metadata,
		Ports:     ports,
		Env:       info.env,
	}, nil
}

// GetSecret returns the shared secret for the sandbox.
func (p *Provider) GetSecret(_ context.Context, sessionID string) (string, error) {
	p.processesMu.RLock()
	defer p.processesMu.RUnlock()

	info, exists := p.processes[sessionID]
	if !exists {
		return "", sandbox.ErrNotFound
	}

	return info.secret, nil
}

// List returns all sandboxes managed by this provider.
func (p *Provider) List(ctx context.Context) ([]*sandbox.Sandbox, error) {
	p.processesMu.RLock()
	defer p.processesMu.RUnlock()

	var sandboxes []*sandbox.Sandbox
	for sessionID := range p.processes {
		sb, err := p.Get(ctx, sessionID)
		if err == nil {
			sandboxes = append(sandboxes, sb)
		}
	}

	return sandboxes, nil
}

// Exec runs a non-interactive command in the workspace directory.
func (p *Provider) Exec(ctx context.Context, sessionID string, cmd []string, opts sandbox.ExecOptions) (*sandbox.ExecResult, error) {
	p.processesMu.RLock()
	info, exists := p.processes[sessionID]
	p.processesMu.RUnlock()

	if !exists {
		return nil, sandbox.ErrNotFound
	}

	if len(cmd) == 0 {
		return nil, fmt.Errorf("command is required")
	}

	// Create command
	execCmd := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
	execCmd.Dir = info.workspacePath

	// Set working directory if specified
	if opts.WorkDir != "" {
		execCmd.Dir = filepath.Join(info.workspacePath, opts.WorkDir)
	}

	// Set environment
	execCmd.Env = os.Environ()
	for k, v := range info.env {
		execCmd.Env = append(execCmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	for k, v := range opts.Env {
		execCmd.Env = append(execCmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Set stdin if provided
	if opts.Stdin != nil {
		execCmd.Stdin = opts.Stdin
	}

	// Capture stdout and stderr
	var stdout, stderr []byte
	var err error

	stdout, err = execCmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr = exitErr.Stderr
			return &sandbox.ExecResult{
				ExitCode: exitErr.ExitCode(),
				Stdout:   stdout,
				Stderr:   stderr,
			}, nil
		}
		return nil, fmt.Errorf("failed to execute command: %w", err)
	}

	return &sandbox.ExecResult{
		ExitCode: 0,
		Stdout:   stdout,
		Stderr:   stderr,
	}, nil
}

// Attach creates an interactive PTY session (not supported for local provider).
// Returns an error result as if the command failed, not the provider itself.
func (p *Provider) Attach(_ context.Context, sessionID string, _ sandbox.AttachOptions) (sandbox.PTY, error) {
	p.processesMu.RLock()
	_, exists := p.processes[sessionID]
	p.processesMu.RUnlock()

	if !exists {
		return nil, sandbox.ErrNotFound
	}

	// Return a fake PTY that immediately fails with a helpful error message
	return &unsupportedPTY{
		errorMessage: "PTY/terminal access is not supported for local sandbox provider.\n" +
			"Use the Docker provider if you need terminal access.\n" +
			"For local provider, use your system terminal to work in the workspace directory.\n",
	}, nil
}

// ExecStream runs a command with bidirectional streaming (not supported for local provider).
// Returns an error result as if the command failed, not the provider itself.
func (p *Provider) ExecStream(_ context.Context, sessionID string, _ []string, _ sandbox.ExecStreamOptions) (sandbox.Stream, error) {
	p.processesMu.RLock()
	_, exists := p.processes[sessionID]
	p.processesMu.RUnlock()

	if !exists {
		return nil, sandbox.ErrNotFound
	}

	// Return a fake stream that immediately fails with a helpful error message
	return &unsupportedStream{
		errorMessage: "Streaming command execution is not supported for local sandbox provider.\n" +
			"Use the Docker provider if you need streaming features (SFTP, port forwarding, etc.).\n",
	}, nil
}

// HTTPClient returns an HTTP client configured to communicate with the sandbox.
func (p *Provider) HTTPClient(_ context.Context, sessionID string) (*http.Client, error) {
	p.processesMu.RLock()
	info, exists := p.processes[sessionID]
	p.processesMu.RUnlock()

	if !exists {
		return nil, sandbox.ErrNotFound
	}

	if info.port == 0 {
		return nil, fmt.Errorf("sandbox not started yet")
	}

	// Create HTTP client that connects to localhost:port
	client := &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: true,
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", info.port))
			},
		},
		Timeout: 60 * time.Second,
	}

	return client, nil
}

// Watch returns a channel that receives sandbox state change events.
func (p *Provider) Watch(ctx context.Context) (<-chan sandbox.StateEvent, error) {
	p.eventMu.Lock()
	defer p.eventMu.Unlock()

	// Create subscriber channel
	sub := make(chan sandbox.StateEvent, 100)
	p.eventSubs = append(p.eventSubs, sub)

	// Send current state of all sandboxes
	go func() {
		p.processesMu.RLock()
		defer p.processesMu.RUnlock()

		for sessionID, info := range p.processes {
			select {
			case sub <- sandbox.StateEvent{
				SessionID: sessionID,
				Status:    info.status,
				Timestamp: time.Now(),
				Error:     info.error,
			}:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Clean up on context cancellation
	go func() {
		<-ctx.Done()
		p.eventMu.Lock()
		defer p.eventMu.Unlock()

		// Remove subscriber
		for i, s := range p.eventSubs {
			if s == sub {
				p.eventSubs = append(p.eventSubs[:i], p.eventSubs[i+1:]...)
				break
			}
		}
		close(sub)
	}()

	return sub, nil
}

// broadcastEvent sends an event to all subscribers.
func (p *Provider) broadcastEvent(event sandbox.StateEvent) {
	p.eventMu.RLock()
	defer p.eventMu.RUnlock()

	for _, sub := range p.eventSubs {
		select {
		case sub <- event:
		default:
			// Skip if channel is full
		}
	}
}

// Reconcile is a no-op for the local provider.
func (p *Provider) Reconcile(_ context.Context) error {
	return nil
}

// RemoveProject is a no-op for the local provider.
func (p *Provider) RemoveProject(_ context.Context, _ string) error {
	return nil
}

// hashSecret creates a salted SHA-256 hash of the secret.
// This matches the Docker provider implementation.
func hashSecret(secret string) string {
	// Generate a random 16-byte salt
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		log.Printf("Failed to generate salt: %v", err)
		return ""
	}

	// Hash the secret with the salt
	hasher := sha256.New()
	hasher.Write(salt)
	hasher.Write([]byte(secret))
	hash := hasher.Sum(nil)

	// Return salt:hash in hex format
	return fmt.Sprintf("%s:%s", hex.EncodeToString(salt), hex.EncodeToString(hash))
}

// unsupportedPTY is a fake PTY that immediately returns an error message.
type unsupportedPTY struct {
	errorMessage string
	closed       bool
	mu           sync.Mutex
}

func (p *unsupportedPTY) Read(buf []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return 0, fmt.Errorf("PTY closed")
	}

	if len(p.errorMessage) == 0 {
		return 0, fmt.Errorf("PTY closed")
	}

	// Return the error message once
	n := copy(buf, []byte(p.errorMessage))
	p.errorMessage = ""
	return n, nil
}

func (p *unsupportedPTY) Write(buf []byte) (int, error) {
	// Silently discard writes
	return len(buf), nil
}

func (p *unsupportedPTY) Resize(_ context.Context, _, _ int) error {
	// Silently ignore resize requests
	return nil
}

func (p *unsupportedPTY) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

func (p *unsupportedPTY) Wait(_ context.Context) (int, error) {
	// Return immediately with exit code 1 (failure)
	return 1, nil
}

// unsupportedStream is a fake stream that immediately returns an error message.
type unsupportedStream struct {
	errorMessage string
	closed       bool
	mu           sync.Mutex
}

func (s *unsupportedStream) Read(_ []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return 0, fmt.Errorf("stream closed")
	}

	// Return empty - all output goes to stderr
	return 0, fmt.Errorf("stream closed")
}

func (s *unsupportedStream) Stderr() io.Reader {
	// Return the error message via stderr
	return &errorReader{message: s.errorMessage}
}

func (s *unsupportedStream) Write(buf []byte) (int, error) {
	// Silently discard writes
	return len(buf), nil
}

func (s *unsupportedStream) Resize(_ context.Context, _, _ int) error {
	return nil
}

func (s *unsupportedStream) CloseWrite() error {
	return nil
}

func (s *unsupportedStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *unsupportedStream) Wait(_ context.Context) (int, error) {
	// Return immediately with exit code 1 (failure)
	return 1, nil
}

// errorReader is a simple reader that returns an error message once.
type errorReader struct {
	message string
	read    bool
	mu      sync.Mutex
}

func (r *errorReader) Read(buf []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.read {
		return 0, fmt.Errorf("EOF")
	}

	n := copy(buf, []byte(r.message))
	r.read = true
	return n, nil
}
