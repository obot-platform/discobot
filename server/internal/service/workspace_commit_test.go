package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/obot-platform/discobot/server/internal/jobs"
	"github.com/obot-platform/discobot/server/internal/model"
)

// TestCommitSession_Success tests that CommitSession enqueues a job with the correct payload.
func TestCommitSession_Success(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()

	project := env.createTestProject(t)
	workspace, initialCommit := env.createTestWorkspace(t, project.ID)
	session := env.createTestSession(t, project.ID, workspace.ID, initialCommit)

	var enqueuedJob jobs.JobPayload
	mockEnqueuer := &mockJobEnqueuer{
		enqueueFunc: func(_ context.Context, payload jobs.JobPayload) error {
			enqueuedJob = payload
			return nil
		},
	}

	sessionSvc := NewSessionService(env.store, env.gitService, env.mockSandbox, nil, env.eventBroker, mockEnqueuer)

	err := sessionSvc.CommitSession(context.Background(), project.ID, session.ID, mockEnqueuer)
	if err != nil {
		t.Fatalf("CommitSession failed: %v", err)
	}

	if enqueuedJob == nil {
		t.Fatal("Expected job to be enqueued")
	}
	commitPayload, ok := enqueuedJob.(jobs.SessionCommitPayload)
	if !ok {
		t.Fatalf("Expected SessionCommitPayload, got %T", enqueuedJob)
	}
	if commitPayload.WorkspaceID != workspace.ID {
		t.Errorf("Expected workspace ID %s, got %s", workspace.ID, commitPayload.WorkspaceID)
	}
	if commitPayload.SessionID != session.ID {
		t.Errorf("Expected session ID %s, got %s", session.ID, commitPayload.SessionID)
	}
}

// TestCommitSession_EnqueueFailure tests that CommitSession returns an error when enqueue fails.
func TestCommitSession_EnqueueFailure(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()

	project := env.createTestProject(t)
	workspace, initialCommit := env.createTestWorkspace(t, project.ID)
	session := env.createTestSession(t, project.ID, workspace.ID, initialCommit)

	mockEnqueuer := &mockJobEnqueuer{
		enqueueFunc: func(_ context.Context, _ jobs.JobPayload) error {
			return fmt.Errorf("simulated enqueue error")
		},
	}

	sessionSvc := NewSessionService(env.store, env.gitService, env.mockSandbox, nil, env.eventBroker, mockEnqueuer)

	err := sessionSvc.CommitSession(context.Background(), project.ID, session.ID, mockEnqueuer)
	if err == nil {
		t.Fatal("Expected CommitSession to fail when enqueue fails")
	}
}

// TestRebaseSession_Success tests that RebaseSession enqueues a job with the correct payload.
func TestRebaseSession_Success(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()

	project := env.createTestProject(t)
	workspace, initialCommit := env.createTestWorkspace(t, project.ID)
	session := env.createTestSession(t, project.ID, workspace.ID, initialCommit)

	var enqueuedJob jobs.JobPayload
	mockEnqueuer := &mockJobEnqueuer{
		enqueueFunc: func(_ context.Context, payload jobs.JobPayload) error {
			enqueuedJob = payload
			return nil
		},
	}

	sessionSvc := NewSessionService(env.store, env.gitService, env.mockSandbox, nil, env.eventBroker, mockEnqueuer)

	err := sessionSvc.RebaseSession(context.Background(), project.ID, session.ID, mockEnqueuer)
	if err != nil {
		t.Fatalf("RebaseSession failed: %v", err)
	}

	if enqueuedJob == nil {
		t.Fatal("Expected job to be enqueued")
	}
	rebasePayload, ok := enqueuedJob.(jobs.SessionRebasePayload)
	if !ok {
		t.Fatalf("Expected SessionRebasePayload, got %T", enqueuedJob)
	}
	if rebasePayload.WorkspaceID != workspace.ID {
		t.Errorf("Expected workspace ID %s, got %s", workspace.ID, rebasePayload.WorkspaceID)
	}
	if rebasePayload.SessionID != session.ID {
		t.Errorf("Expected session ID %s, got %s", session.ID, rebasePayload.SessionID)
	}
}

// TestRebaseSession_EnqueueFailure tests that RebaseSession returns an error when enqueue fails.
func TestRebaseSession_EnqueueFailure(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()

	project := env.createTestProject(t)
	workspace, initialCommit := env.createTestWorkspace(t, project.ID)
	session := env.createTestSession(t, project.ID, workspace.ID, initialCommit)

	mockEnqueuer := &mockJobEnqueuer{
		enqueueFunc: func(_ context.Context, _ jobs.JobPayload) error {
			return fmt.Errorf("simulated enqueue error")
		},
	}

	sessionSvc := NewSessionService(env.store, env.gitService, env.mockSandbox, nil, env.eventBroker, mockEnqueuer)

	err := sessionSvc.RebaseSession(context.Background(), project.ID, session.ID, mockEnqueuer)
	if err == nil {
		t.Fatal("Expected RebaseSession to fail when enqueue fails")
	}
}

// TestSessionOperations_BlockParallelStart tests that a second operation cannot start while one is in progress.
func TestSessionOperations_BlockParallelStart(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()

	project := env.createTestProject(t)
	workspace, initialCommit := env.createTestWorkspace(t, project.ID)
	session := env.createTestSession(t, project.ID, workspace.ID, initialCommit)

	session.CommitStatus = model.CommitStatusPending
	session.CommitOperation = ptrString(model.CommitOperationCommit)
	if err := env.store.UpdateSession(context.Background(), session); err != nil {
		t.Fatalf("Failed to update session: %v", err)
	}

	sessionSvc := NewSessionService(env.store, env.gitService, env.mockSandbox, nil, env.eventBroker, nil)

	if err := sessionSvc.RebaseSession(context.Background(), project.ID, session.ID, &mockJobEnqueuer{}); !errors.Is(err, ErrSessionOperationInProgress) {
		t.Fatalf("Expected ErrSessionOperationInProgress, got %v", err)
	}
}

func TestReconcileCommitStates_ReenqueuesCommitWhenOperationUnset(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()

	project := env.createTestProject(t)
	workspace, initialCommit := env.createTestWorkspace(t, project.ID)
	session := env.createTestSession(t, project.ID, workspace.ID, initialCommit)

	session.CommitStatus = model.CommitStatusPending
	session.CommitOperation = nil
	if err := env.store.UpdateSession(context.Background(), session); err != nil {
		t.Fatalf("Failed to update session: %v", err)
	}

	var enqueued []jobs.JobPayload
	mockEnqueuer := &mockJobEnqueuer{
		enqueueFunc: func(_ context.Context, payload jobs.JobPayload) error {
			enqueued = append(enqueued, payload)
			return nil
		},
	}
	sessionSvc := NewSessionService(env.store, env.gitService, env.mockSandbox, nil, env.eventBroker, mockEnqueuer)

	if err := sessionSvc.ReconcileCommitStates(context.Background()); err != nil {
		t.Fatalf("ReconcileCommitStates failed: %v", err)
	}
	if len(enqueued) != 1 {
		t.Fatalf("Expected 1 enqueued job, got %d", len(enqueued))
	}
	if _, ok := enqueued[0].(jobs.SessionCommitPayload); !ok {
		t.Fatalf("Expected SessionCommitPayload, got %T", enqueued[0])
	}
}

func TestReconcileCommitStates_ReenqueuesRebaseWhenOperationSet(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()

	project := env.createTestProject(t)
	workspace, initialCommit := env.createTestWorkspace(t, project.ID)
	session := env.createTestSession(t, project.ID, workspace.ID, initialCommit)

	session.CommitStatus = model.CommitStatusCommitting
	session.CommitOperation = ptrString(model.CommitOperationRebase)
	if err := env.store.UpdateSession(context.Background(), session); err != nil {
		t.Fatalf("Failed to update session: %v", err)
	}

	var enqueued []jobs.JobPayload
	mockEnqueuer := &mockJobEnqueuer{
		enqueueFunc: func(_ context.Context, payload jobs.JobPayload) error {
			enqueued = append(enqueued, payload)
			return nil
		},
	}
	sessionSvc := NewSessionService(env.store, env.gitService, env.mockSandbox, nil, env.eventBroker, mockEnqueuer)

	if err := sessionSvc.ReconcileCommitStates(context.Background()); err != nil {
		t.Fatalf("ReconcileCommitStates failed: %v", err)
	}
	if len(enqueued) != 1 {
		t.Fatalf("Expected 1 enqueued job, got %d", len(enqueued))
	}
	if _, ok := enqueued[0].(jobs.SessionRebasePayload); !ok {
		t.Fatalf("Expected SessionRebasePayload, got %T", enqueued[0])
	}
}

func TestReconcileCommitStates_ReenqueuesRebaseWhenWorkspaceJobIsRunning(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()

	project := env.createTestProject(t)
	workspace, initialCommit := env.createTestWorkspace(t, project.ID)
	session := env.createTestSession(t, project.ID, workspace.ID, initialCommit)

	session.CommitStatus = model.CommitStatusPending
	session.CommitOperation = ptrString(model.CommitOperationRebase)
	if err := env.store.UpdateSession(context.Background(), session); err != nil {
		t.Fatalf("Failed to update session: %v", err)
	}

	if err := env.store.CreateJob(context.Background(), &model.Job{
		Type:         string(jobs.JobTypeSessionCommit),
		Payload:      []byte("{}"),
		Status:       string(model.JobStatusRunning),
		MaxAttempts:  1,
		Priority:     10,
		ResourceType: ptrString(jobs.ResourceTypeWorkspace),
		ResourceID:   ptrString(workspace.ID),
	}); err != nil {
		t.Fatalf("Failed to create running workspace job: %v", err)
	}

	var enqueued []jobs.JobPayload
	mockEnqueuer := &mockJobEnqueuer{
		enqueueFunc: func(_ context.Context, payload jobs.JobPayload) error {
			enqueued = append(enqueued, payload)
			return nil
		},
	}
	sessionSvc := NewSessionService(env.store, env.gitService, env.mockSandbox, nil, env.eventBroker, mockEnqueuer)

	if err := sessionSvc.ReconcileCommitStates(context.Background()); err != nil {
		t.Fatalf("ReconcileCommitStates failed: %v", err)
	}
	if len(enqueued) != 1 {
		t.Fatalf("Expected 1 enqueued job, got %d", len(enqueued))
	}
	if _, ok := enqueued[0].(jobs.SessionRebasePayload); !ok {
		t.Fatalf("Expected SessionRebasePayload, got %T", enqueued[0])
	}
}

func TestMarkCommitCompleted_CommitMarksCompleted(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()

	project := env.createTestProject(t)
	workspace, initialCommit := env.createTestWorkspace(t, project.ID)
	session := env.createTestSession(t, project.ID, workspace.ID, initialCommit)

	session.CommitStatus = model.CommitStatusCommitting
	session.CommitOperation = ptrString(model.CommitOperationCommit)
	if err := env.store.UpdateSession(context.Background(), session); err != nil {
		t.Fatalf("Failed to update session: %v", err)
	}

	sessionSvc := NewSessionService(env.store, env.gitService, env.mockSandbox, nil, env.eventBroker, nil)
	if err := sessionSvc.markCommitCompleted(context.Background(), project.ID, session); err != nil {
		t.Fatalf("markCommitCompleted failed: %v", err)
	}

	updated, err := env.store.GetSessionByID(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("Failed to get session: %v", err)
	}
	if updated.CommitStatus != model.CommitStatusCompleted {
		t.Fatalf("Expected commit status %s, got %s", model.CommitStatusCompleted, updated.CommitStatus)
	}
	if updated.CommitOperation == nil || *updated.CommitOperation != model.CommitOperationCommit {
		t.Fatalf("Expected commit operation %q, got %v", model.CommitOperationCommit, updated.CommitOperation)
	}
}

func TestMarkCommitCompleted_RebaseClearsCommitState(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()

	project := env.createTestProject(t)
	workspace, initialCommit := env.createTestWorkspace(t, project.ID)
	session := env.createTestSession(t, project.ID, workspace.ID, initialCommit)

	session.CommitStatus = model.CommitStatusCommitting
	session.CommitOperation = ptrString(model.CommitOperationRebase)
	rebasedCommit := "rebased123"
	session.BaseCommit = ptrString(rebasedCommit)
	session.CommitError = ptrString("old error")
	if err := env.store.UpdateSession(context.Background(), session); err != nil {
		t.Fatalf("Failed to update session: %v", err)
	}

	sessionSvc := NewSessionService(env.store, env.gitService, env.mockSandbox, nil, env.eventBroker, nil)
	if err := sessionSvc.markCommitCompleted(context.Background(), project.ID, session); err != nil {
		t.Fatalf("markCommitCompleted failed: %v", err)
	}

	updated, err := env.store.GetSessionByID(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("Failed to get session: %v", err)
	}
	if updated.CommitStatus != model.CommitStatusNone {
		t.Fatalf("Expected commit status %q, got %q", model.CommitStatusNone, updated.CommitStatus)
	}
	if updated.CommitOperation != nil {
		t.Fatalf("Expected commit operation to be cleared, got %v", updated.CommitOperation)
	}
	if updated.CommitError != nil {
		t.Fatalf("Expected commit error to be cleared, got %v", updated.CommitError)
	}
	if updated.WorkspaceCommit == nil || *updated.WorkspaceCommit != rebasedCommit {
		t.Fatalf("Expected workspace commit %q, got %v", rebasedCommit, updated.WorkspaceCommit)
	}
}

// TestSessionCommitPayload_ResourceKey tests that SessionCommitPayload returns workspace resource.
func TestSessionCommitPayload_ResourceKey(t *testing.T) {
	payload := jobs.SessionCommitPayload{
		ProjectID:   "test-project",
		SessionID:   "test-session",
		WorkspaceID: "test-workspace",
	}

	resourceType, resourceID := payload.ResourceKey()

	if resourceType != jobs.ResourceTypeWorkspace {
		t.Errorf("Expected resource type %s, got %s", jobs.ResourceTypeWorkspace, resourceType)
	}
	if resourceID != "test-workspace" {
		t.Errorf("Expected resource ID test-workspace, got %s", resourceID)
	}
}

// TestSessionCommitPayload_AllowDuplicates tests that SessionCommitPayload allows duplicate jobs.
func TestSessionCommitPayload_AllowDuplicates(t *testing.T) {
	payload := jobs.SessionCommitPayload{
		ProjectID:   "test-project",
		SessionID:   "test-session",
		WorkspaceID: "test-workspace",
	}

	if !payload.AllowDuplicates() {
		t.Error("Expected SessionCommitPayload.AllowDuplicates() to return true")
	}
}

// TestSessionRebasePayload_ResourceKey tests that SessionRebasePayload returns session resource.
func TestSessionRebasePayload_ResourceKey(t *testing.T) {
	payload := jobs.SessionRebasePayload{
		ProjectID:   "test-project",
		SessionID:   "test-session",
		WorkspaceID: "test-workspace",
	}

	resourceType, resourceID := payload.ResourceKey()

	if resourceType != jobs.ResourceTypeSession {
		t.Errorf("Expected resource type %s, got %s", jobs.ResourceTypeSession, resourceType)
	}
	if resourceID != "test-session" {
		t.Errorf("Expected resource ID test-session, got %s", resourceID)
	}
}

// TestSessionRebasePayload_AllowDuplicates tests that SessionRebasePayload allows duplicate jobs.
func TestSessionRebasePayload_AllowDuplicates(t *testing.T) {
	payload := jobs.SessionRebasePayload{
		ProjectID:   "test-project",
		SessionID:   "test-session",
		WorkspaceID: "test-workspace",
	}

	if !payload.AllowDuplicates() {
		t.Error("Expected SessionRebasePayload.AllowDuplicates() to return true")
	}
}

// TestClearCompletedCommitStatus clears a completed commit state when the
// session resumes active chat/editing work.
func TestClearCompletedCommitStatus(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()

	project := env.createTestProject(t)
	workspace, initialCommit := env.createTestWorkspace(t, project.ID)

	// Create session with completed commit status
	session := env.createTestSession(t, project.ID, workspace.ID, initialCommit)
	session.CommitStatus = model.CommitStatusCompleted
	session.AppliedCommit = ptrString("abc123")
	if err := env.store.UpdateSession(context.Background(), session); err != nil {
		t.Fatalf("Failed to update session: %v", err)
	}

	sessionSvc := NewSessionService(env.store, env.gitService, env.mockSandbox, nil, env.eventBroker, nil)

	err := sessionSvc.ClearCompletedCommitStatus(context.Background(), project.ID, session.ID)
	if err != nil {
		t.Fatalf("ClearCompletedCommitStatus failed: %v", err)
	}

	// Verify commit status was cleared
	sess, err := env.store.GetSessionByID(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("Failed to get session: %v", err)
	}
	if sess.CommitStatus != model.CommitStatusNone {
		t.Errorf("Expected commit status to be cleared to none, got %s", sess.CommitStatus)
	}

	// Verify the applied commit is still preserved
	if sess.AppliedCommit == nil || *sess.AppliedCommit != "abc123" {
		t.Errorf("Expected applied commit to be preserved as 'abc123', got %v", sess.AppliedCommit)
	}
}

// TestClearCompletedCommitStatus_DoesNotChangeIncompleteState tests that commit
// status is only cleared when it is "completed".
func TestClearCompletedCommitStatus_DoesNotChangeIncompleteState(t *testing.T) {
	env := newTestEnv(t)
	defer env.cleanup()

	project := env.createTestProject(t)
	workspace, initialCommit := env.createTestWorkspace(t, project.ID)

	// Create session with no commit status
	session := env.createTestSession(t, project.ID, workspace.ID, initialCommit)
	session.CommitStatus = model.CommitStatusNone
	if err := env.store.UpdateSession(context.Background(), session); err != nil {
		t.Fatalf("Failed to update session: %v", err)
	}

	sessionSvc := NewSessionService(env.store, env.gitService, env.mockSandbox, nil, env.eventBroker, nil)

	err := sessionSvc.ClearCompletedCommitStatus(context.Background(), project.ID, session.ID)
	if err != nil {
		t.Fatalf("ClearCompletedCommitStatus failed: %v", err)
	}

	// Verify commit status remains none (unchanged)
	sess, err := env.store.GetSessionByID(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("Failed to get session: %v", err)
	}
	if sess.CommitStatus != model.CommitStatusNone {
		t.Errorf("Expected commit status to remain none, got %s", sess.CommitStatus)
	}
}

// mockJobEnqueuer is a mock implementation of JobEnqueuer for testing.
type mockJobEnqueuer struct {
	enqueueFunc func(ctx context.Context, payload jobs.JobPayload) error
}

func (m *mockJobEnqueuer) Enqueue(ctx context.Context, payload jobs.JobPayload) error {
	if m.enqueueFunc != nil {
		return m.enqueueFunc(ctx, payload)
	}
	return nil
}
