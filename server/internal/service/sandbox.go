package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/conntrack"
	"github.com/obot-platform/discobot/server/internal/encryption"
	"github.com/obot-platform/discobot/server/internal/events"
	"github.com/obot-platform/discobot/server/internal/git"
	"github.com/obot-platform/discobot/server/internal/jobs"
	"github.com/obot-platform/discobot/server/internal/model"
	"github.com/obot-platform/discobot/server/internal/sandbox"
	sandboxlocal "github.com/obot-platform/discobot/server/internal/sandbox/local"
	"github.com/obot-platform/discobot/server/internal/sandbox/sandboxapi"
	"github.com/obot-platform/discobot/server/internal/store"
)

// GitConfigProvider retrieves the git user name and email configuration.
// It is called once and the result is cached.
type GitConfigProvider func(ctx context.Context) (name, email string)

// SandboxEventHandler observes sandbox runtime lifecycle events after the
// session has been resolved. Handlers must not mutate the event.
type SandboxEventHandler func(ctx context.Context, event sandbox.StateEvent, session *model.Session)

// SandboxService manages sandbox lifecycle for sessions.
type SandboxService struct {
	store              *store.Store
	provider           sandbox.Provider
	providerManager    *sandbox.ProviderManager
	cfg                *config.Config
	credentialFetcher  CredentialFetcher
	credentialService  *CredentialService
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

const sandboxHealthWaitTimeout = 30 * time.Second

const sessionReadyWaitTimeout = 2 * time.Minute

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

// SetProviderManager sets the provider manager used for provider catalog and
// project-scoped capability operations.
func (s *SandboxService) SetProviderManager(manager *sandbox.ProviderManager) {
	s.providerManager = manager
}

// SetCredentialService sets the credential service used to validate provider
// instance credential configuration.
func (s *SandboxService) SetCredentialService(credSvc *CredentialService) {
	s.credentialService = credSvc
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

// Start watches sandbox lifecycle events and keeps persisted session state in
// sync with provider state. It handles external runtime changes, such as
// sandboxes being stopped or deleted outside of Discobot.
func (s *SandboxService) Start(ctx context.Context, handlers ...SandboxEventHandler) error {
	if s == nil || s.provider == nil {
		return fmt.Errorf("sandbox provider unavailable")
	}

	eventCh, err := s.WatchSandboxEvents(ctx)
	if err != nil {
		return err
	}

	log.Printf("[SandboxService] Started watching sandbox events")
	for {
		select {
		case <-ctx.Done():
			log.Printf("[SandboxService] Stopped watching sandbox events")
			return ctx.Err()
		case event, ok := <-eventCh:
			if !ok {
				log.Printf("[SandboxService] Sandbox event channel closed")
				return nil
			}
			s.handleSandboxEvent(ctx, event, handlers...)
		}
	}
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
	return s.newSessionClient(ctx, sessionID)
}

// AcquireHTTPClient returns a leased HTTP client for the session sandbox using
// persisted provider state when the provider requires it.
func (s *SandboxService) AcquireHTTPClient(ctx context.Context, sessionID string) (*sandbox.HTTPClientLease, error) {
	return s.acquireHTTPClient(ctx, sessionID)
}

// GetSandbox returns the current provider state for a session sandbox using
// persisted provider state when the provider requires it.
func (s *SandboxService) GetSandbox(ctx context.Context, sessionID string) (*sandbox.Sandbox, error) {
	return s.getSandbox(ctx, sessionID)
}

// ClonesGitWorkspaces reports whether the provider expects git URL workspaces
// to be cloned inside the sandbox instead of on the host.
func (s *SandboxService) ClonesGitWorkspaces() bool {
	return sandboxClonesGitWorkspace(s.provider)
}

// ListSandboxes returns all sandboxes known to the underlying runtime. This is
// intended for routing and reconciliation code that needs to discover existing
// sessions without directly depending on the provider.
func (s *SandboxService) ListSandboxes(ctx context.Context) ([]*sandbox.Sandbox, error) {
	return s.provider.List(ctx)
}

// GetSessionActivityIfRunning returns the sandbox's aggregate thread activity
// only when the sandbox is already running. It intentionally does not reconcile,
// start, health-probe, or record idle activity for stopped/unavailable sandboxes.
func (s *SandboxService) GetSessionActivityIfRunning(ctx context.Context, sessionID string) (*sandboxapi.SessionActivityResponse, error) {
	if _, err := s.store.GetSessionByID(ctx, sessionID); err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}

	sb, err := s.getSandbox(ctx, sessionID)
	if err != nil {
		if errors.Is(err, sandbox.ErrNotFound) {
			return nil, sandbox.ErrNotRunning
		}
		return nil, err
	}
	if sb.Status != sandbox.StatusRunning {
		return nil, sandbox.ErrNotRunning
	}

	return s.newAgentClient(ctx).GetSessionActivity(ctx, sessionID)
}

// StreamSessionActivityIfRunning connects to the sandbox activity stream only
// when the sandbox already exists and is running. It never starts a stopped
// sandbox.
func (s *SandboxService) StreamSessionActivityIfRunning(ctx context.Context, sessionID string) (<-chan *sandboxapi.SessionActivityResponse, error) {
	if _, err := s.store.GetSessionByID(ctx, sessionID); err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}

	sb, err := s.getSandbox(ctx, sessionID)
	if err != nil {
		if errors.Is(err, sandbox.ErrNotFound) {
			return nil, sandbox.ErrNotRunning
		}
		return nil, err
	}
	if sb.Status != sandbox.StatusRunning {
		return nil, sandbox.ErrNotRunning
	}

	return s.newAgentClient(ctx).StreamSessionActivity(ctx, sessionID)
}

// WatchSandboxEvents delegates sandbox state watching to the configured
// provider. Provider implementations are responsible for handling any dynamic
// backing providers they own.
func (s *SandboxService) WatchSandboxEvents(ctx context.Context) (<-chan sandbox.StateEvent, error) {
	if s == nil || s.provider == nil {
		return nil, fmt.Errorf("sandbox provider unavailable")
	}
	return s.provider.Watch(ctx)
}

func (s *SandboxService) handleSandboxEvent(ctx context.Context, event sandbox.StateEvent, handlers ...SandboxEventHandler) {
	session, err := s.store.GetSessionByID(ctx, event.SessionID)
	if err != nil {
		// Session doesn't exist - the sandbox is orphaned. This can happen if a
		// session was deleted but the sandbox wasn't cleaned up.
		log.Printf("[SandboxService] Session %s not found for sandbox event (status: %s)", event.SessionID, event.Status)
		return
	}
	for _, handler := range handlers {
		if handler != nil {
			handler(ctx, event, session)
		}
	}

	var newStatus string
	var errMsg *string
	switch event.Status {
	case sandbox.StatusRunning:
		if session.SandboxStatus != model.SessionStatusReady {
			newStatus = model.SessionStatusReady
		}
	case sandbox.StatusStopped:
		if session.SandboxStatus == model.SessionStatusReady ||
			session.SandboxStatus == model.SessionStatusInitializing ||
			session.SandboxStatus == model.SessionStatusCreatingSandbox {
			newStatus = model.SessionStatusStopped
		}
	case sandbox.StatusFailed:
		if session.SandboxStatus != model.SessionStatusError {
			newStatus = model.SessionStatusError
			if event.Error != "" {
				msg := "Sandbox failed: " + event.Error
				errMsg = &msg
			}
		}
	case sandbox.StatusRemoved:
		if session.SandboxStatus == model.SessionStatusReady ||
			session.SandboxStatus == model.SessionStatusInitializing ||
			session.SandboxStatus == model.SessionStatusCreatingSandbox {
			newStatus = model.SessionStatusStopped
			log.Printf("[SandboxService] Sandbox for session %s was removed, marking session as stopped", event.SessionID)
		}
	case sandbox.StatusCreated:
		// Sandbox created but not started; no session status update needed.
	default:
		log.Printf("[SandboxService] Unknown sandbox status: %s for session %s", event.Status, event.SessionID)
		return
	}

	if newStatus == "" {
		return
	}

	log.Printf("[SandboxService] Updating session %s status from %s to %s", event.SessionID, session.SandboxStatus, newStatus)
	if err := s.store.UpdateSessionStatus(ctx, event.SessionID, newStatus, errMsg); err != nil {
		log.Printf("[SandboxService] Failed to update session %s status: %v", event.SessionID, err)
		return
	}

	if s.eventBroker != nil {
		if err := s.eventBroker.PublishSessionUpdated(ctx, session.ProjectID, event.SessionID, newStatus, session.CommitStatus); err != nil {
			log.Printf("[SandboxService] Failed to publish session update event: %v", err)
		}
	}
}

func (s *SandboxService) newSessionClient(ctx context.Context, sessionID string) (*SessionClient, error) {
	return &SessionClient{
		sessionID:       sessionID,
		inner:           s.newAgentClient(ctx),
		sandboxSvc:      s,
		activityTracker: s.RecordActivity,
		connTracker:     s.connTracker,
	}, nil
}

func (s *SandboxService) newAgentClient(ctx context.Context) *SandboxAgentClient {
	gitName, gitEmail := s.getGitConfig(ctx)
	return NewSandboxAgentClient(s.provider, s.credentialFetcher, &SandboxAgentClientConfig{
		GitUserName:       gitName,
		GitUserEmail:      gitEmail,
		AcquireHTTPClient: s.acquireHTTPClient,
		GetSecret:         s.getSandboxSecret,
		GetAuthToken:      s.createSandboxAuthToken,
	})
}

func (s *SandboxService) loadProviderState(ctx context.Context, sessionID string) ([]byte, error) {
	encryptedData, err := s.store.GetSessionSandboxState(ctx, sessionID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	encryptor, err := encryption.NewEncryptor(s.cfg.EncryptionKey)
	if err != nil {
		return nil, err
	}
	return encryptor.Decrypt(encryptedData)
}

func (s *SandboxService) saveProviderState(ctx context.Context, sessionID string, state []byte) error {
	if len(state) == 0 {
		return s.store.DeleteSessionSandboxState(ctx, sessionID)
	}
	encryptor, err := encryption.NewEncryptor(s.cfg.EncryptionKey)
	if err != nil {
		return err
	}
	encryptedData, err := encryptor.Encrypt(state)
	if err != nil {
		return err
	}
	return s.store.SaveSessionSandboxState(ctx, sessionID, encryptedData)
}

func (s *SandboxService) saveProviderStateIfChanged(ctx context.Context, sessionID string, oldState, newState []byte) error {
	if bytes.Equal(oldState, newState) {
		return nil
	}
	return s.saveProviderState(ctx, sessionID, newState)
}

func (s *SandboxService) getSandbox(ctx context.Context, sessionID string) (*sandbox.Sandbox, error) {
	state, err := s.loadProviderState(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to load sandbox provider state: %w", err)
	}
	return s.provider.Get(ctx, state, sessionID)
}

func (s *SandboxService) acquireHTTPClient(ctx context.Context, sessionID string) (*sandbox.HTTPClientLease, error) {
	state, err := s.loadProviderState(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to load sandbox provider state: %w", err)
	}
	return sandbox.AcquireHTTPClient(ctx, s.provider, state, sessionID)
}

func (s *SandboxService) getSandboxSecret(ctx context.Context, sessionID string) (string, error) {
	state, err := s.loadProviderState(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("failed to load sandbox provider state: %w", err)
	}
	return s.provider.GetSecret(ctx, state, sessionID)
}

func (s *SandboxService) stopSandbox(ctx context.Context, sessionID string, timeout time.Duration) error {
	state, err := s.loadProviderState(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to load sandbox provider state: %w", err)
	}
	newState, err := s.provider.Stop(ctx, state, sessionID, timeout)
	if err != nil {
		return err
	}
	return s.saveProviderStateIfChanged(ctx, sessionID, state, newState)
}

func (s *SandboxService) removeSandbox(ctx context.Context, sessionID string, opts ...sandbox.RemoveOption) error {
	state, err := s.loadProviderState(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to load sandbox provider state: %w", err)
	}
	newState, err := s.provider.Remove(ctx, state, sessionID, opts...)
	if err != nil {
		return err
	}
	return s.saveProviderStateIfChanged(ctx, sessionID, state, newState)
}

// RemoveSessionSandbox removes a session sandbox and persists provider state.
func (s *SandboxService) RemoveSessionSandbox(ctx context.Context, sessionID string, opts ...sandbox.RemoveOption) error {
	return s.removeSandbox(ctx, sessionID, opts...)
}

// StartSessionSandbox starts a session sandbox and persists provider state.
func (s *SandboxService) StartSessionSandbox(ctx context.Context, sessionID string) error {
	state, err := s.loadProviderState(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to load sandbox provider state: %w", err)
	}
	newState, err := s.provider.Start(ctx, state, sessionID)
	if err != nil {
		return err
	}
	if err := s.saveProviderStateIfChanged(ctx, sessionID, state, newState); err != nil {
		return err
	}
	if err := s.waitForSandboxHealth(ctx, sessionID); err != nil {
		return fmt.Errorf("sandbox health check failed: %w", err)
	}
	return s.configureStartedSandbox(ctx, sessionID)
}

// PrepareSessionSandboxState prepares and persists provider state before
// sandbox creation.
func (s *SandboxService) PrepareSessionSandboxState(ctx context.Context, sessionID string, opts sandbox.CreateOptions) error {
	state, err := s.provider.PrepareState(ctx, sessionID, opts)
	if err != nil {
		return err
	}
	return s.saveProviderState(ctx, sessionID, state)
}

// CreateSessionSandbox creates a session sandbox and persists returned state.
func (s *SandboxService) CreateSessionSandbox(ctx context.Context, sessionID string, opts sandbox.CreateOptions) error {
	state, err := s.loadProviderState(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to load sandbox provider state: %w", err)
	}
	_, newState, err := s.provider.Create(ctx, state, sessionID, opts)
	if err != nil {
		return err
	}
	return s.saveProviderState(ctx, sessionID, newState)
}

// SandboxImage returns the provider image reference used for new sandboxes.
func (s *SandboxService) SandboxImage() string {
	return s.provider.Image()
}

// SandboxImageExists reports whether the provider image is already present.
func (s *SandboxService) SandboxImageExists(ctx context.Context) bool {
	return s.provider.ImageExists(ctx)
}

// WaitForSandboxImageOpsReady waits for the image operation provider to become
// ready when the underlying provider exposes a readiness hook.
func (s *SandboxService) WaitForSandboxImageOpsReady(ctx context.Context) error {
	return waitForSandboxImageOpsReady(ctx, providerForSandboxImageOps(s.provider))
}

// CurrentSandboxImageID returns the current provider image ID when available.
func (s *SandboxService) CurrentSandboxImageID(ctx context.Context) (string, error) {
	imageProvider := providerForSandboxImageOps(s.provider)
	imageIDProvider, ok := imageProvider.(sandbox.CurrentImageIDProvider)
	if !ok {
		return "", nil
	}
	return imageIDProvider.CurrentImageID(ctx)
}

// EnsureSandboxReady checks the session state from the database and ensures
// the sandbox is ready. Stopped and errored sessions trigger reconciliation,
// while create_failed sessions return their stored error until explicitly reset.
func (s *SandboxService) EnsureSandboxReady(ctx context.Context, sessionID string) error {
	return s.ensureSandboxReady(ctx, sessionID)
}

// ensureSandboxReady checks the session state from the database and ensures
// the sandbox is ready. Stopped and errored sessions trigger reconciliation,
// while create_failed sessions return their stored error until explicitly reset.
func (s *SandboxService) ensureSandboxReady(ctx context.Context, sessionID string) error {
	sess, err := s.store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("session not found: %w", err)
	}

	switch sess.SandboxStatus {
	case model.SessionStatusReady, legacySessionStatusRunning:
		return s.ensureSandboxRunningAndHealthy(ctx, sessionID, sess.SandboxStatus)
	case model.SessionStatusStopped, model.SessionStatusError:
		return s.ReconcileSandbox(ctx, sessionID)
	case model.SessionStatusCreateFailed:
		return sessionNotReadyError(sess)
	case model.SessionStatusInitializing, model.SessionStatusReinitializing,
		model.SessionStatusCloning, model.SessionStatusPullingImage, model.SessionStatusCreatingSandbox:
		if err := s.waitForSessionReady(ctx, sessionID); err != nil {
			return err
		}
		return s.ensureSandboxRunningAndHealthy(ctx, sessionID, model.SessionStatusReady)
	default:
		return sessionNotReadyError(sess)
	}
}

func sessionNotReadyError(sess *model.Session) error {
	if sess != nil && sess.ErrorMessage != nil && *sess.ErrorMessage != "" {
		return fmt.Errorf("session in %s state: %s", sess.SandboxStatus, *sess.ErrorMessage)
	}
	if sess != nil {
		return fmt.Errorf("session in %s state", sess.SandboxStatus)
	}
	return fmt.Errorf("session is not ready")
}

func (s *SandboxService) ensureSandboxRunningAndHealthy(ctx context.Context, sessionID string, sessionStatus string) error {
	// Session status looks good — verify the container is actually running.
	// This fast-path check avoids expensive reconciliation when everything is healthy.
	sb, err := s.getSandbox(ctx, sessionID)
	if errors.Is(err, sandbox.ErrNotFound) || (err == nil && sb.Status != sandbox.StatusRunning) {
		log.Printf("Session %s status is %s but container not running, reconciling", sessionID, sessionStatus)
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
}

// waitForSessionReady polls the session status until it reaches a terminal state.
func (s *SandboxService) waitForSessionReady(ctx context.Context, sessionID string) error {
	const pollInterval = 500 * time.Millisecond

	deadline := time.Now().Add(sessionReadyWaitTimeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		sess, err := s.store.GetSessionByID(ctx, sessionID)
		if err != nil {
			return fmt.Errorf("session not found: %w", err)
		}

		switch sess.SandboxStatus {
		case model.SessionStatusReady:
			return nil
		case model.SessionStatusError, model.SessionStatusCreateFailed, model.SessionStatusStopped:
			return sessionNotReadyError(sess)
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for session to be ready (status: %s)", sess.SandboxStatus)
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

	// If job orchestration dependencies are not available (e.g., in tests), fall
	// back to direct initialization.
	if s.jobEnqueuer == nil || s.eventBroker == nil {
		log.Printf("Job orchestration not fully available, falling back to direct initialization for session %s", sessionID)
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
// and configures either user-key auth or a legacy shared secret.
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

	// Recreated sandboxes clone the workspace's current HEAD by default.
	workspaceCommit := ""

	// Get workspace source for the WORKSPACE_SOURCE env var
	workspace, err := s.store.GetWorkspaceByID(ctx, session.WorkspaceID)
	if err != nil {
		return fmt.Errorf("failed to get workspace: %w", err)
	}

	trustKey, err := s.sandboxTrustKeyForUser(ctx, session.CreatedByUserID)
	if err != nil {
		return fmt.Errorf("failed to get sandbox trust key: %w", err)
	}
	sharedSecret := ""
	if trustKey == "" {
		sharedSecret = generateSandboxSecret(32)
	}

	// Create sandbox with session configuration
	// Note: The sandbox image is configured globally on the provider via SANDBOX_IMAGE env var
	opts := sandbox.CreateOptions{
		SharedSecret: sharedSecret,
		Env:          sandboxCreateEnv(sessionID, sharedSecret, trustKey),
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

	state, err := s.provider.PrepareState(ctx, sessionID, opts)
	if err != nil {
		return fmt.Errorf("failed to prepare sandbox state: %w", err)
	}
	if err := s.saveProviderState(ctx, sessionID, state); err != nil {
		return fmt.Errorf("failed to save sandbox provider state: %w", err)
	}

	// Create the sandbox
	_, state, err = s.provider.Create(ctx, state, sessionID, opts)
	if err != nil {
		return fmt.Errorf("failed to create sandbox: %w", err)
	}
	if err := s.saveProviderState(ctx, sessionID, state); err != nil {
		return fmt.Errorf("failed to save sandbox provider state: %w", err)
	}

	// Start the sandbox immediately
	state, err = s.provider.Start(ctx, state, sessionID)
	if err != nil {
		// Clean up on failure (don't need to remove volumes since this is a new sandbox)
		_ = s.removeSandbox(ctx, sessionID)
		return fmt.Errorf("failed to start sandbox: %w", err)
	}
	if err := s.saveProviderState(ctx, sessionID, state); err != nil {
		return fmt.Errorf("failed to save sandbox provider state: %w", err)
	}

	if err := s.waitForSandboxHealth(ctx, sessionID); err != nil {
		_ = s.removeSandbox(ctx, sessionID)
		return fmt.Errorf("sandbox health check failed: %w", err)
	}

	if err := s.configureSandboxAgent(ctx, sessionID, session, workspace, workspaceCommit, ""); err != nil {
		_ = s.removeSandbox(ctx, sessionID)
		return fmt.Errorf("failed to configure sandbox agent: %w", err)
	}

	return nil
}

func (s *SandboxService) configureSandboxAgent(ctx context.Context, sessionID string, session *model.Session, workspace *model.Workspace, workspaceCommit, workspaceTargetRef string) error {
	client := s.newAgentClient(ctx)
	needsConfigure, err := client.NeedsConfigure(ctx, sessionID)
	if err != nil {
		log.Printf("Sandbox agent health check before configure failed for session %s: %v", sessionID, err)
		needsConfigure = true
	}
	if !needsConfigure {
		return nil
	}
	creds, err := s.configureCredentials(ctx, sessionID)
	if err != nil {
		return err
	}
	gitUserName, gitUserEmail := s.getGitConfig(ctx)
	msg := "configuring agent runtime"
	if err := s.store.UpdateSessionSandboxProgress(ctx, sessionID, model.SessionStatusInitializing, &msg, nil); err != nil {
		log.Printf("Warning: failed to update session progress for %s: %v", sessionID, err)
	}
	if s.eventBroker != nil {
		if err := s.eventBroker.PublishSessionUpdated(ctx, session.ProjectID, sessionID, model.SessionStatusInitializing, "", msg); err != nil {
			log.Printf("Warning: failed to publish session update event: %v", err)
		}
	}
	return client.Configure(ctx, sessionID, sandboxConfigureRequest{
		WorkspaceOrigin:        "/.workspace",
		WorkspaceSource:        workspace.Path,
		WorkspaceSourceType:    workspace.SourceType,
		WorkspaceCommit:        workspaceCommit,
		WorkspaceTargetRef:     workspaceTargetRef,
		SessionID:              sessionID,
		MCPOAuthRedirectBase:   s.cfg.MCPOAuthRedirectBase,
		DiscobotServerURL:      s.cfg.AgentServerURL,
		DiscobotProjectID:      session.ProjectID,
		EnableGitControlSocket: sandboxGitControlSocketEnabled(workspace, valueOrEmpty(session.WorkspacePath)),
		Credentials:            creds,
		GitUserName:            gitUserName,
		GitUserEmail:           gitUserEmail,
	}, func(event sandboxConfigureEvent) {
		if event.Message == "" {
			return
		}
		if err := s.store.UpdateSessionSandboxProgress(ctx, sessionID, model.SessionStatusInitializing, &event.Message, nil); err != nil {
			log.Printf("Warning: failed to update session progress for %s: %v", sessionID, err)
		}
		if s.eventBroker != nil {
			if err := s.eventBroker.PublishSessionUpdated(ctx, session.ProjectID, sessionID, model.SessionStatusInitializing, "", event.Message); err != nil {
				log.Printf("Warning: failed to publish session update event: %v", err)
			}
		}
	})
}

func (s *SandboxService) configureStartedSandbox(ctx context.Context, sessionID string) error {
	session, err := s.store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}
	workspace, err := s.store.GetWorkspaceByID(ctx, session.WorkspaceID)
	if err != nil {
		return fmt.Errorf("failed to get workspace: %w", err)
	}
	return s.configureSandboxAgent(ctx, sessionID, session, workspace, "", valueOrEmpty(session.TargetRef))
}

func (s *SandboxService) configureCredentials(ctx context.Context, sessionID string) ([]CredentialEnvVar, error) {
	if s.credentialFetcher == nil {
		return nil, nil
	}
	creds, err := s.credentialFetcher(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch credentials for sandbox configuration: %w", err)
	}
	return creds, nil
}

func sandboxGitControlSocketEnabled(workspace *model.Workspace, workspacePath string) bool {
	if workspace == nil || workspacePath == "" {
		return false
	}
	return isLocalWorkspaceSourceType(workspace.SourceType) && !git.IsGitURL(workspace.Path)
}

func sandboxCreateEnv(sessionID, sharedSecret, trustKey string) map[string]string {
	env := map[string]string{
		"DISCOBOT_SESSION_ID":      sessionID,
		"DISCOBOT_WAIT_FOR_CONFIG": "true",
	}
	if sharedSecret != "" {
		env["DISCOBOT_SECRET"] = hashSandboxSecret(sharedSecret)
	}
	if trustKey != "" {
		env["DISCOBOT_TRUST_KEY"] = trustKey
	}
	// Spike: opt-in path to drive the official Claude Code CLI over ACP using a
	// Claude subscription. Both vars are passed through from the server's own
	// environment, flow to systemd PID 1, and reach discobot-agent-api (and the
	// claude-code-acp child it spawns) via /run/discobot/agent-env. Leaving the
	// Anthropic credential unset keeps the proxy from injecting an x-api-key
	// header, so Claude Code authenticates with its own subscription token.
	for _, key := range []string{"DISCOBOT_AGENT_BACKEND", "CLAUDE_CODE_OAUTH_TOKEN"} {
		if value := os.Getenv(key); value != "" {
			env[key] = value
		}
	}
	return env
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func hashSandboxSecret(secret string) string {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		salt = make([]byte, 16)
	}
	h := sha256.New()
	h.Write(salt)
	h.Write([]byte(secret))
	return hex.EncodeToString(salt) + ":" + hex.EncodeToString(h.Sum(nil))
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
	return s.getSandbox(ctx, sessionID)
}

// Attach creates an interactive PTY session to the sandbox.
// If user is empty, the container's default user is used.
// env contains additional environment variables to set in the session.
func (s *SandboxService) Attach(ctx context.Context, sessionID string, rows, cols int, user, workDir string, env map[string]string) (sandbox.PTY, error) {
	if err := s.ensureSandboxReady(ctx, sessionID); err != nil {
		return nil, err
	}
	return s.newAgentClient(ctx).Attach(ctx, sessionID, rows, cols, user, workDir, env)
}

// AttachTerminal creates or reuses a persistent terminal PTY for a session.
func (s *SandboxService) AttachTerminal(ctx context.Context, sessionID string, rows, cols int, user, workDir, reuseKey string, env map[string]string) (sandbox.PTY, error) {
	if err := s.ensureSandboxReady(ctx, sessionID); err != nil {
		return nil, err
	}
	return s.newAgentClient(ctx).AttachTerminal(ctx, sessionID, rows, cols, user, workDir, reuseKey, env)
}

// ExecStream runs a command with bidirectional streaming I/O in the session's sandbox.
func (s *SandboxService) ExecStream(ctx context.Context, sessionID string, cmd []string, opts sandbox.ExecStreamOptions) (sandbox.Stream, error) {
	if err := s.ensureSandboxReady(ctx, sessionID); err != nil {
		return nil, err
	}
	return s.newAgentClient(ctx).ExecStream(ctx, sessionID, cmd, opts)
}

// StopForSession stops the sandbox for a session.
func (s *SandboxService) StopForSession(ctx context.Context, sessionID string) error {
	return s.stopSandbox(ctx, sessionID, 10*time.Second)
}

// RemoveForSession removes a session sandbox and its volumes using persisted
// provider state when available.
func (s *SandboxService) RemoveForSession(ctx context.Context, sessionID string) error {
	return s.removeSandbox(ctx, sessionID, sandbox.RemoveVolumes())
}

// RemoveForDeletedSession removes a session sandbox after its database record
// has been deleted. Missing sandboxes are treated as already removed, and any
// retained provider state is cleared.
func (s *SandboxService) RemoveForDeletedSession(ctx context.Context, sessionID string) error {
	err := s.RemoveForSession(ctx, sessionID)
	if err == nil {
		return nil
	}
	if errors.Is(err, sandbox.ErrNotFound) {
		if deleteErr := s.store.DeleteSessionSandboxState(ctx, sessionID); deleteErr != nil {
			log.Printf("Failed to delete missing sandbox state for session %s: %v", sessionID, deleteErr)
		}
		return nil
	}
	return err
}

// probeSandboxHealth does a fast, single-attempt HTTP health check against the
// sandbox's agent-api. It accepts the bootstrap "not configured" response as a
// live agent so initialization can configure it instead of recreating it. It
// uses a short timeout (2s) to quickly detect dead or dying containers without
// blocking for the full retry backoff (~14s).
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

	statusCode, err := s.checkSandboxStartupHealth(probeCtx, sessionID)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	if statusCode >= 500 {
		return fmt.Errorf("health check returned %d", statusCode)
	}

	s.recordSandboxHealthy(sessionID)
	return nil
}

// waitForSandboxHealth waits for a newly started sandbox to begin responding to
// health checks. Unlike probeSandboxHealth, it keeps retrying transient
// connection and 5xx failures until the startup timeout expires.
func (s *SandboxService) waitForSandboxHealth(ctx context.Context, sessionID string) error {
	s.healthCacheMu.RLock()
	lastHealthy, ok := s.healthCacheMap[sessionID]
	s.healthCacheMu.RUnlock()
	if ok && time.Since(lastHealthy) < healthCacheTTL {
		return nil
	}

	waitCtx, cancel := context.WithTimeout(ctx, sandboxHealthWaitTimeout)
	defer cancel()

	delay := retryInitialDelay
	for {
		statusCode, err := s.checkSandboxStartupHealth(waitCtx, sessionID)
		if err == nil && !isRetryableStatus(statusCode) {
			s.recordSandboxHealthy(sessionID)
			return nil
		}

		if err != nil && !isRetryableError(err) {
			return fmt.Errorf("health check failed: %w", err)
		}

		select {
		case <-waitCtx.Done():
			if err != nil {
				return fmt.Errorf("health check failed: %w", err)
			}
			return fmt.Errorf("health check failed: sandbox returned retryable status %d before startup timeout", statusCode)
		case <-time.After(delay):
		}

		delay = min(time.Duration(float64(delay)*retryMultiplier), retryMaxDelay)
	}
}

func (s *SandboxService) checkSandboxStartupHealth(ctx context.Context, sessionID string) (int, error) {
	return s.checkSandboxHealthResponse(ctx, sessionID, true)
}

func (s *SandboxService) checkSandboxHealthResponse(ctx context.Context, sessionID string, acceptAgentNotConfigured bool) (int, error) {
	httpClientLease, err := s.acquireHTTPClient(ctx, sessionID)
	if err != nil {
		return 0, fmt.Errorf("failed to get HTTP client: %w", err)
	}
	defer httpClientLease.Release()

	req, err := http.NewRequestWithContext(ctx, "GET", "http://sandbox/health", nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := httpClientLease.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if acceptAgentNotConfigured {
		var body struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body)
		if body.Code == "AGENT_NOT_CONFIGURED" {
			return http.StatusOK, nil
		}
	}

	return resp.StatusCode, nil
}

func (s *SandboxService) recordSandboxHealthy(sessionID string) {
	s.healthCacheMu.Lock()
	s.healthCacheMap[sessionID] = time.Now()
	s.healthCacheMu.Unlock()
}

// DestroyForSession removes the sandbox when a session is deleted.
// This is deprecated - use SessionService.PerformDeletion instead which handles volumes.
func (s *SandboxService) DestroyForSession(ctx context.Context, sessionID string) error {
	err := s.removeSandbox(ctx, sessionID)
	if errors.Is(err, sandbox.ErrNotFound) {
		// Already removed, not an error
		return nil
	}
	return err
}

type sandboxImageOpsReadyWaiter interface {
	WaitForReady(ctx context.Context) error
}

func providerForSandboxImageOps(provider sandbox.Provider) sandbox.Provider {
	if defaultProviderGetter, ok := provider.(interface{ DefaultProvider() sandbox.Provider }); ok {
		if defaultProvider := defaultProviderGetter.DefaultProvider(); defaultProvider != nil {
			return defaultProvider
		}
	}
	return provider
}

func waitForSandboxImageOpsReady(ctx context.Context, provider sandbox.Provider) error {
	waiter, ok := provider.(sandboxImageOpsReadyWaiter)
	if !ok {
		return nil
	}
	return waiter.WaitForReady(ctx)
}

func sandboxClonesGitWorkspace(provider sandbox.Provider) bool {
	_, isLocal := provider.(*sandboxlocal.Provider)
	return !isLocal
}

func sandboxUsesExpectedImage(sb *sandbox.Sandbox, expectedImage, expectedImageID string) bool {
	if sb == nil {
		return false
	}
	if expectedImageID != "" {
		if sandboxImageID := sb.Metadata[sandbox.MetadataImageID]; sandboxImageID != "" {
			return sandboxImageID == expectedImageID
		}
	}
	return sb.Image == expectedImage
}

// ReconcileSandboxes checks all existing sandboxes and recreates any that
// are using an outdated image. This should be called on server startup.
func (s *SandboxService) ReconcileSandboxes(ctx context.Context) error {
	expectedImage := s.provider.Image()
	if expectedImage == "" {
		log.Printf("No sandbox image configured, skipping reconciliation")
		return nil
	}

	providerForImageOps := providerForSandboxImageOps(s.provider)
	if err := waitForSandboxImageOpsReady(ctx, providerForImageOps); err != nil {
		return fmt.Errorf("failed to wait for sandbox provider readiness: %w", err)
	}

	sandboxes, err := s.provider.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list sandboxes: %w", err)
	}

	log.Printf("Reconciling %d sandboxes (expected image: %s)", len(sandboxes), expectedImage)

	expectedImageID := ""
	if imageIDProvider, ok := providerForImageOps.(sandbox.CurrentImageIDProvider); ok {
		imageID, err := imageIDProvider.CurrentImageID(ctx)
		if err != nil {
			log.Printf("Warning: Failed to resolve current sandbox image ID for %s: %v", expectedImage, err)
		} else {
			expectedImageID = imageID
		}
	}

	for _, sb := range sandboxes {
		if expectedImageID != "" && sb.Metadata[sandbox.MetadataImageID] == "" {
			log.Printf("Sandbox for session %s is missing image ID metadata, falling back to image reference comparison", sb.SessionID)
		}
		if sandboxUsesExpectedImage(sb, expectedImage, expectedImageID) {
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
			if err := s.removeSandbox(ctx, sb.SessionID); err != nil {
				log.Printf("Failed to remove orphaned sandbox for session %s: %v", sb.SessionID, err)
			}
			continue
		}

		// Remove the old sandbox (preserve volume for image update)
		if err := s.removeSandbox(ctx, sb.SessionID); err != nil {
			log.Printf("Failed to remove sandbox for session %s: %v", sb.SessionID, err)
			continue
		}

		if sb.Status == sandbox.StatusStopped || sb.Status == sandbox.StatusCreated {
			log.Printf("Removed outdated inactive sandbox for session %s; it will be recreated on demand", sb.SessionID)
			continue
		}

		// Recreate the sandbox with the correct image via job system only when the
		// sandbox had been active. Stopped sandboxes stay stopped after image updates.
		// This ensures proper serialization with any concurrent user operations.
		if err := s.ReconcileSandbox(ctx, sb.SessionID); err != nil {
			log.Printf("Failed to recreate sandbox for session %s: %v", sb.SessionID, err)
			errorMsg := fmt.Sprintf("sandbox image upgrade failed: %v", err)
			if statusErr := s.store.UpdateSessionStatus(ctx, sb.SessionID, model.SessionStatusStopped, &errorMsg); statusErr != nil {
				log.Printf("Failed to mark session %s stopped after sandbox image upgrade failure: %v", sb.SessionID, statusErr)
			}
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
		sb, err := s.getSandbox(ctx, session.ID)
		if errors.Is(err, sandbox.ErrNotFound) {
			// Sandbox doesn't exist - mark as stopped, will be recreated on demand
			log.Printf("Session %s (status: %s) has no sandbox, marking as stopped", session.ID, session.SandboxStatus)
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
			if session.SandboxStatus != model.SessionStatusReady {
				log.Printf("Session %s was in %s state but sandbox is running, updating to ready", session.ID, session.SandboxStatus)
				if err := s.store.UpdateSessionStatus(ctx, session.ID, model.SessionStatusReady, nil); err != nil {
					log.Printf("Failed to update session %s status: %v", session.ID, err)
				}
			}
			continue
		}

		log.Printf("Session %s (status: %s) sandbox status: %s", session.ID, session.SandboxStatus, sb.Status)
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
	sb, err := s.getSandbox(ctx, sessionID)
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
	secret, err := s.getSandboxSecret(ctx, sessionID)
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
