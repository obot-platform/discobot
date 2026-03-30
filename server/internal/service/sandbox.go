package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/conntrack"
	"github.com/obot-platform/discobot/server/internal/events"
	"github.com/obot-platform/discobot/server/internal/jobs"
	"github.com/obot-platform/discobot/server/internal/model"
	"github.com/obot-platform/discobot/server/internal/sandbox"
	"github.com/obot-platform/discobot/server/internal/store"
)

// GitConfigProvider retrieves the git user name and email configuration.
// It is called once and the result is cached.
type GitConfigProvider func(ctx context.Context) (name, email string)

// SandboxService manages sandbox lifecycle for sessions.
type SandboxService struct {
	store              *store.Store
	provider           sandbox.Provider
	cfg                *config.Config
	credentialFetcher  CredentialFetcher
	eventBroker        *events.Broker
	jobEnqueuer        JobEnqueuer
	sessionInitializer SessionInitializer

	// Git user config cache - populated once on first GetClient call
	gitConfigProvider GitConfigProvider
	gitConfigOnce     sync.Once
	gitUserName       string
	gitUserEmail      string

	// Connection tracker for idle timeout — shared with idle monitor, SSH server, and service proxy.
	connTracker *conntrack.Tracker

	// Activity tracking for idle timeout
	lastActivityMap map[string]time.Time
	lastActivityMu  sync.RWMutex

	// Health probe cache — skip probing if container was healthy recently
	healthCacheMap map[string]time.Time
	healthCacheMu  sync.RWMutex
}

const healthCacheTTL = 10 * time.Second

// NewSandboxService creates a new sandbox service.
// connTracker may be nil; when set it tracks active streaming connections so the
// idle monitor can avoid stopping sandboxes that still have live clients.
func NewSandboxService(s *store.Store, p sandbox.Provider, cfg *config.Config, credFetcher CredentialFetcher, eventBroker *events.Broker, jobEnqueuer JobEnqueuer, connTracker *conntrack.Tracker) *SandboxService {
	return &SandboxService{
		store:             s,
		provider:          p,
		cfg:               cfg,
		credentialFetcher: credFetcher,
		eventBroker:       eventBroker,
		jobEnqueuer:       jobEnqueuer,
		connTracker:       connTracker,
		lastActivityMap:   make(map[string]time.Time),
		healthCacheMap:    make(map[string]time.Time),
	}
}

// SetSessionInitializer sets the session initializer (post-construction to break circular dependency).
func (s *SandboxService) SetSessionInitializer(init SessionInitializer) {
	s.sessionInitializer = init
}

// SetGitConfigProvider sets the function used to look up git user config.
// The result is cached after the first call.
func (s *SandboxService) SetGitConfigProvider(provider GitConfigProvider) {
	s.gitConfigProvider = provider
}

// getGitConfig returns the cached Git user configuration.
// On first call, fetches the config from the provider and caches it.
func (s *SandboxService) getGitConfig(ctx context.Context) (name, email string) {
	s.gitConfigOnce.Do(func() {
		if s.gitConfigProvider != nil {
			s.gitUserName, s.gitUserEmail = s.gitConfigProvider(ctx)
			log.Printf("[SandboxService] Cached Git user config: name=%q email=%q", s.gitUserName, s.gitUserEmail)
		}
	})
	return s.gitUserName, s.gitUserEmail
}

// GetClient ensures the sandbox is ready and returns a session-bound client.
func (s *SandboxService) GetClient(ctx context.Context, sessionID string) (*SessionClient, error) {
	if err := s.ensureSandboxReady(ctx, sessionID); err != nil {
		return nil, err
	}

	gitName, gitEmail := s.getGitConfig(ctx)

	inner := NewSandboxChatClient(s.provider, s.credentialFetcher, &SandboxChatClientConfig{
		GitUserName:  gitName,
		GitUserEmail: gitEmail,
	})

	return &SessionClient{
		sessionID:       sessionID,
		inner:           inner,
		sandboxSvc:      s,
		activityTracker: s.RecordActivity,
		connTracker:     s.connTracker,
	}, nil
}

// ensureSandboxReady checks the session state from the database and ensures
// the sandbox is ready. For states like "stopped" or "error", it triggers reconciliation.
// For "initializing" states, it waits briefly then reconciles if still not ready.
func (s *SandboxService) ensureSandboxReady(ctx context.Context, sessionID string) error {
	sess, err := s.store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("session not found: %w", err)
	}

	switch sess.Status {
	case model.SessionStatusReady, legacySessionStatusRunning:
		// Session status looks good — verify the container is actually running.
		// This fast-path check avoids expensive reconciliation when everything is healthy.
		sb, err := s.provider.Get(ctx, sessionID)
		if errors.Is(err, sandbox.ErrNotFound) || (err == nil && sb.Status != sandbox.StatusRunning) {
			log.Printf("Session %s status is %s but container not running, reconciling", sessionID, sess.Status)
			return s.ReconcileSandbox(ctx, sessionID)
		}
		if err != nil {
			return fmt.Errorf("failed to check sandbox status: %w", err)
		}
		// Container is running per Docker — verify services are actually responsive.
		// This catches cases where the container is mid-shutdown (SIGTERM received,
		// Docker still reports "running", but internal services are dead).
		if err := s.probeSandboxHealth(ctx, sessionID); err != nil {
			log.Printf("Session %s container running but health check failed (%v), reconciling", sessionID, err)
			return s.ReconcileSandbox(ctx, sessionID)
		}
		return nil
	case model.SessionStatusStopped, model.SessionStatusError:
		return s.ReconcileSandbox(ctx, sessionID)
	case model.SessionStatusInitializing, model.SessionStatusReinitializing,
		model.SessionStatusCloning, model.SessionStatusPullingImage, model.SessionStatusCreatingSandbox:
		if err := s.waitForSessionReady(ctx, sessionID); err != nil {
			log.Printf("Session %s wait failed (%v), attempting reconciliation", sessionID, err)
			return s.ReconcileSandbox(ctx, sessionID)
		}
		return nil
	default:
		return s.ReconcileSandbox(ctx, sessionID)
	}
}

// waitForSessionReady polls the session status until it reaches a terminal state.
func (s *SandboxService) waitForSessionReady(ctx context.Context, sessionID string) error {
	const (
		pollInterval = 500 * time.Millisecond
		maxWait      = 30 * time.Second
	)

	deadline := time.Now().Add(maxWait)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		sess, err := s.store.GetSessionByID(ctx, sessionID)
		if err != nil {
			return fmt.Errorf("session not found: %w", err)
		}

		switch sess.Status {
		case model.SessionStatusReady:
			return nil
		case model.SessionStatusError, model.SessionStatusStopped:
			return fmt.Errorf("session in %s state", sess.Status)
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for session to be ready (status: %s)", sess.Status)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// invalidateHealthCache clears the cached health status for a session,
// forcing the next request to perform a real probe.
func (s *SandboxService) invalidateHealthCache(sessionID string) {
	s.healthCacheMu.Lock()
	delete(s.healthCacheMap, sessionID)
	s.healthCacheMu.Unlock()
}

// ReconcileSandbox reinitializes the sandbox by enqueuing a job and waiting for completion.
func (s *SandboxService) ReconcileSandbox(ctx context.Context, sessionID string) error {
	s.invalidateHealthCache(sessionID)
	log.Printf("Reconciling sandbox for session %s", sessionID)

	// Look up projectID from session
	sess, err := s.store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}
	projectID := sess.ProjectID

	// Update status to reinitializing
	if err := s.store.UpdateSessionStatus(ctx, sessionID, model.SessionStatusReinitializing, nil); err != nil {
		log.Printf("Warning: failed to update session status for %s: %v", sessionID, err)
	}

	// Emit SSE event for status change
	if s.eventBroker != nil {
		if err := s.eventBroker.PublishSessionUpdated(ctx, projectID, sessionID, model.SessionStatusReinitializing, ""); err != nil {
			log.Printf("Warning: failed to publish session update event: %v", err)
		}
	}

	// If job enqueuer is not available (e.g., in tests), fall back to direct initialization
	if s.jobEnqueuer == nil {
		log.Printf("Job enqueuer not available, falling back to direct initialization for session %s", sessionID)
		if s.sessionInitializer == nil {
			return fmt.Errorf("no session initializer available for session %s", sessionID)
		}
		if err := s.sessionInitializer.Initialize(ctx, sessionID); err != nil {
			return fmt.Errorf("failed to reinitialize sandbox: %w", err)
		}
		return nil
	}

	// Enqueue initialization job
	err = s.jobEnqueuer.Enqueue(ctx, jobs.SessionInitPayload{
		ProjectID:   projectID,
		SessionID:   sessionID,
		WorkspaceID: sess.WorkspaceID,
	})
	if err != nil {
		log.Printf("Note: session init job may already exist for %s: %v", sessionID, err)
	}

	// Wait for job to complete
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	status, errorMsg, err := events.WaitForJobCompletion(waitCtx, s.eventBroker, s.store, projectID, "session", sessionID)
	if err != nil {
		return fmt.Errorf("failed to wait for job completion: %w", err)
	}

	if status == "failed" {
		return fmt.Errorf("session initialization failed: %s", errorMsg)
	}

	log.Printf("Session %s initialized successfully via job", sessionID)
	return nil
}

// CreateForSession creates and starts a sandbox for the given session.
// It retrieves the workspace path and commit from the session in the database
// and generates a cryptographically secure shared secret.
func (s *SandboxService) CreateForSession(ctx context.Context, sessionID string) error {
	// Get session to retrieve workspace path and commit
	session, err := s.store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}

	// Workspace path should be set during session initialization
	workspacePath := ""
	if session.WorkspacePath != nil {
		workspacePath = *session.WorkspacePath
	}
	if workspacePath == "" {
		return fmt.Errorf("session %s has no workspace path set", sessionID)
	}

	// Workspace commit may be empty for local (non-git) workspaces
	workspaceCommit := ""
	if session.WorkspaceCommit != nil {
		workspaceCommit = *session.WorkspaceCommit
	}

	// Get workspace source for the WORKSPACE_SOURCE env var
	workspace, err := s.store.GetWorkspaceByID(ctx, session.WorkspaceID)
	if err != nil {
		return fmt.Errorf("failed to get workspace: %w", err)
	}

	// Generate or load the sandbox SSH identity for this session.
	sshKey, err := ensureSessionSSHKey(ctx, s.store, s.cfg, session)
	if err != nil {
		return fmt.Errorf("failed to ensure sandbox ssh key: %w", err)
	}

	// Generate a cryptographically secure shared secret
	sharedSecret := generateSandboxSecret(32)

	// Create sandbox with session configuration
	// Note: The sandbox image is configured globally on the provider via SANDBOX_IMAGE env var
	opts := sandbox.CreateOptions{
		SharedSecret: sharedSecret,
		SSHKey:       sshKey,
		Labels: map[string]string{
			"discobot.session.id":   sessionID,
			"discobot.workspace.id": session.WorkspaceID,
			"discobot.project.id":   session.ProjectID,
		},
		WorkspacePath:        workspacePath,
		WorkspaceSource:      workspace.Path, // Original workspace path (local or git URL)
		WorkspaceCommit:      workspaceCommit,
		ProjectID:            session.ProjectID,
		MCPOAuthRedirectBase: s.cfg.MCPOAuthRedirectBase,
		AgentServerURL:       s.cfg.AgentServerURL,
		Resources: sandbox.ResourceConfig{
			Timeout: s.cfg.SandboxIdleTimeout,
		},
	}

	// Create the sandbox
	_, err = s.provider.Create(ctx, sessionID, opts)
	if err != nil {
		return fmt.Errorf("failed to create sandbox: %w", err)
	}

	// Start the sandbox immediately
	if err := s.provider.Start(ctx, sessionID); err != nil {
		// Clean up on failure (don't need to remove volumes since this is a new sandbox)
		_ = s.provider.Remove(ctx, sessionID)
		return fmt.Errorf("failed to start sandbox: %w", err)
	}

	return nil
}

// generateSandboxSecret generates a cryptographically secure random hex string.
func generateSandboxSecret(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to a less random but still unique value
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

// GetForSession returns the sandbox state for a session.
func (s *SandboxService) GetForSession(ctx context.Context, sessionID string) (*sandbox.Sandbox, error) {
	return s.provider.Get(ctx, sessionID)
}

// Exec runs a non-interactive command in the session's sandbox.
func (s *SandboxService) Exec(ctx context.Context, sessionID string, cmd []string, opts sandbox.ExecOptions) (*sandbox.ExecResult, error) {
	return s.provider.Exec(ctx, sessionID, cmd, opts)
}

// Attach creates an interactive PTY session to the sandbox.
// If user is empty, the container's default user is used.
// env contains additional environment variables to set in the session.
func (s *SandboxService) Attach(ctx context.Context, sessionID string, rows, cols int, user string, env map[string]string) (sandbox.PTY, error) {
	opts := sandbox.AttachOptions{
		Rows: rows,
		Cols: cols,
		User: user,
		Env:  env,
	}
	return s.provider.Attach(ctx, sessionID, opts)
}

// StopForSession stops the sandbox for a session.
func (s *SandboxService) StopForSession(ctx context.Context, sessionID string) error {
	return s.provider.Stop(ctx, sessionID, 10*time.Second)
}

// probeSandboxHealth does a fast, single-attempt HTTP health check against the
// sandbox's agent-api. It uses a short timeout (2s) to quickly detect dead or
// dying containers without blocking for the full retry backoff (~14s).
// Results are cached per session for healthCacheTTL to reduce probe frequency.
func (s *SandboxService) probeSandboxHealth(ctx context.Context, sessionID string) error {
	s.healthCacheMu.RLock()
	lastHealthy, ok := s.healthCacheMap[sessionID]
	s.healthCacheMu.RUnlock()
	if ok && time.Since(lastHealthy) < healthCacheTTL {
		return nil
	}

	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	httpClient, err := s.provider.HTTPClient(probeCtx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get HTTP client: %w", err)
	}

	req, err := http.NewRequestWithContext(probeCtx, "GET", "http://sandbox/health", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return fmt.Errorf("health check returned %d", resp.StatusCode)
	}

	s.healthCacheMu.Lock()
	s.healthCacheMap[sessionID] = time.Now()
	s.healthCacheMu.Unlock()
	return nil
}

// DestroyForSession removes the sandbox when a session is deleted.
// This is deprecated - use SessionService.PerformDeletion instead which handles volumes.
func (s *SandboxService) DestroyForSession(ctx context.Context, sessionID string) error {
	err := s.provider.Remove(ctx, sessionID)
	if errors.Is(err, sandbox.ErrNotFound) {
		// Already removed, not an error
		return nil
	}
	return err
}

// Provider returns the underlying provider for advanced operations.
func (s *SandboxService) Provider() sandbox.Provider {
	return s.provider
}

// ReconcileSandboxes checks all existing sandboxes and recreates any that
// are using an outdated image. This should be called on server startup.
func (s *SandboxService) ReconcileSandboxes(ctx context.Context) error {
	expectedImage := s.provider.Image()
	if expectedImage == "" {
		log.Printf("No sandbox image configured, skipping reconciliation")
		return nil
	}

	providerForImageOps := s.provider
	if defaultProviderGetter, ok := s.provider.(interface{ DefaultProvider() sandbox.Provider }); ok {
		if defaultProvider := defaultProviderGetter.DefaultProvider(); defaultProvider != nil {
			providerForImageOps = defaultProvider
		}
	}

	expectedImageID := ""
	if imageIDProvider, ok := providerForImageOps.(sandbox.CurrentImageIDProvider); ok {
		imageID, err := imageIDProvider.CurrentImageID(ctx)
		if err != nil {
			log.Printf("Warning: Failed to resolve current sandbox image ID for %s: %v", expectedImage, err)
		} else {
			expectedImageID = imageID
		}
	}

	sandboxes, err := s.provider.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list sandboxes: %w", err)
	}

	log.Printf("Reconciling %d sandboxes (expected image: %s)", len(sandboxes), expectedImage)

	for _, sb := range sandboxes {
		usesExpectedImage := sb.Image == expectedImage
		if expectedImageID != "" {
			if sandboxImageID := sb.Metadata[sandbox.MetadataImageID]; sandboxImageID != "" {
				usesExpectedImage = sandboxImageID == expectedImageID
			} else {
				log.Printf("Sandbox for session %s is missing image ID metadata, falling back to image reference comparison", sb.SessionID)
			}
		}

		if usesExpectedImage {
			log.Printf("Sandbox for session %s uses correct image", sb.SessionID)
			continue
		}

		log.Printf("Sandbox for session %s uses outdated image %s (expected %s), recreating...",
			sb.SessionID, sb.Image, expectedImage)

		// Check if the session exists; if not, remove orphaned sandbox unless it is
		// still within the deferred deletion retention window.
		_, err := s.store.GetSessionByID(ctx, sb.SessionID)
		if err != nil {
			retained, retainedErr := s.store.HasActiveJobForResource(ctx, jobs.ResourceTypeRetainedSandbox, sb.SessionID)
			if retainedErr != nil {
				log.Printf("Failed to check deferred sandbox delete job for session %s: %v", sb.SessionID, retainedErr)
			}
			if retained {
				log.Printf("Preserving retained sandbox for deleted session %s until deferred delete job runs", sb.SessionID)
				continue
			}

			log.Printf("Failed to get session %s, removing orphaned sandbox: %v", sb.SessionID, err)
			// Preserve volumes for orphaned sandboxes in case of recovery
			if err := s.provider.Remove(ctx, sb.SessionID); err != nil {
				log.Printf("Failed to remove orphaned sandbox for session %s: %v", sb.SessionID, err)
			}
			continue
		}

		// Remove the old sandbox (preserve volume for image update)
		if err := s.provider.Remove(ctx, sb.SessionID); err != nil {
			log.Printf("Failed to remove sandbox for session %s: %v", sb.SessionID, err)
			continue
		}

		// Recreate the sandbox with the correct image via job system
		// This ensures proper serialization with any concurrent user operations
		if err := s.ReconcileSandbox(ctx, sb.SessionID); err != nil {
			log.Printf("Failed to recreate sandbox for session %s: %v", sb.SessionID, err)
			continue
		}

		log.Printf("Successfully recreated sandbox for session %s with image %s", sb.SessionID, expectedImage)
	}

	if cleanupProvider, ok := providerForImageOps.(sandbox.CleanupUnusedImagesProvider); ok {
		if err := cleanupProvider.CleanupUnusedImages(ctx); err != nil {
			log.Printf("Warning: Failed to clean up sandbox images: %v", err)
		}
	}

	// Run provider-specific reconciliation (BuildKit containers, etc.)
	if err := s.provider.Reconcile(ctx); err != nil {
		log.Printf("Warning: Provider reconciliation failed: %v", err)
	}

	return nil
}

// ReconcileSessionStates checks sessions that the database considers active or
// in-progress and verifies their sandbox state matches. If a sandbox has failed,
// the session is marked as error. If the sandbox is stopped or doesn't exist,
// the session is marked as stopped. This should be called on server startup
// after ReconcileSandboxes.
//
// This handles three cases:
//  1. Sessions marked "ready" but sandbox is missing/stopped/failed
//  2. Sessions stuck in intermediate states (initializing, creating_sandbox, etc.)
//     where the server died mid-creation and the sandbox doesn't exist
func (s *SandboxService) ReconcileSessionStates(ctx context.Context) error {
	// Get all sessions that need reconciliation:
	// - "ready" sessions where sandbox might have died
	// - intermediate states where server might have died mid-creation
	statesToReconcile := []string{
		model.SessionStatusReady,
		legacySessionStatusRunning,
		model.SessionStatusInitializing,
		model.SessionStatusReinitializing,
		model.SessionStatusCloning,
		model.SessionStatusPullingImage,
		model.SessionStatusCreatingSandbox,
	}

	activeSessions, err := s.store.ListSessionsByStatuses(ctx, statesToReconcile)
	if err != nil {
		return fmt.Errorf("failed to list active sessions: %w", err)
	}

	log.Printf("Reconciling state for %d active/in-progress sessions", len(activeSessions))

	for _, session := range activeSessions {
		sb, err := s.provider.Get(ctx, session.ID)
		if errors.Is(err, sandbox.ErrNotFound) {
			// Sandbox doesn't exist - mark as stopped, will be recreated on demand
			log.Printf("Session %s (status: %s) has no sandbox, marking as stopped", session.ID, session.Status)
			if err := s.store.UpdateSessionStatus(ctx, session.ID, model.SessionStatusStopped, nil); err != nil {
				log.Printf("Failed to update session %s status: %v", session.ID, err)
			}
			continue
		}
		if err != nil {
			log.Printf("Failed to get sandbox for session %s: %v", session.ID, err)
			continue
		}

		// Check if sandbox is in a failed state
		if sb.Status == sandbox.StatusFailed {
			log.Printf("Session %s has failed sandbox (error: %s), marking session as error", session.ID, sb.Error)
			errMsg := fmt.Sprintf("Sandbox failed: %s", sb.Error)
			if err := s.store.UpdateSessionStatus(ctx, session.ID, model.SessionStatusError, &errMsg); err != nil {
				log.Printf("Failed to update session %s status: %v", session.ID, err)
			}
			continue
		}

		// Check if sandbox is stopped or just created (not running)
		if sb.Status == sandbox.StatusStopped || sb.Status == sandbox.StatusCreated {
			log.Printf("Session %s has %s sandbox, marking as stopped", session.ID, sb.Status)
			if err := s.store.UpdateSessionStatus(ctx, session.ID, model.SessionStatusStopped, nil); err != nil {
				log.Printf("Failed to update session %s status: %v", session.ID, err)
			}
			continue
		}

		// Sandbox exists and is running
		if sb.Status == sandbox.StatusRunning {
			// Update session status if it was in intermediate state
			if session.Status != model.SessionStatusReady {
				log.Printf("Session %s was in %s state but sandbox is running, updating to ready", session.ID, session.Status)
				if err := s.store.UpdateSessionStatus(ctx, session.ID, model.SessionStatusReady, nil); err != nil {
					log.Printf("Failed to update session %s status: %v", session.ID, err)
				}
			}
			continue
		}

		log.Printf("Session %s (status: %s) sandbox status: %s", session.ID, session.Status, sb.Status)
	}

	return nil
}

// SandboxEndpoint contains the information needed to communicate with a sandbox.
type SandboxEndpoint struct {
	Port   int    // Host port mapped to sandbox port 3002
	Secret string // Raw shared secret (use for authentication)
}

// GetEndpoint returns the port and secret for communicating with the session's sandbox.
// The port is the host port mapped to sandbox port 3002.
// The secret is the raw shared secret stored during sandbox creation.
func (s *SandboxService) GetEndpoint(ctx context.Context, sessionID string) (*SandboxEndpoint, error) {
	sb, err := s.provider.Get(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get sandbox: %w", err)
	}

	// Find the host port for sandbox port 3002
	var port int
	for _, p := range sb.Ports {
		if p.ContainerPort == 3002 {
			port = p.HostPort
			break
		}
	}

	if port == 0 {
		return nil, fmt.Errorf("sandbox port 3002 not mapped")
	}

	// Get the raw secret from the provider
	secret, err := s.provider.GetSecret(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get sandbox secret: %w", err)
	}

	return &SandboxEndpoint{
		Port:   port,
		Secret: secret,
	}, nil
}

// RecordActivity updates the last activity time for a session.
// This is called automatically by SessionClient on successful operations.
func (s *SandboxService) RecordActivity(sessionID string) {
	s.lastActivityMu.Lock()
	s.lastActivityMap[sessionID] = time.Now()
	s.lastActivityMu.Unlock()
}

// GetLastActivity returns the last activity time for a session.
// Returns zero time if the session has no recorded activity.
func (s *SandboxService) GetLastActivity(sessionID string) time.Time {
	s.lastActivityMu.RLock()
	defer s.lastActivityMu.RUnlock()
	return s.lastActivityMap[sessionID]
}
