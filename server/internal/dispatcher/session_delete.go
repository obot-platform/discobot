package dispatcher

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/obot-platform/discobot/server/internal/jobs"
	"github.com/obot-platform/discobot/server/internal/model"
	"github.com/obot-platform/discobot/server/internal/service"
)

// SessionDeleteExecutor handles session_delete jobs.
type SessionDeleteExecutor struct {
	sessionService *service.SessionService
}

// NewSessionDeleteExecutor creates a new session delete executor.
func NewSessionDeleteExecutor(sessionSvc *service.SessionService) *SessionDeleteExecutor {
	return &SessionDeleteExecutor{sessionService: sessionSvc}
}

// Type returns the job type this executor handles.
func (e *SessionDeleteExecutor) Type() jobs.JobType {
	return jobs.JobTypeSessionDelete
}

// Execute processes the job.
func (e *SessionDeleteExecutor) Execute(ctx context.Context, job *model.Job) error {
	if e.sessionService == nil {
		return fmt.Errorf("session service not available")
	}

	var payload jobs.SessionDeletePayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("invalid payload: %w", err)
	}

	if payload.SessionID == "" {
		return fmt.Errorf("sessionId is required")
	}

	if payload.ProjectID == "" {
		return fmt.Errorf("projectId is required")
	}

	return e.sessionService.PerformDeletion(ctx, payload.ProjectID, payload.SessionID)
}

// SessionSandboxDeleteExecutor handles session_sandbox_delete jobs.
type SessionSandboxDeleteExecutor struct {
	sessionService *service.SessionService
}

// NewSessionSandboxDeleteExecutor creates a new session sandbox delete executor.
func NewSessionSandboxDeleteExecutor(sessionSvc *service.SessionService) *SessionSandboxDeleteExecutor {
	return &SessionSandboxDeleteExecutor{sessionService: sessionSvc}
}

// Type returns the job type this executor handles.
func (e *SessionSandboxDeleteExecutor) Type() jobs.JobType {
	return jobs.JobTypeSessionSandboxDelete
}

// Execute processes the job.
func (e *SessionSandboxDeleteExecutor) Execute(ctx context.Context, job *model.Job) error {
	if e.sessionService == nil {
		return fmt.Errorf("session service not available")
	}

	var payload jobs.SessionSandboxDeletePayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("invalid payload: %w", err)
	}

	if payload.SessionID == "" {
		return fmt.Errorf("sessionId is required")
	}

	return e.sessionService.PerformDeferredSandboxDeletion(ctx, payload.SessionID)
}
