package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/obot-platform/discobot/server/internal/events"
	"github.com/obot-platform/discobot/server/internal/git"
	"github.com/obot-platform/discobot/server/internal/jobs"
	"github.com/obot-platform/discobot/server/internal/model"
	"github.com/obot-platform/discobot/server/internal/sandbox"
	"github.com/obot-platform/discobot/server/internal/store"
)

// SessionIDMaxLength is the maximum allowed length for a session ID.
const SessionIDMaxLength = 65

// Commit operation constants for API/session state.
const (
	CommitOperationCommit = model.CommitOperationCommit
	CommitOperationRebase = model.CommitOperationRebase
)

// ErrSessionOperationInProgress indicates a commit/rebase operation is already running.
var ErrSessionOperationInProgress = errors.New("session operation already in progress")

const (
	legacySessionStatusRunning    = "running"
	sessionDeletionRetentionDelay = 24 * time.Hour
)

// sessionIDRegex matches valid session IDs (alphanumeric and hyphens only).
var sessionIDRegex = regexp.MustCompile(`^[a-zA-Z0-9-]+$`)

// ValidateSessionID validates that a session ID meets format requirements:
// - Only alphanumeric characters (a-z, A-Z, 0-9) and hyphens (-) are allowed
// - Maximum length is 65 characters
func ValidateSessionID(sessionID string) error {
	if sessionID == "" {
		return errors.New("session ID is required")
	}
	if len(sessionID) > SessionIDMaxLength {
		return fmt.Errorf("session ID exceeds maximum length of %d characters", SessionIDMaxLength)
	}
	if !sessionIDRegex.MatchString(sessionID) {
		return errors.New("session ID must contain only alphanumeric characters and hyphens")
	}
	return nil
}

func normalizeSessionStatus(status string) string {
	if status == legacySessionStatusRunning {
		return model.SessionStatusReady
	}
	return status
}

// Session represents a chat session (for API responses)
type Session struct {
	ID              string     `json:"id"`
	ProjectID       string     `json:"projectId"`
	Name            string     `json:"name"`
	DisplayName     string     `json:"displayName,omitempty"`
	Description     string     `json:"description"`
	CreatedAt       string     `json:"createdAt"`
	Timestamp       string     `json:"timestamp"`
	Status          string     `json:"status"`
	CommitStatus    string     `json:"commitStatus,omitempty"`
	CommitOperation string     `json:"commitOperation,omitempty"`
	CommitError     string     `json:"commitError,omitempty"`
	BaseCommit      string     `json:"baseCommit,omitempty"`
	AppliedCommit   string     `json:"appliedCommit,omitempty"`
	ErrorMessage    string     `json:"errorMessage,omitempty"`
	Files           []FileNode `json:"files"`
	WorkspaceID     string     `json:"workspaceId,omitempty"`
	WorkspacePath   string     `json:"workspacePath,omitempty"`
	WorkspaceCommit string     `json:"workspaceCommit,omitempty"`
	ActiveEnvSetIDs []string   `json:"activeEnvSetIds,omitempty"`
}

// FileNode represents a file in a session
type FileNode struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Type            string     `json:"type"`
	Children        []FileNode `json:"children,omitempty"`
	Content         string     `json:"content,omitempty"`
	OriginalContent string     `json:"originalContent,omitempty"`
	Changed         bool       `json:"changed,omitempty"`
}

// SessionService handles session operations
type SessionService struct {
	store           *store.Store
	gitService      *GitService
	sandboxProvider sandbox.Provider
	sandboxService  *SandboxService
	eventBroker     *events.Broker
	jobEnqueuer     JobEnqueuer
}

// NewSessionService creates a new session service
func NewSessionService(s *store.Store, gitSvc *GitService, sandboxProv sandbox.Provider, sandboxService *SandboxService, eventBroker *events.Broker, jobEnqueuer JobEnqueuer) *SessionService {
	return &SessionService{
		store:           s,
		gitService:      gitSvc,
		sandboxProvider: sandboxProv,
		sandboxService:  sandboxService,
		eventBroker:     eventBroker,
		jobEnqueuer:     jobEnqueuer,
	}
}

// ListSessionsByProject returns all sessions for a project.
func (s *SessionService) ListSessionsByProject(ctx context.Context, projectID string) ([]*Session, error) {
	dbSessions, err := s.store.ListSessionsByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}

	sessions := make([]*Session, len(dbSessions))
	for i, sess := range dbSessions {
		s.syncSessionNameFromPrimaryThread(ctx, sess)
		sessions[i] = s.mapSession(sess)
	}
	return sessions, nil
}

// ListSessionsByWorkspace returns all sessions for a workspace.
// Deprecated: prefer ListSessionsByProject for project-scoped session listing.
func (s *SessionService) ListSessionsByWorkspace(ctx context.Context, workspaceID string) ([]*Session, error) {
	dbSessions, err := s.store.ListSessionsByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}

	sessions := make([]*Session, len(dbSessions))
	for i, sess := range dbSessions {
		s.syncSessionNameFromPrimaryThread(ctx, sess)
		sessions[i] = s.mapSession(sess)
	}
	return sessions, nil
}

// GetSession returns a session by ID
func (s *SessionService) GetSession(ctx context.Context, sessionID string) (*Session, error) {
	sess, err := s.store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	s.syncSessionNameFromPrimaryThread(ctx, sess)

	return s.mapSession(sess), nil
}

func (s *SessionService) syncSessionNameFromPrimaryThread(ctx context.Context, sess *model.Session) {
	if sess == nil || strings.TrimSpace(sess.Name) != "" || s.sandboxService == nil {
		return
	}

	client, err := s.sandboxService.GetClient(ctx, sess.ID)
	if err != nil {
		return
	}

	thread, err := client.GetThread(ctx, sess.ID)
	if err != nil {
		return
	}

	name := strings.TrimSpace(thread.Name)
	if name == "" {
		return
	}

	if _, err := s.UpdateSession(ctx, sess.ID, name, nil, ""); err != nil {
		log.Printf("Warning: failed to sync session name from thread for %s: %v", sess.ID, err)
		return
	}

	sess.Name = name
}

// CreateSession creates a new session with initializing status and auto-generated ID.
// If initialMessage is provided, it creates the first user message in the session.
func (s *SessionService) CreateSession(ctx context.Context, projectID, workspaceID, name, initialMessage string) (*Session, error) {
	sess := &model.Session{
		ProjectID:   projectID,
		WorkspaceID: workspaceID,
		Name:        name,
		Description: nil,
		Status:      model.SessionStatusInitializing,
	}
	if err := s.store.CreateSession(ctx, sess); err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	// Create the initial user message if provided
	if initialMessage != "" {
		msg := &model.Message{
			SessionID: sess.ID,
			Role:      "user",
			Parts:     model.NewTextParts(initialMessage),
		}
		if err := s.store.CreateMessage(ctx, msg); err != nil {
			// Log the error but don't fail session creation
			log.Printf("Warning: failed to create initial message for session %s: %v", sess.ID, err)
		}
	}

	return s.mapSession(sess), nil
}

// CreateSessionWithID creates a new session with the provided client ID.
func (s *SessionService) CreateSessionWithID(ctx context.Context, sessionID, projectID, workspaceID, name string) (*Session, error) {
	sess := &model.Session{
		ID:          sessionID, // Use client-provided ID
		ProjectID:   projectID,
		WorkspaceID: workspaceID,
		Name:        name,
		Description: nil,
		Status:      model.SessionStatusInitializing,
	}
	if err := s.store.CreateSession(ctx, sess); err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	return s.mapSession(sess), nil
}

// UpdateStatus updates the session status and optional error message, and publishes an SSE event.
func (s *SessionService) UpdateStatus(ctx context.Context, projectID, sessionID, status string, errorMsg *string) (*Session, error) {
	status = normalizeSessionStatus(status)

	// Use targeted column update to avoid overwriting concurrent changes to other fields
	if err := s.store.UpdateSessionStatus(ctx, sessionID, status, errorMsg); err != nil {
		return nil, fmt.Errorf("failed to update session status: %w", err)
	}

	// Re-read the session to return the full updated state
	sess, err := s.store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	// Always publish SSE event for status changes
	if s.eventBroker != nil {
		if err := s.eventBroker.PublishSessionUpdated(ctx, projectID, sessionID, status, ""); err != nil {
			log.Printf("Failed to publish session update event: %v", err)
		}
	}

	return s.mapSession(sess), nil
}

// UpdateSession updates a session and publishes an SSE event.
func (s *SessionService) UpdateSession(ctx context.Context, sessionID, name string, displayName *string, status string) (*Session, error) {
	sess, err := s.store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	if name != "" {
		sess.Name = name
	}
	if displayName != nil {
		// Treat empty string as null (clear the displayName)
		if *displayName == "" {
			sess.DisplayName = nil
		} else {
			sess.DisplayName = displayName
		}
	}
	if status != "" {
		sess.Status = normalizeSessionStatus(status)
	}
	if err := s.store.UpdateSession(ctx, sess); err != nil {
		return nil, fmt.Errorf("failed to update session: %w", err)
	}

	if s.eventBroker != nil {
		if err := s.eventBroker.PublishSessionUpdated(ctx, sess.ProjectID, sessionID, sess.Status, sess.CommitStatus); err != nil {
			log.Printf("Failed to publish session update event: %v", err)
		}
	}

	return s.mapSession(sess), nil
}

// ClearCompletedCommitStatus resets the commit status for a session that is
// moving back into active editing/chat work.
func (s *SessionService) ClearCompletedCommitStatus(ctx context.Context, projectID, sessionID string) error {
	sess, err := s.store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get session: %w", err)
	}
	if sess.CommitStatus != model.CommitStatusCompleted {
		return nil
	}

	sess.CommitStatus = model.CommitStatusNone
	sess.CommitOperation = nil
	sess.CommitError = nil
	if err := s.store.UpdateSession(ctx, sess); err != nil {
		return fmt.Errorf("failed to clear commit status: %w", err)
	}

	s.publishCommitStatusChanged(ctx, projectID, sessionID, model.CommitStatusNone)
	return nil
}

// DeleteSession initiates async deletion of a session.
// It sets the session status to "removing", emits an SSE event, and enqueues a deletion job.
func (s *SessionService) DeleteSession(ctx context.Context, projectID, sessionID string, jobQueue JobEnqueuer) error {
	// Get session to verify it exists
	sess, err := s.store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("session not found: %w", err)
	}

	// Don't allow deletion of sessions already being removed
	if sess.Status == model.SessionStatusRemoving {
		return nil // Already being deleted
	}

	// Update status to "removing"
	sess.Status = model.SessionStatusRemoving
	if err := s.store.UpdateSession(ctx, sess); err != nil {
		return fmt.Errorf("failed to update session status: %w", err)
	}

	// Emit SSE event
	if s.eventBroker != nil {
		if err := s.eventBroker.PublishSessionUpdated(ctx, projectID, sessionID, model.SessionStatusRemoving, sess.CommitStatus); err != nil {
			log.Printf("Failed to publish session removing event: %v", err)
		}
	}

	// Enqueue deletion job
	if err := jobQueue.Enqueue(ctx, jobs.SessionDeletePayload{ProjectID: projectID, SessionID: sessionID}); err != nil {
		// If job enqueueing fails, log but don't fail - the session is marked as removing
		// and can be cleaned up later by reconciliation
		log.Printf("Failed to enqueue session delete job for %s: %v", sessionID, err)
	}

	return nil
}

// CommitSession initiates async commit of a session.
func (s *SessionService) CommitSession(ctx context.Context, projectID, sessionID string, jobQueue JobEnqueuer) error {
	// Get session to verify it exists and get workspace ID
	sess, err := s.store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("session not found: %w", err)
	}

	if err := s.markSessionOperationPending(ctx, projectID, sess, CommitOperationCommit); err != nil {
		return err
	}

	if err = jobQueue.Enqueue(ctx, jobs.SessionCommitPayload{ProjectID: projectID, SessionID: sessionID, WorkspaceID: sess.WorkspaceID}); err != nil {
		return fmt.Errorf("failed to enqueue commit job: %w", err)
	}

	return nil
}

// RebaseSession initiates async rebase of a session.
func (s *SessionService) RebaseSession(ctx context.Context, projectID, sessionID string, jobQueue JobEnqueuer) error {
	// Get session to verify it exists and get workspace ID
	sess, err := s.store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("session not found: %w", err)
	}

	if err := s.markSessionOperationPending(ctx, projectID, sess, CommitOperationRebase); err != nil {
		return err
	}

	if err = jobQueue.Enqueue(ctx, jobs.SessionRebasePayload{ProjectID: projectID, SessionID: sessionID, WorkspaceID: sess.WorkspaceID}); err != nil {
		return fmt.Errorf("failed to enqueue rebase job: %w", err)
	}

	return nil
}

func (s *SessionService) markSessionOperationPending(ctx context.Context, projectID string, sess *model.Session, operation string) error {
	if sess.CommitStatus == model.CommitStatusPending || sess.CommitStatus == model.CommitStatusCommitting {
		return ErrSessionOperationInProgress
	}

	sess.CommitStatus = model.CommitStatusPending
	sess.CommitOperation = ptrString(operation)
	sess.CommitError = nil
	if operation == CommitOperationRebase {
		sess.AppliedCommit = nil
	}
	if err := s.store.UpdateSession(ctx, sess); err != nil {
		return fmt.Errorf("failed to update session commit status: %w", err)
	}
	s.publishCommitStatusChanged(ctx, projectID, sess.ID, model.CommitStatusPending)

	return nil
}

// publishCommitStatusChanged publishes an SSE event for commit status changes.
func (s *SessionService) publishCommitStatusChanged(ctx context.Context, projectID, sessionID, commitStatus string) {
	if s.eventBroker != nil {
		// Send empty string for session status since only commit status changed
		if err := s.eventBroker.PublishSessionUpdated(ctx, projectID, sessionID, "", commitStatus); err != nil {
			log.Printf("Failed to publish session commit status event: %v", err)
		}
	}
}

// ReconcileCommitStates checks sessions stuck in pending/committing commit states
// and re-enqueues commit jobs if needed. This should be called on server startup.
func (s *SessionService) ReconcileCommitStates(ctx context.Context) error {
	// Query sessions with stuck commit states
	statuses := []string{model.CommitStatusPending, model.CommitStatusCommitting}
	sessions, err := s.store.ListSessionsByCommitStatuses(ctx, statuses)
	if err != nil {
		return fmt.Errorf("failed to list sessions with commit states: %w", err)
	}

	if len(sessions) == 0 {
		log.Println("No sessions with stuck commit states found")
		return nil
	}

	log.Printf("Reconciling %d sessions with stuck commit states", len(sessions))

	// For each session, check if an active job for the same serialized resource already exists.
	var enqueuedCount int
	for _, sess := range sessions {
		operation := CommitOperationCommit
		if sess.CommitOperation != nil && *sess.CommitOperation != "" {
			operation = *sess.CommitOperation
		}

		resourceType := jobs.ResourceTypeWorkspace
		resourceID := sess.WorkspaceID
		if operation == CommitOperationRebase {
			resourceType = jobs.ResourceTypeSession
			resourceID = sess.ID
		}

		hasJob, err := s.store.HasActiveJobForResource(ctx, resourceType, resourceID)
		if err != nil {
			log.Printf("Failed to check job for session %s: %v", sess.ID, err)
			continue
		}

		if hasJob {
			log.Printf("Session %s (commit_status: %s) already has active job, skipping", sess.ID, sess.CommitStatus)
			continue
		}

		var payload jobs.JobPayload
		switch operation {
		case CommitOperationRebase:
			log.Printf("Re-enqueueing rebase job for session %s (commit_status: %s)", sess.ID, sess.CommitStatus)
			payload = jobs.SessionRebasePayload{
				ProjectID:   sess.ProjectID,
				SessionID:   sess.ID,
				WorkspaceID: sess.WorkspaceID,
			}
		default:
			log.Printf("Re-enqueueing commit job for session %s (commit_status: %s)", sess.ID, sess.CommitStatus)
			payload = jobs.SessionCommitPayload{
				ProjectID:   sess.ProjectID,
				SessionID:   sess.ID,
				WorkspaceID: sess.WorkspaceID,
			}
		}

		if s.jobEnqueuer != nil {
			if err := s.jobEnqueuer.Enqueue(ctx, payload); err != nil {
				// Log but continue - this session remains stuck but others proceed
				log.Printf("Failed to enqueue %s job for session %s: %v", operation, sess.ID, err)
				continue
			}
			enqueuedCount++
		} else {
			log.Printf("Job enqueuer not available for session %s, skipping", sess.ID)
		}
	}

	log.Printf("Reconciled commit states: %d jobs re-enqueued", enqueuedCount)
	return nil
}

// PerformDeletion performs the actual session deletion work.
// This is called by the SessionDeleteExecutor job handler.
func (s *SessionService) PerformDeletion(ctx context.Context, projectID, sessionID string) error {
	// Step 1: Stop the sandbox immediately so the session is inactive during the
	// recovery window, but keep the sandbox itself for deferred deletion.
	if s.sandboxProvider != nil {
		if err := s.sandboxProvider.Stop(ctx, sessionID, 10*time.Second); err != nil {
			if !errors.Is(err, sandbox.ErrNotFound) && !errors.Is(err, sandbox.ErrNotRunning) {
				return fmt.Errorf("failed to stop sandbox: %w", err)
			}
		}
	}

	// Step 2: Delete from database (messages, terminal history, session)
	if err := s.store.DeleteSession(ctx, sessionID); err != nil {
		return fmt.Errorf("failed to delete session from database: %w", err)
	}

	// Step 3: Schedule deferred sandbox cleanup.
	if s.jobEnqueuer != nil && s.sandboxProvider != nil {
		if err := s.jobEnqueuer.Enqueue(ctx, jobs.SessionSandboxDeletePayload{
			SessionID: sessionID,
			DeleteAt:  time.Now().Add(sessionDeletionRetentionDelay),
		}); err != nil {
			log.Printf("Failed to enqueue deferred session sandbox delete job for %s: %v", sessionID, err)
		}
	}

	// Step 4: Emit "removed" event to notify clients
	if s.eventBroker != nil {
		if err := s.eventBroker.PublishSessionUpdated(ctx, projectID, sessionID, model.SessionStatusRemoved, ""); err != nil {
			log.Printf("Failed to publish session removed event: %v", err)
		}
	}

	log.Printf("Session %s deleted successfully", sessionID)
	return nil
}

// PerformDeferredSandboxDeletion removes a retained sandbox after the
// session has been deleted long enough to age out of the recovery window.
func (s *SessionService) PerformDeferredSandboxDeletion(ctx context.Context, sessionID string) error {
	if s.sandboxProvider == nil {
		return nil
	}

	if _, err := s.store.GetSessionByID(ctx, sessionID); err == nil {
		log.Printf("Skipping deferred sandbox cleanup for session %s because the session exists again", sessionID)
		return nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("failed to check session before deferred sandbox cleanup: %w", err)
	}

	if err := s.sandboxProvider.Remove(ctx, sessionID, sandbox.RemoveVolumes()); err != nil && !errors.Is(err, sandbox.ErrNotFound) {
		return fmt.Errorf("failed to remove deferred sandbox: %w", err)
	}

	log.Printf("Deferred sandbox cleanup completed for session %s", sessionID)
	return nil
}

// mapSession maps a model Session to a service Session
func (s *SessionService) mapSession(sess *model.Session) *Session {
	description := ""
	if sess.Description != nil {
		description = *sess.Description
	}

	displayName := ""
	if sess.DisplayName != nil {
		displayName = *sess.DisplayName
	}

	errorMessage := ""
	if sess.ErrorMessage != nil {
		errorMessage = *sess.ErrorMessage
	}

	commitError := ""
	if sess.CommitError != nil {
		commitError = *sess.CommitError
	}

	commitOperation := ""
	if sess.CommitOperation != nil {
		commitOperation = *sess.CommitOperation
	}

	baseCommit := ""
	if sess.BaseCommit != nil {
		baseCommit = *sess.BaseCommit
	}

	appliedCommit := ""
	if sess.AppliedCommit != nil {
		appliedCommit = *sess.AppliedCommit
	}

	workspacePath := ""
	if sess.WorkspacePath != nil {
		workspacePath = *sess.WorkspacePath
	}

	workspaceCommit := ""
	if sess.WorkspaceCommit != nil {
		workspaceCommit = *sess.WorkspaceCommit
	}

	activeEnvSetIDs := sess.ActiveEnvSetIDs
	if activeEnvSetIDs == nil {
		activeEnvSetIDs = []string{}
	}

	timestamp := sess.UpdatedAt.Format(time.RFC3339)
	if sess.UpdatedAt.IsZero() {
		timestamp = time.Now().Format(time.RFC3339)
	}

	createdAt := sess.CreatedAt.Format(time.RFC3339)
	if sess.CreatedAt.IsZero() {
		createdAt = timestamp
	}

	return &Session{
		ID:              sess.ID,
		ProjectID:       sess.ProjectID,
		Name:            sess.Name,
		DisplayName:     displayName,
		Description:     description,
		CreatedAt:       createdAt,
		Timestamp:       timestamp,
		Status:          normalizeSessionStatus(sess.Status),
		CommitStatus:    sess.CommitStatus,
		CommitOperation: commitOperation,
		CommitError:     commitError,
		BaseCommit:      baseCommit,
		AppliedCommit:   appliedCommit,
		ErrorMessage:    errorMessage,
		Files:           []FileNode{},
		WorkspaceID:     sess.WorkspaceID,
		WorkspacePath:   workspacePath,
		WorkspaceCommit: workspaceCommit,
		ActiveEnvSetIDs: activeEnvSetIDs,
	}
}

// SessionConfig contains all the configuration needed for agent startup.
type SessionConfig struct {
	Session   *Session   `json:"session"`
	Workspace *Workspace `json:"workspace"`
}

// Initialize performs the session initialization work synchronously.
// This is called by the dispatcher when processing a session_init job.
// The session must already exist in the database.
func (s *SessionService) Initialize(
	ctx context.Context,
	sessionID string,
) error {
	// Get session from store (model)
	sessionModel, err := s.store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("session not found: %w", err)
	}

	// Get workspace info
	workspace, err := s.store.GetWorkspaceByID(ctx, sessionModel.WorkspaceID)
	if err != nil {
		s.updateStatusWithEvent(ctx, sessionModel.ProjectID, sessionID, model.SessionStatusError, ptrString("workspace not found: "+err.Error()))
		return fmt.Errorf("workspace not found: %w", err)
	}

	// Convert to service Session for initializeSync
	session := s.mapSession(sessionModel)

	// Run initialization synchronously
	return s.initializeSync(ctx, session.ProjectID, session, workspace)
}

// initializeSync runs the initialization flow synchronously.
// The flow is: ensure workspace -> save workspace info on session -> create sandbox.
func (s *SessionService) initializeSync(
	ctx context.Context,
	projectID string,
	session *Session,
	workspace *model.Workspace,
) error {
	sessionID := session.ID

	// Step 1: Ensure workspace is available (always needed, even on reconcile)
	var workspacePath string
	var currentCommit string

	if s.gitService != nil {
		isGit := workspace.SourceType == "git" || git.IsGitURL(workspace.Path)
		if isGit {
			s.updateStatusWithEvent(ctx, projectID, sessionID, model.SessionStatusCloning, nil)
		}

		var err error
		workspacePath, currentCommit, err = s.gitService.EnsureWorkspaceRepo(ctx, workspace.ID)
		if err != nil {
			log.Printf("Git setup failed for session %s: %v", sessionID, err)
			s.updateStatusWithEvent(ctx, projectID, sessionID, model.SessionStatusError, ptrString("git setup failed: "+err.Error()))
			return fmt.Errorf("git setup failed: %w", err)
		}
	} else {
		// No git service - use workspace path directly (fallback for testing)
		workspacePath = workspace.Path
	}

	// Step 2: Determine which commit to use for the sandbox
	// On first initialization, save the workspace info and use the current commit.
	// On reconcile (values already set), use the stored commit to maintain consistency.
	var workspaceCommit string
	if session.WorkspacePath != "" {
		// Already initialized - use existing values (reconcile case)
		workspacePath = session.WorkspacePath
		workspaceCommit = session.WorkspaceCommit
	} else {
		// First initialization - save workspace path and commit
		workspaceCommit = currentCommit
		if err := s.store.UpdateSessionWorkspace(ctx, sessionID, workspacePath, workspaceCommit); err != nil {
			log.Printf("Failed to update session workspace info for %s: %v", sessionID, err)
			s.updateStatusWithEvent(ctx, projectID, sessionID, model.SessionStatusError, ptrString("failed to save workspace info: "+err.Error()))
			return fmt.Errorf("failed to save workspace info: %w", err)
		}
	}

	// Step 3: Create or get existing sandbox (idempotent)
	// First check if sandbox already exists (from a previous failed attempt)
	existingSandbox, err := s.sandboxProvider.Get(ctx, sessionID)
	if err != nil && !errors.Is(err, sandbox.ErrNotFound) {
		log.Printf("Failed to check for existing sandbox for session %s: %v", sessionID, err)
		s.updateStatusWithEvent(ctx, projectID, sessionID, model.SessionStatusError, ptrString("failed to check sandbox: "+err.Error()))
		return fmt.Errorf("failed to check sandbox: %w", err)
	}

	needsCreation := true
	if existingSandbox != nil {
		log.Printf("Sandbox already exists for session %s (status: %s)", sessionID, existingSandbox.Status)

		switch existingSandbox.Status {
		case sandbox.StatusRunning:
			// Container reports running — verify services are actually responsive.
			// This prevents marking a session "ready" when the container is mid-shutdown
			// (e.g., idle monitor sent SIGTERM but Docker still reports "running").
			if s.sandboxService != nil {
				if err := s.sandboxService.probeSandboxHealth(ctx, sessionID); err != nil {
					log.Printf("Sandbox for session %s reports running but health check failed: %v, removing", sessionID, err)
					if rmErr := s.sandboxProvider.Remove(ctx, sessionID); rmErr != nil {
						log.Printf("Failed to remove unhealthy sandbox for session %s: %v", sessionID, rmErr)
					}
					needsCreation = true
					break
				}
			}
			log.Printf("Sandbox for session %s is already running (verified healthy)", sessionID)
			needsCreation = false

		case sandbox.StatusCreated, sandbox.StatusStopped:
			s.updateStatusWithEvent(ctx, projectID, sessionID, model.SessionStatusCreatingSandbox, nil)
			if err := s.sandboxProvider.Start(ctx, sessionID); err != nil {
				if !errors.Is(err, sandbox.ErrAlreadyRunning) {
					log.Printf("Sandbox start failed for session %s: %v, will attempt to remove and recreate", sessionID, err)
					// Start failed - try to remove and recreate
					if rmErr := s.sandboxProvider.Remove(ctx, sessionID); rmErr != nil {
						log.Printf("Failed to remove failed sandbox for session %s: %v", sessionID, rmErr)
						s.updateStatusWithEvent(ctx, projectID, sessionID, model.SessionStatusError, ptrString("sandbox start failed and removal failed: "+rmErr.Error()))
						return fmt.Errorf("sandbox start failed and removal failed: %w", rmErr)
					}
					// Successfully removed, allow recreation
					needsCreation = true
				} else {
					// Already running (race condition), treat as success
					needsCreation = false
				}
			} else {
				// Successfully started
				needsCreation = false
			}

		default:
			// Sandbox is in failed state - remove and recreate (preserve volumes)
			log.Printf("Removing failed sandbox for session %s", sessionID)
			if err := s.sandboxProvider.Remove(ctx, sessionID); err != nil {
				log.Printf("Failed to remove old sandbox for session %s: %v", sessionID, err)
				s.updateStatusWithEvent(ctx, projectID, sessionID, model.SessionStatusError, ptrString("failed to remove old sandbox: "+err.Error()))
				return fmt.Errorf("failed to remove old sandbox: %w", err)
			}
		}
	}

	if needsCreation {
		// Check if image needs to be pulled and notify if so
		if !s.sandboxProvider.ImageExists(ctx) {
			s.updateStatusWithEvent(ctx, projectID, sessionID, model.SessionStatusPullingImage, nil)
			log.Printf("Pulling sandbox image %s for session %s", s.sandboxProvider.Image(), sessionID)
		} else {
			s.updateStatusWithEvent(ctx, projectID, sessionID, model.SessionStatusCreatingSandbox, nil)
		}

		var sshKey *sandbox.SSHKeyProvision
		if s.sandboxService != nil {
			sessionModel, err := s.store.GetSessionByID(ctx, sessionID)
			if err != nil {
				return fmt.Errorf("failed to reload session for sandbox ssh key provisioning: %w", err)
			}
			sshKey, err = ensureSessionSSHKey(ctx, s.store, s.sandboxService.cfg, sessionModel)
			if err != nil {
				return fmt.Errorf("failed to ensure sandbox ssh key: %w", err)
			}
		}

		sandboxSecret := generateSecret(32)
		opts := sandbox.CreateOptions{
			SharedSecret: sandboxSecret,
			SSHKey:       sshKey,
			Labels: map[string]string{
				"discobot.session.id":   sessionID,
				"discobot.workspace.id": workspace.ID,
				"discobot.project.id":   projectID,
			},
			WorkspacePath:   workspacePath,
			WorkspaceSource: workspace.Path, // Original source (git URL or local path) for WORKSPACE_ORIGIN_PATH env var
			WorkspaceCommit: workspaceCommit,
		}

		_, err := s.sandboxProvider.Create(ctx, sessionID, opts)
		if err != nil {
			log.Printf("Sandbox creation failed for session %s: %v", sessionID, err)
			s.updateStatusWithEvent(ctx, projectID, sessionID, model.SessionStatusError, ptrString("sandbox creation failed: "+err.Error()))
			return fmt.Errorf("sandbox creation failed: %w", err)
		}

		// Start the sandbox
		if err := s.sandboxProvider.Start(ctx, sessionID); err != nil {
			log.Printf("Sandbox start failed for session %s: %v", sessionID, err)
			s.updateStatusWithEvent(ctx, projectID, sessionID, model.SessionStatusError, ptrString("sandbox start failed: "+err.Error()))
			return fmt.Errorf("sandbox start failed: %w", err)
		}
	}

	// Success! Update status to running
	s.updateStatusWithEvent(ctx, projectID, sessionID, model.SessionStatusReady, nil)
	log.Printf("Session %s initialized successfully", sessionID)
	return nil
}

// updateStatusWithEvent updates session status and emits an SSE event.
// This now just delegates to UpdateStatus since it always publishes events.
func (s *SessionService) updateStatusWithEvent(ctx context.Context, projectID, sessionID, status string, errorMsg *string) {
	_, err := s.UpdateStatus(ctx, projectID, sessionID, status, errorMsg)
	if err != nil {
		log.Printf("Failed to update session %s status to %s: %v", sessionID, status, err)
	}
}

// generateSecret generates a cryptographically secure random hex string.
func generateSecret(length int) string {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to a less random but still unique value
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

// ptrString returns a pointer to a string.
func ptrString(s string) *string {
	return &s
}

// PerformCommit performs the session commit work synchronously.
// This is called by the dispatcher when processing a session_commit job.
// Jobs for the same workspace are serialized by the job queue, so no
// precondition checks on commit status are needed.
//
// Flow:
// 1. Set workspace/session to committing, get fresh base commit
// 2. If workspace commit changed, update baseCommit and check for an existing replay bundle
// 3. If pending: send /discobot-commit to agent, transition to committing
// 4. If appliedCommit not set: fetch replay bundle from agent-api and apply it to the workspace
// 5. Transition to completed
func (s *SessionService) PerformCommit(ctx context.Context, projectID, sessionID string) (retErr error) {
	// Get session
	sess, err := s.store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("session not found: %w", err)
	}

	// Get workspace
	workspace, err := s.store.GetWorkspaceByID(ctx, sess.WorkspaceID)
	if err != nil {
		return fmt.Errorf("workspace not found: %w", err)
	}

	// If PerformCommit returns an error (e.g. context deadline exceeded),
	// mark the commit as failed so it doesn't get stuck in "pending" or "committing".
	// Use a background context since the original ctx may have been cancelled.
	defer func() {
		if retErr != nil {
			failCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			s.setCommitFailed(failCtx, projectID, workspace, sess, retErr.Error())
			retErr = nil
		}
	}()

	// Get current git status and set up session for this commit
	gitStatus, err := s.gitService.Status(ctx, sess.WorkspaceID)
	if err != nil {
		s.setCommitFailed(ctx, projectID, workspace, sess, fmt.Sprintf("Failed to get workspace status: %v", err))
		return nil
	}

	sess.CommitStatus = model.CommitStatusPending
	sess.CommitOperation = ptrString(CommitOperationCommit)
	sess.BaseCommit = ptrString(gitStatus.Commit)
	sess.AppliedCommit = nil
	sess.CommitError = nil
	if err := s.store.UpdateSession(ctx, sess); err != nil {
		return fmt.Errorf("failed to update session for commit: %w", err)
	}
	s.publishCommitStatusChanged(ctx, projectID, sess.ID, model.CommitStatusPending)

	// Step 1: Handle workspace commit changes
	if err := s.syncBaseCommit(ctx, projectID, workspace, sess); err != nil {
		return err
	}
	if sess.CommitStatus == model.CommitStatusFailed {
		return nil
	}

	// Step 1.5: Optimistically check if the agent already has a replay bundle ready
	if sess.CommitStatus == model.CommitStatusPending && (sess.AppliedCommit == nil || *sess.AppliedCommit == "") {
		if err := s.tryApplyExistingReplayBundle(ctx, projectID, workspace, sess); err != nil {
			return err
		}
		if sess.CommitStatus == model.CommitStatusFailed {
			return nil
		}
	}

	// Step 2: Send /discobot-commit to agent (if pending)
	if sess.CommitStatus == model.CommitStatusPending {
		if err := s.sendCommitPrompt(ctx, projectID, workspace, sess); err != nil {
			return err
		}
		if sess.CommitStatus == model.CommitStatusFailed {
			return nil
		}
	}

	// Step 3: Fetch and apply replay bundle (if not yet done)
	if sess.AppliedCommit == nil || *sess.AppliedCommit == "" {
		if err := s.fetchAndApplyReplayBundle(ctx, projectID, workspace, sess); err != nil {
			return err
		}
		if sess.CommitStatus == model.CommitStatusFailed {
			return nil
		}
	}

	// Step 4: Complete
	log.Printf("Session %s: commit completed with applied commit %s", sess.ID, *sess.AppliedCommit)
	if err := s.markCommitCompleted(ctx, projectID, sess); err != nil {
		return err
	}

	log.Printf("Workspace %s committed successfully via session %s", workspace.ID, sess.ID)
	return nil
}

// PerformRebase performs session rebase work synchronously.
// It ensures sandbox commits are rebased on the current workspace HEAD.
func (s *SessionService) PerformRebase(ctx context.Context, projectID, sessionID string) (retErr error) {
	// Get session
	sess, err := s.store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("session not found: %w", err)
	}

	// Get workspace
	workspace, err := s.store.GetWorkspaceByID(ctx, sess.WorkspaceID)
	if err != nil {
		return fmt.Errorf("workspace not found: %w", err)
	}

	defer func() {
		if retErr != nil {
			failCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			s.setCommitFailed(failCtx, projectID, workspace, sess, retErr.Error())
			retErr = nil
		}
	}()

	// Get current git status and set up session for this rebase
	gitStatus, err := s.gitService.Status(ctx, sess.WorkspaceID)
	if err != nil {
		s.setCommitFailed(ctx, projectID, workspace, sess, fmt.Sprintf("Failed to get workspace status: %v", err))
		return nil
	}

	sess.CommitStatus = model.CommitStatusPending
	sess.CommitOperation = ptrString(CommitOperationRebase)
	sess.BaseCommit = ptrString(gitStatus.Commit)
	sess.AppliedCommit = nil
	sess.CommitError = nil
	if err := s.store.UpdateSession(ctx, sess); err != nil {
		return fmt.Errorf("failed to update session for rebase: %w", err)
	}
	s.publishCommitStatusChanged(ctx, projectID, sess.ID, model.CommitStatusPending)

	// Step 1: Handle workspace commit changes
	if err := s.syncBaseCommit(ctx, projectID, workspace, sess); err != nil {
		return err
	}
	if sess.CommitStatus == model.CommitStatusFailed {
		return nil
	}

	// Step 1.5: Optimistically check if sandbox is already rebased.
	// Parent mismatch here means "not rebased yet" and should fall through to prompting.
	if sess.CommitStatus == model.CommitStatusPending {
		validated, err := s.validateSandboxRebased(ctx, projectID, workspace, sess, true)
		if err != nil {
			return err
		}
		if validated {
			return nil
		}
		if sess.CommitStatus == model.CommitStatusFailed {
			return nil
		}
	}

	// Step 2: Send /discobot-rebase to agent
	if sess.CommitStatus == model.CommitStatusPending {
		if err := s.sendRebasePrompt(ctx, projectID, workspace, sess); err != nil {
			return err
		}
		if sess.CommitStatus == model.CommitStatusFailed {
			return nil
		}
	}

	// Step 3: Validate rebased state
	validated, err := s.validateSandboxRebased(ctx, projectID, workspace, sess, false)
	if err != nil {
		return err
	}
	if validated {
		log.Printf("Workspace %s rebased successfully in sandbox via session %s", workspace.ID, sess.ID)
	}
	return nil
}

// syncBaseCommit checks if the workspace commit has changed and updates baseCommit.
// If a replay bundle is already available from the agent, it applies it directly.
func (s *SessionService) syncBaseCommit(ctx context.Context, projectID string, workspace *model.Workspace, sess *model.Session) error {
	gitStatus, err := s.gitService.Status(ctx, sess.WorkspaceID)
	if err != nil {
		s.setCommitFailed(ctx, projectID, workspace, sess, fmt.Sprintf("Failed to get workspace status: %v", err))
		return nil
	}

	// No change - nothing to do
	if gitStatus.Commit == *sess.BaseCommit {
		return nil
	}

	log.Printf("Session %s: workspace commit changed from %s to %s, updating baseCommit", sess.ID, *sess.BaseCommit, gitStatus.Commit)
	sess.BaseCommit = ptrString(gitStatus.Commit)
	if err := s.store.UpdateSession(ctx, sess); err != nil {
		return fmt.Errorf("failed to update session baseCommit: %w", err)
	}

	return nil
}

// tryApplyExistingReplayBundle checks if the agent already has a replay bundle ready and applies it.
// This is called optimistically before sending /discobot-commit in case commits are already available.
func (s *SessionService) tryApplyExistingReplayBundle(ctx context.Context, projectID string, workspace *model.Workspace, sess *model.Session) error {
	if s.sandboxService == nil {
		return nil
	}

	log.Printf("Session %s: checking if agent has an existing replay bundle for commit %s", sess.ID, *sess.BaseCommit)

	client, err := s.sandboxService.GetClient(ctx, sess.ID)
	if err != nil {
		log.Printf("Session %s: no existing replay bundle available (error: %v), continuing with prompt", sess.ID, err)
		return nil
	}

	commitsResp, err := client.GetCommits(ctx, *sess.BaseCommit)
	if err != nil {
		log.Printf("Session %s: no existing replay bundle available (error: %v), continuing with prompt", sess.ID, err)
		return nil
	}
	if commitsResp.CommitCount == 0 {
		log.Printf("Session %s: no existing replay bundle available (commit count: 0), continuing with prompt", sess.ID)
		return nil
	}

	// Agent already has commits ready - apply the replay bundle directly
	log.Printf("Session %s: agent has %d existing commits, skipping prompt and applying replay bundle", sess.ID, commitsResp.CommitCount)
	return s.applyReplayBundle(ctx, projectID, workspace, sess, commitsResp.ReplayBundle, commitsResp.CommitCount)
}

// waitForPromptTerminalEvent waits for a prompt stream to reach a terminal state.
// Newer agent streams stay open across turns, so commit/rebase operations must
// stop on terminal chunk types rather than waiting for the SSE connection to close.
func waitForPromptTerminalEvent(ctx context.Context, streamCh <-chan SSELine) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case line, ok := <-streamCh:
			if !ok {
				if err := ctx.Err(); err != nil {
					return err
				}
				return fmt.Errorf("chat stream ended before completion finished")
			}

			if line.Done {
				return nil
			}
			if line.Data == "" {
				continue
			}

			var payload struct {
				Type      string `json:"type"`
				ErrorText string `json:"errorText,omitempty"`
				Reason    string `json:"reason,omitempty"`
			}
			if err := json.Unmarshal([]byte(line.Data), &payload); err != nil {
				continue
			}

			switch payload.Type {
			case "finish":
				return nil
			case "error":
				if payload.ErrorText == "" {
					payload.ErrorText = "agent completion returned an error"
				}
				return errors.New(payload.ErrorText)
			case "abort":
				if payload.Reason == "" {
					payload.Reason = "agent completion aborted"
				}
				return errors.New(payload.Reason)
			}
		}
	}
}

// sendCommitPrompt sends the /discobot-commit command to the agent.
func (s *SessionService) sendCommitPrompt(ctx context.Context, projectID string, workspace *model.Workspace, sess *model.Session) error {
	if s.sandboxService == nil {
		s.setCommitFailed(ctx, projectID, workspace, sess, "Sandbox service not available")
		return nil
	}

	log.Printf("Session %s: sending /discobot-commit %s to agent", sess.ID, *sess.BaseCommit)

	commitMessage := fmt.Sprintf("/discobot-commit %s", *sess.BaseCommit)
	messages, err := buildCommitMessage(sess.ID+"-commit", commitMessage)
	if err != nil {
		s.setCommitFailed(ctx, projectID, workspace, sess, fmt.Sprintf("Failed to build commit message: %v", err))
		return nil
	}

	client, err := s.sandboxService.GetClient(ctx, sess.ID)
	if err != nil {
		s.setCommitFailed(ctx, projectID, workspace, sess, fmt.Sprintf("Failed to get sandbox client: %v", err))
		return nil
	}

	// Dereference model pointer; use empty string if nil (agent will use default)
	modelID := ""
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	streamCh, err := client.SendMessages(streamCtx, sess.ID, messages, modelID, nil)
	if err != nil {
		s.setCommitFailed(ctx, projectID, workspace, sess, fmt.Sprintf("Failed to send commit message to agent: %v", err))
		return nil
	}

	// Transition to committing now that the agent is actively working
	sess.CommitStatus = model.CommitStatusCommitting
	if err := s.store.UpdateSession(ctx, sess); err != nil {
		return fmt.Errorf("failed to update session status: %w", err)
	}
	s.publishCommitStatusChanged(ctx, projectID, sess.ID, model.CommitStatusCommitting)

	if err := waitForPromptTerminalEvent(streamCtx, streamCh); err != nil {
		s.setCommitFailed(ctx, projectID, workspace, sess, fmt.Sprintf("Failed while waiting for commit prompt to finish: %v", err))
		return nil
	}

	log.Printf("Session %s: /discobot-commit message completed", sess.ID)
	return nil
}

// sendRebasePrompt sends the /discobot-rebase command to the agent.
func (s *SessionService) sendRebasePrompt(ctx context.Context, projectID string, workspace *model.Workspace, sess *model.Session) error {
	if s.sandboxService == nil {
		s.setCommitFailed(ctx, projectID, workspace, sess, "Sandbox service not available")
		return nil
	}

	log.Printf("Session %s: sending /discobot-rebase %s to agent", sess.ID, *sess.BaseCommit)

	rebaseMessage := fmt.Sprintf("/discobot-rebase %s", *sess.BaseCommit)
	messages, err := buildCommitMessage(sess.ID+"-rebase", rebaseMessage)
	if err != nil {
		s.setCommitFailed(ctx, projectID, workspace, sess, fmt.Sprintf("Failed to build rebase message: %v", err))
		return nil
	}

	client, err := s.sandboxService.GetClient(ctx, sess.ID)
	if err != nil {
		s.setCommitFailed(ctx, projectID, workspace, sess, fmt.Sprintf("Failed to get sandbox client: %v", err))
		return nil
	}

	modelID := ""
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	streamCh, err := client.SendMessages(streamCtx, sess.ID, messages, modelID, nil)
	if err != nil {
		s.setCommitFailed(ctx, projectID, workspace, sess, fmt.Sprintf("Failed to send rebase message to agent: %v", err))
		return nil
	}

	sess.CommitStatus = model.CommitStatusCommitting
	if err := s.store.UpdateSession(ctx, sess); err != nil {
		return fmt.Errorf("failed to update session status: %w", err)
	}
	s.publishCommitStatusChanged(ctx, projectID, sess.ID, model.CommitStatusCommitting)

	if err := waitForPromptTerminalEvent(streamCtx, streamCh); err != nil {
		s.setCommitFailed(ctx, projectID, workspace, sess, fmt.Sprintf("Failed while waiting for rebase prompt to finish: %v", err))
		return nil
	}

	log.Printf("Session %s: /discobot-rebase message completed", sess.ID)
	return nil
}

// validateSandboxRebased checks whether sandbox history is rebased onto baseCommit.
// When allowParentMismatch is true, parent mismatch is treated as "not yet rebased"
// and the caller can continue by sending a rebase prompt.
func (s *SessionService) validateSandboxRebased(ctx context.Context, projectID string, workspace *model.Workspace, sess *model.Session, allowParentMismatch bool) (bool, error) {
	if s.sandboxService == nil {
		s.setCommitFailed(ctx, projectID, workspace, sess, "Sandbox service not available")
		return false, nil
	}

	client, err := s.sandboxService.GetClient(ctx, sess.ID)
	if err != nil {
		s.setCommitFailed(ctx, projectID, workspace, sess, fmt.Sprintf("Failed to get sandbox client: %v", err))
		return false, nil
	}

	commitsResp, err := client.GetCommits(ctx, *sess.BaseCommit)
	if err != nil {
		if strings.Contains(err.Error(), "commits error (no_commits)") {
			if completeErr := s.markCommitCompleted(ctx, projectID, sess); completeErr != nil {
				return false, completeErr
			}
			log.Printf("Session %s: sandbox already aligned with workspace base commit %s", sess.ID, *sess.BaseCommit)
			return true, nil
		}

		if allowParentMismatch && strings.Contains(err.Error(), "commits error (parent_mismatch)") {
			log.Printf("Session %s: sandbox not yet rebased onto %s (parent mismatch), continuing with rebase prompt", sess.ID, *sess.BaseCommit)
			return false, nil
		}

		s.setCommitFailed(ctx, projectID, workspace, sess, fmt.Sprintf("Failed to validate rebased commits: %v", err))
		return false, nil
	}

	if commitsResp.CommitCount > 0 {
		log.Printf("Session %s: sandbox rebased with %d commits on base %s", sess.ID, commitsResp.CommitCount, *sess.BaseCommit)
	} else {
		log.Printf("Session %s: sandbox rebased on base %s", sess.ID, *sess.BaseCommit)
	}

	if err := s.markCommitCompleted(ctx, projectID, sess); err != nil {
		return false, err
	}
	return true, nil
}

// fetchAndApplyReplayBundle fetches a replay bundle from the agent and applies it to the workspace.
func (s *SessionService) fetchAndApplyReplayBundle(ctx context.Context, projectID string, workspace *model.Workspace, sess *model.Session) error {
	if s.sandboxService == nil {
		s.setCommitFailed(ctx, projectID, workspace, sess, "Sandbox service not available")
		return nil
	}

	log.Printf("Session %s: fetching commits from agent-api (parent=%s)", sess.ID, *sess.BaseCommit)

	client, err := s.sandboxService.GetClient(ctx, sess.ID)
	if err != nil {
		s.setCommitFailed(ctx, projectID, workspace, sess, fmt.Sprintf("Failed to get sandbox client: %v", err))
		return nil
	}

	commitsResp, err := client.GetCommits(ctx, *sess.BaseCommit)
	if err != nil {
		s.setCommitFailed(ctx, projectID, workspace, sess, fmt.Sprintf("Failed to get commits from agent: %v", err))
		return nil
	}

	if commitsResp.CommitCount == 0 {
		s.setCommitFailed(ctx, projectID, workspace, sess, "No commits found in agent sandbox")
		return nil
	}

	log.Printf("Session %s: received %d commits from agent, applying replay bundle to workspace", sess.ID, commitsResp.CommitCount)
	return s.applyReplayBundle(ctx, projectID, workspace, sess, commitsResp.ReplayBundle, commitsResp.CommitCount)
}

// applyReplayBundle applies the given replay bundle to the workspace and updates the session.
func (s *SessionService) applyReplayBundle(ctx context.Context, projectID string, workspace *model.Workspace, sess *model.Session, replayBundle string, commitCount int) error {
	if sess.CommitStatus != model.CommitStatusCommitting {
		sess.CommitStatus = model.CommitStatusCommitting
		if err := s.store.UpdateSession(ctx, sess); err != nil {
			return fmt.Errorf("failed to update session status: %w", err)
		}
		s.publishCommitStatusChanged(ctx, projectID, sess.ID, model.CommitStatusCommitting)
	}

	finalCommit, err := s.gitService.ApplyReplayBundle(ctx, sess.WorkspaceID, []byte(replayBundle))
	if err != nil {
		s.setCommitFailed(ctx, projectID, workspace, sess, fmt.Sprintf("Failed to apply replay bundle to workspace: %v", err))
		return nil
	}

	sess.AppliedCommit = ptrString(finalCommit)
	if err := s.store.UpdateSession(ctx, sess); err != nil {
		return fmt.Errorf("failed to update session applied commit: %w", err)
	}
	s.publishCommitStatusChanged(ctx, projectID, sess.ID, model.CommitStatusCommitting)
	log.Printf("Session %s: %d replayed commits applied, final commit=%s", sess.ID, commitCount, finalCommit)
	return nil
}

func (s *SessionService) markCommitCompleted(ctx context.Context, projectID string, sess *model.Session) error {
	if sess.CommitOperation != nil && *sess.CommitOperation == CommitOperationRebase {
		sess.WorkspaceCommit = sess.BaseCommit
	}

	sess.CommitStatus = model.CommitStatusCompleted
	sess.CommitError = nil
	if err := s.store.UpdateSession(ctx, sess); err != nil {
		return fmt.Errorf("failed to update session commit status: %w", err)
	}
	s.publishCommitStatusChanged(ctx, projectID, sess.ID, model.CommitStatusCompleted)
	return nil
}

// setCommitFailed sets the commit status to failed with an error message.
func (s *SessionService) setCommitFailed(ctx context.Context, projectID string, workspace *model.Workspace, sess *model.Session, errorMsg string) {
	log.Printf("Workspace %s commit failed (via session %s): %s", workspace.ID, sess.ID, errorMsg)

	sess.CommitStatus = model.CommitStatusFailed
	sess.CommitError = ptrString(errorMsg)
	if err := s.store.UpdateSession(ctx, sess); err != nil {
		log.Printf("Failed to update session %s commit status to failed: %v", sess.ID, err)
		return
	}

	s.publishCommitStatusChanged(ctx, projectID, sess.ID, model.CommitStatusFailed)
}

// buildCommitMessage creates a UIMessage array for operation commands.
// Returns json.RawMessage that can be passed to SendMessages.
func buildCommitMessage(msgID, text string) (json.RawMessage, error) {
	// Build the text part
	part := map[string]interface{}{
		"type": "text",
		"text": text,
	}
	parts, err := json.Marshal([]interface{}{part})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal parts: %w", err)
	}

	// Build the message
	message := map[string]interface{}{
		"id":    msgID,
		"role":  "user",
		"parts": json.RawMessage(parts),
	}
	messages, err := json.Marshal([]interface{}{message})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal messages: %w", err)
	}

	return messages, nil
}
