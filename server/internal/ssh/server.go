// Package ssh provides an SSH server that routes connections to sandbox containers.
// It uses the username as the session ID to identify which container to connect to.
// This enables VS Code Remote SSH to connect to sandbox sessions.
package ssh

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"golang.org/x/crypto/ssh"

	"github.com/obot-platform/discobot/server/internal/sandbox"
)

// UserInfoFetcher fetches user info from a sandbox.
// This is used to determine which user to run commands as.
type UserInfoFetcher interface {
	// GetUserInfo returns the default user for a sandbox.
	// Returns username, uid, gid, and any error.
	GetUserInfo(ctx context.Context, sessionID string) (username string, uid, gid int, err error)
}

// EnvVarFetcher fetches environment variables for a session from runtime-managed
// sources like visible credentials.
type EnvVarFetcher interface {
	// GetEnvVarsForSession returns the merged environment variables to inject into
	// SSH sessions before applying client-provided env overrides.
	GetEnvVarsForSession(ctx context.Context, sessionID string) (map[string]string, error)
}

// ConnectionTracker tracks active connections per session.
// Implementations must be safe for concurrent use.
type ConnectionTracker interface {
	// Track registers an active connection for sessionID and returns a release
	// function that must be called when the connection ends.
	Track(sessionID string) func()
}

// Config holds SSH server configuration.
type Config struct {
	// Address to listen on (e.g., ":2222")
	Address string

	// HostKeyPath is the path to the SSH host key file.
	// If the file doesn't exist, a new key will be generated.
	HostKeyPath string

	// SandboxProvider is used to route connections to containers.
	SandboxProvider sandbox.Provider

	// UserInfoFetcher is used to get the default user for sandbox sessions.
	// If nil, commands run as root.
	UserInfoFetcher UserInfoFetcher

	// EnvVarFetcher is used to get environment variables from the session. If nil,
	// no runtime-managed env vars are applied.
	EnvVarFetcher EnvVarFetcher

	// ConnectionTracker is notified when SSH connections are established and closed.
	// If nil, connection tracking is disabled.
	ConnectionTracker ConnectionTracker
}

// Server is an SSH server that routes connections to sandbox containers.
type Server struct {
	config            *ssh.ServerConfig
	provider          sandbox.Provider
	userInfoFetcher   UserInfoFetcher
	envVarFetcher     EnvVarFetcher
	connectionTracker ConnectionTracker
	listener          net.Listener
	addr              string

	mu       sync.Mutex
	sessions map[string]*sessionHandler // sessionID -> handler
	closed   bool
}

// New creates a new SSH server with the given configuration.
func New(cfg *Config) (*Server, error) {
	if cfg.SandboxProvider == nil {
		return nil, errors.New("sandbox provider is required")
	}

	// Load or generate host key
	hostKey, err := loadOrGenerateHostKey(cfg.HostKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load host key: %w", err)
	}

	// Configure SSH server
	sshConfig := &ssh.ServerConfig{
		// No authentication required - username is the session ID
		NoClientAuth: true,

		// Optional: Log auth attempts
		AuthLogCallback: func(conn ssh.ConnMetadata, method string, err error) {
			if err != nil {
				log.Printf("SSH auth failed for %s@%s: method=%s err=%v",
					conn.User(), conn.RemoteAddr(), method, err)
			}
		},
	}
	sshConfig.AddHostKey(hostKey)

	return &Server{
		config:            sshConfig,
		provider:          cfg.SandboxProvider,
		userInfoFetcher:   cfg.UserInfoFetcher,
		envVarFetcher:     cfg.EnvVarFetcher,
		connectionTracker: cfg.ConnectionTracker,
		addr:              cfg.Address,
		sessions:          make(map[string]*sessionHandler),
	}, nil
}

// Start begins accepting SSH connections.
func (s *Server) Start() error {
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.addr, err)
	}

	s.mu.Lock()
	s.listener = listener
	s.mu.Unlock()

	log.Printf("SSH server listening on %s", s.addr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return nil
			}
			log.Printf("SSH accept error: %v", err)
			continue
		}

		go s.handleConnection(conn)
	}
}

// Stop gracefully shuts down the SSH server.
func (s *Server) Stop() error {
	s.mu.Lock()
	s.closed = true
	listener := s.listener
	s.mu.Unlock()

	if listener != nil {
		return listener.Close()
	}
	return nil
}

// Addr returns the address the server is listening on.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.addr
}

func (s *Server) handleConnection(netConn net.Conn) {
	// Perform SSH handshake
	sshConn, chans, reqs, err := ssh.NewServerConn(netConn, s.config)
	if err != nil {
		log.Printf("SSH handshake failed: %v", err)
		netConn.Close()
		return
	}

	// Username is the session ID
	sessionID := sshConn.User()
	log.Printf("SSH connection from %s for session %s", sshConn.RemoteAddr(), sessionID)

	// Verify sandbox exists and is running
	ctx := context.Background()
	sb, err := s.provider.Get(ctx, sessionID)
	if err != nil {
		log.Printf("SSH session %s: sandbox not found: %v", sessionID, err)
		sshConn.Close()
		return
	}
	if sb.Status != sandbox.StatusRunning {
		log.Printf("SSH session %s: sandbox not running (status=%s)", sessionID, sb.Status)
		sshConn.Close()
		return
	}

	// Create session handler
	handler := newSessionHandler(sessionID, s.provider, s.userInfoFetcher, s.envVarFetcher)

	s.mu.Lock()
	s.sessions[sessionID] = handler
	s.mu.Unlock()

	// Notify connection tracker while the SSH connection is alive.
	if s.connectionTracker != nil {
		release := s.connectionTracker.Track(sessionID)
		defer release()
	}

	defer func() {
		s.mu.Lock()
		delete(s.sessions, sessionID)
		s.mu.Unlock()
		sshConn.Close()
		log.Printf("SSH connection closed for session %s", sessionID)
	}()

	// Handle global requests (keepalive, etc.)
	go ssh.DiscardRequests(reqs)

	// Handle channels
	for newChannel := range chans {
		go handler.handleChannel(newChannel)
	}
}

// loadOrGenerateHostKey loads an SSH host key from disk, or generates a new one.
func loadOrGenerateHostKey(path string) (ssh.Signer, error) {
	// Try to load existing key
	if path != "" {
		if keyBytes, err := os.ReadFile(path); err == nil {
			return ssh.ParsePrivateKey(keyBytes)
		}
	}

	// Generate new RSA key
	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RSA key: %w", err)
	}

	// Encode to PEM
	privateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}
	keyBytes := pem.EncodeToMemory(privateKeyPEM)

	// Save to disk if path provided
	if path != "" {
		// Ensure directory exists
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, fmt.Errorf("failed to create key directory: %w", err)
		}

		if err := os.WriteFile(path, keyBytes, 0600); err != nil {
			return nil, fmt.Errorf("failed to save host key: %w", err)
		}
		log.Printf("Generated new SSH host key: %s", path)
	}

	return ssh.ParsePrivateKey(keyBytes)
}

// sessionHandler handles SSH channels for a specific session/sandbox.
type sessionHandler struct {
	sessionID       string
	provider        sandbox.Provider
	userInfoFetcher UserInfoFetcher
	envVarFetcher   EnvVarFetcher
}

func newSessionHandler(sessionID string, provider sandbox.Provider, userInfoFetcher UserInfoFetcher, envVarFetcher EnvVarFetcher) *sessionHandler {
	return &sessionHandler{
		sessionID:       sessionID,
		provider:        provider,
		userInfoFetcher: userInfoFetcher,
		envVarFetcher:   envVarFetcher,
	}
}

// getEnvVars fetches runtime-managed env vars for the session and merges them
// with the client-provided SSH env vars. SSH client vars take precedence.
func (h *sessionHandler) getEnvVars(ctx context.Context, sshEnvVars map[string]string) map[string]string {
	merged := map[string]string{}
	if h.envVarFetcher != nil {
		sessionVars, err := h.envVarFetcher.GetEnvVarsForSession(ctx, h.sessionID)
		if err != nil {
			log.Printf("SSH session %s: failed to get runtime env vars: %v", h.sessionID, err)
		} else {
			for k, v := range sessionVars {
				merged[k] = v
			}
		}
	}
	// SSH client-provided env vars take precedence over runtime-managed vars
	for k, v := range sshEnvVars {
		merged[k] = v
	}
	return merged
}

// getUser returns the user string (uid:gid format) for running commands.
// Falls back to empty string (default/root) if user info cannot be fetched.
func (h *sessionHandler) getUser(ctx context.Context) string {
	if h.userInfoFetcher == nil {
		return ""
	}

	_, uid, gid, err := h.userInfoFetcher.GetUserInfo(ctx, h.sessionID)
	if err != nil {
		log.Printf("SSH session %s: failed to get user info, using default: %v", h.sessionID, err)
		return ""
	}

	return strconv.Itoa(uid) + ":" + strconv.Itoa(gid)
}

func (h *sessionHandler) handleChannel(newChannel ssh.NewChannel) {
	switch newChannel.ChannelType() {
	case "session":
		h.handleSessionChannel(newChannel)
	case "direct-tcpip":
		h.handleDirectTCPIP(newChannel)
	default:
		log.Printf("SSH session %s: rejecting unknown channel type: %s",
			h.sessionID, newChannel.ChannelType())
		_ = newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
	}
}

// resizeable is implemented by sandbox.PTY to allow terminal resize.
type resizeable interface {
	Resize(ctx context.Context, rows, cols int) error
}

func (h *sessionHandler) handleSessionChannel(newChannel ssh.NewChannel) {
	channel, requests, err := newChannel.Accept()
	if err != nil {
		log.Printf("SSH session %s: failed to accept channel: %v", h.sessionID, err)
		return
	}
	defer channel.Close()

	// Track PTY settings
	var ptyReq *ptyRequest
	var envVars = make(map[string]string)

	// activePTY holds a reference to the running PTY so window-change
	// requests can resize it. Set by runShell/runExec via the onPTY callback.
	var activePTY resizeable
	var ptyMu sync.Mutex

	// done is closed when the shell/exec goroutine finishes.
	done := make(chan struct{})
	started := false

	for {
		select {
		case <-done:
			// Shell/exec goroutine finished — exit the handler.
			return

		case req, ok := <-requests:
			if !ok {
				// Channel request stream closed.
				if started {
					<-done
				}
				return
			}

			switch req.Type {
			case "pty-req":
				ptyReq = parsePTYRequest(req.Payload)
				if req.WantReply {
					_ = req.Reply(true, nil)
				}

			case "env":
				// Parse environment variable request
				name, value := parseEnvRequest(req.Payload)
				envVars[name] = value
				if req.WantReply {
					_ = req.Reply(true, nil)
				}

			case "shell":
				if req.WantReply {
					_ = req.Reply(true, nil)
				}
				started = true
				go func() {
					defer close(done)
					h.runShell(channel, ptyReq, envVars, func(pty resizeable) {
						ptyMu.Lock()
						activePTY = pty
						ptyMu.Unlock()
					})
				}()

			case "exec":
				command := parseExecRequest(req.Payload)
				if req.WantReply {
					_ = req.Reply(true, nil)
				}
				started = true
				go func() {
					defer close(done)
					h.runExec(channel, command, ptyReq, envVars, func(pty resizeable) {
						ptyMu.Lock()
						activePTY = pty
						ptyMu.Unlock()
					})
				}()

			case "subsystem":
				subsystem := parseSubsystemRequest(req.Payload)
				if subsystem == "sftp" {
					if req.WantReply {
						_ = req.Reply(true, nil)
					}
					h.runSFTP(channel)
					return
				}
				if req.WantReply {
					_ = req.Reply(false, nil)
				}

			case "window-change":
				cols, rows := parseWindowChange(req.Payload)
				log.Printf("SSH session %s: window-change received: cols=%d rows=%d (payload=%d bytes)",
					h.sessionID, cols, rows, len(req.Payload))
				ptyMu.Lock()
				p := activePTY
				ptyMu.Unlock()
				if p != nil {
					log.Printf("SSH session %s: sending resize to PTY: rows=%d cols=%d", h.sessionID, rows, cols)
					if resizeErr := p.Resize(context.Background(), int(rows), int(cols)); resizeErr != nil {
						log.Printf("SSH session %s: PTY resize error: %v", h.sessionID, resizeErr)
					} else {
						log.Printf("SSH session %s: PTY resize succeeded", h.sessionID)
					}
				} else {
					log.Printf("SSH session %s: window-change ignored, no active PTY", h.sessionID)
				}
				if req.WantReply {
					_ = req.Reply(true, nil)
				}

			default:
				log.Printf("SSH session %s: unknown request type: %s", h.sessionID, req.Type)
				if req.WantReply {
					_ = req.Reply(false, nil)
				}
			}
		}
	}
}

func (h *sessionHandler) runShell(channel ssh.Channel, ptyReq *ptyRequest, envVars map[string]string, onPTY func(resizeable)) {
	ctx := context.Background()

	// Get user for this session (uid:gid format)
	user := h.getUser(ctx)

	// Merge runtime-managed env vars with SSH client-provided vars (client takes precedence)
	mergedEnv := h.getEnvVars(ctx, envVars)

	opts := sandbox.AttachOptions{
		Env:  mergedEnv,
		User: user,
	}
	if ptyReq != nil {
		opts.Rows = int(ptyReq.Rows)
		opts.Cols = int(ptyReq.Cols)
	}

	pty, err := h.provider.Attach(ctx, h.sessionID, opts)
	if err != nil {
		log.Printf("SSH session %s: failed to attach: %v", h.sessionID, err)
		sendExitStatus(channel, 1)
		return
	}
	defer pty.Close()

	// Expose PTY for window-change resize handling
	if onPTY != nil {
		onPTY(pty)
	}

	// Done channel to signal when PTY output is fully drained
	outputDone := make(chan struct{})

	// Channel -> PTY (stdin) - will be terminated when channel closes
	go func() {
		_, _ = io.Copy(pty, channel)
	}()

	// PTY -> Channel (stdout) - completes when PTY returns EOF after process exits
	go func() {
		_, _ = io.Copy(channel, pty)
		close(outputDone)
	}()

	// Wait for PTY to exit
	exitCode, _ := pty.Wait(ctx)

	// Wait for output to drain before sending exit status
	<-outputDone

	sendExitStatus(channel, uint32(exitCode))
}

func (h *sessionHandler) runExec(channel ssh.Channel, command string, ptyReq *ptyRequest, envVars map[string]string, onResize func(resizeable)) {
	ctx := context.Background()

	// Get user for this session (uid:gid format)
	user := h.getUser(ctx)

	// Merge runtime-managed env vars with SSH client-provided vars (client takes precedence)
	mergedEnv := h.getEnvVars(ctx, envVars)

	// Execute command in sandbox using streaming to avoid buffering large outputs.
	stream, err := h.provider.ExecStream(ctx, h.sessionID, []string{"sh", "-c", command}, sandbox.ExecStreamOptions{
		Env:  mergedEnv,
		User: user,
		TTY:  ptyReq != nil,
	})

	if err != nil {
		log.Printf("SSH session %s: exec failed: %v", h.sessionID, err)
		fmt.Fprintf(channel.Stderr(), "exec error: %v\n", err)
		sendExitStatus(channel, 1)
		return
	}
	defer stream.Close()

	// Expose stream for window-change resize handling (effective when TTY=true)
	if onResize != nil && ptyReq != nil {
		onResize(stream)
	}

	// Done channels to signal when output is fully drained
	stdoutDone := make(chan struct{})
	stderrDone := make(chan struct{})

	// Channel -> command stdin
	go func() {
		_, _ = io.Copy(stream, channel)
		_ = stream.CloseWrite()
	}()

	// Command stdout -> Channel
	go func() {
		_, _ = io.Copy(channel, stream)
		close(stdoutDone)
	}()

	// Command stderr -> Channel.Stderr() (if available)
	go func() {
		if stderr := stream.Stderr(); stderr != nil {
			_, _ = io.Copy(channel.Stderr(), stderr)
		}
		close(stderrDone)
	}()

	// Wait for command to exit
	exitCode, _ := stream.Wait(ctx)

	// Wait for all output to drain before sending exit status
	<-stdoutDone
	<-stderrDone

	sendExitStatus(channel, uint32(exitCode))
}

func (h *sessionHandler) runSFTP(channel ssh.Channel) {
	ctx := context.Background()

	// Get user for this session (uid:gid format)
	user := h.getUser(ctx)

	// Run sftp-server inside the container using ExecStream for bidirectional I/O
	// The sftp-server binary handles the SFTP protocol
	stream, err := h.provider.ExecStream(ctx, h.sessionID, []string{"/usr/lib/openssh/sftp-server"}, sandbox.ExecStreamOptions{
		User: user,
	})
	if err != nil {
		log.Printf("SSH session %s: sftp-server failed to start: %v", h.sessionID, err)
		return
	}
	defer stream.Close()

	// Done channel to signal when server output is fully drained
	outputDone := make(chan struct{})

	// Channel -> SFTP server stdin - will be terminated when channel closes
	go func() {
		_, _ = io.Copy(stream, channel)
		_ = stream.CloseWrite()
	}()

	// SFTP server stdout -> Channel - completes when server exits
	go func() {
		_, _ = io.Copy(channel, stream)
		close(outputDone)
	}()

	// Wait for sftp-server process to exit
	_, _ = stream.Wait(ctx)

	// Wait for output to drain
	<-outputDone
}

func (h *sessionHandler) handleDirectTCPIP(newChannel ssh.NewChannel) {
	// Parse direct-tcpip request
	data := newChannel.ExtraData()
	destHost, destPort, origHost, origPort := parseDirectTCPIPData(data)

	log.Printf("SSH session %s: direct-tcpip %s:%d -> %s:%d",
		h.sessionID, origHost, origPort, destHost, destPort)

	// Accept the channel
	channel, _, err := newChannel.Accept()
	if err != nil {
		log.Printf("SSH session %s: failed to accept direct-tcpip channel: %v", h.sessionID, err)
		return
	}
	defer channel.Close()

	ctx := context.Background()

	// Get user for this session (uid:gid format)
	user := h.getUser(ctx)

	// Use socat to forward the connection inside the container
	// socat reads from stdin, writes to TCP; reads from TCP, writes to stdout
	cmd := []string{"socat", "-", fmt.Sprintf("TCP:%s:%d", destHost, destPort)}
	stream, err := h.provider.ExecStream(ctx, h.sessionID, cmd, sandbox.ExecStreamOptions{
		User: user,
	})
	if err != nil {
		log.Printf("SSH session %s: failed to start socat: %v", h.sessionID, err)
		return
	}
	defer stream.Close()

	// Done channel to signal when forwarding completes
	outputDone := make(chan struct{})

	// Channel -> socat stdin (to remote TCP)
	go func() {
		_, _ = io.Copy(stream, channel)
		_ = stream.CloseWrite()
	}()

	// socat stdout (from remote TCP) -> Channel
	go func() {
		_, _ = io.Copy(channel, stream)
		close(outputDone)
	}()

	// Wait for socat to exit (connection closed from either end)
	_, _ = stream.Wait(ctx)

	// Wait for output to drain
	<-outputDone
}

// sendExitStatus sends the exit-status request to signal command completion.
func sendExitStatus(channel ssh.Channel, code uint32) {
	payload := make([]byte, 4)
	payload[0] = byte(code >> 24)
	payload[1] = byte(code >> 16)
	payload[2] = byte(code >> 8)
	payload[3] = byte(code)
	_, _ = channel.SendRequest("exit-status", false, payload)
}

// PTY request parsing
type ptyRequest struct {
	Term   string
	Cols   uint32
	Rows   uint32
	Width  uint32
	Height uint32
}

func parsePTYRequest(payload []byte) *ptyRequest {
	if len(payload) < 4 {
		return nil
	}

	// Parse term string
	termLen := uint32(payload[0])<<24 | uint32(payload[1])<<16 | uint32(payload[2])<<8 | uint32(payload[3])
	if len(payload) < int(4+termLen+16) {
		return nil
	}

	term := string(payload[4 : 4+termLen])
	offset := 4 + termLen

	// Parse dimensions
	cols := uint32(payload[offset])<<24 | uint32(payload[offset+1])<<16 | uint32(payload[offset+2])<<8 | uint32(payload[offset+3])
	offset += 4
	rows := uint32(payload[offset])<<24 | uint32(payload[offset+1])<<16 | uint32(payload[offset+2])<<8 | uint32(payload[offset+3])
	offset += 4
	width := uint32(payload[offset])<<24 | uint32(payload[offset+1])<<16 | uint32(payload[offset+2])<<8 | uint32(payload[offset+3])
	offset += 4
	height := uint32(payload[offset])<<24 | uint32(payload[offset+1])<<16 | uint32(payload[offset+2])<<8 | uint32(payload[offset+3])

	return &ptyRequest{
		Term:   term,
		Cols:   cols,
		Rows:   rows,
		Width:  width,
		Height: height,
	}
}

func parseWindowChange(payload []byte) (cols, rows uint32) {
	if len(payload) < 8 {
		return 0, 0
	}
	cols = uint32(payload[0])<<24 | uint32(payload[1])<<16 | uint32(payload[2])<<8 | uint32(payload[3])
	rows = uint32(payload[4])<<24 | uint32(payload[5])<<16 | uint32(payload[6])<<8 | uint32(payload[7])
	return
}

func parseEnvRequest(payload []byte) (name, value string) {
	if len(payload) < 4 {
		return "", ""
	}

	nameLen := uint32(payload[0])<<24 | uint32(payload[1])<<16 | uint32(payload[2])<<8 | uint32(payload[3])
	if len(payload) < int(4+nameLen+4) {
		return "", ""
	}

	name = string(payload[4 : 4+nameLen])
	offset := 4 + nameLen

	valueLen := uint32(payload[offset])<<24 | uint32(payload[offset+1])<<16 | uint32(payload[offset+2])<<8 | uint32(payload[offset+3])
	if len(payload) < int(offset+4+valueLen) {
		return name, ""
	}

	value = string(payload[offset+4 : offset+4+valueLen])
	return name, value
}

func parseExecRequest(payload []byte) string {
	if len(payload) < 4 {
		return ""
	}

	cmdLen := uint32(payload[0])<<24 | uint32(payload[1])<<16 | uint32(payload[2])<<8 | uint32(payload[3])
	if len(payload) < int(4+cmdLen) {
		return ""
	}

	return string(payload[4 : 4+cmdLen])
}

func parseSubsystemRequest(payload []byte) string {
	return parseExecRequest(payload) // Same format
}

func parseDirectTCPIPData(data []byte) (destHost string, destPort uint32, origHost string, origPort uint32) {
	if len(data) < 4 {
		return
	}

	offset := 0

	// Destination host
	hostLen := uint32(data[offset])<<24 | uint32(data[offset+1])<<16 | uint32(data[offset+2])<<8 | uint32(data[offset+3])
	offset += 4
	if len(data) < offset+int(hostLen)+4 {
		return
	}
	destHost = string(data[offset : offset+int(hostLen)])
	offset += int(hostLen)

	// Destination port
	destPort = uint32(data[offset])<<24 | uint32(data[offset+1])<<16 | uint32(data[offset+2])<<8 | uint32(data[offset+3])
	offset += 4

	// Originator host
	if len(data) < offset+4 {
		return
	}
	hostLen = uint32(data[offset])<<24 | uint32(data[offset+1])<<16 | uint32(data[offset+2])<<8 | uint32(data[offset+3])
	offset += 4
	if len(data) < offset+int(hostLen)+4 {
		return
	}
	origHost = string(data[offset : offset+int(hostLen)])
	offset += int(hostLen)

	// Originator port
	if len(data) < offset+4 {
		return
	}
	origPort = uint32(data[offset])<<24 | uint32(data[offset+1])<<16 | uint32(data[offset+2])<<8 | uint32(data[offset+3])

	return
}
