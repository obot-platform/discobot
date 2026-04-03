package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"github.com/obot-platform/discobot/server/internal/events"
	"github.com/obot-platform/discobot/server/internal/jobs"
	"github.com/obot-platform/discobot/server/internal/model"
	"github.com/obot-platform/discobot/server/internal/sandbox/sandboxapi"
	"github.com/obot-platform/discobot/server/internal/store"
)

// JobEnqueuer is an interface for enqueuing background jobs.
// This breaks the import cycle between service and jobs packages.
type JobEnqueuer interface {
	Enqueue(ctx context.Context, payload jobs.JobPayload) error
}

// ChatService handles chat operations including session creation and message streaming.
type ChatService struct {
	store          *store.Store
	sessionService *SessionService
	jobEnqueuer    JobEnqueuer
	eventBroker    *events.Broker
	sandboxService *SandboxService
	gitService     *GitService
}

// NewChatService creates a new chat service.
func NewChatService(s *store.Store, sessionService *SessionService, jobEnqueuer JobEnqueuer, eventBroker *events.Broker, sandboxService *SandboxService, gitService *GitService) *ChatService {
	return &ChatService{
		store:          s,
		sessionService: sessionService,
		jobEnqueuer:    jobEnqueuer,
		eventBroker:    eventBroker,
		sandboxService: sandboxService,
		gitService:     gitService,
	}
}

// NewSessionRequest contains the parameters for creating a new chat session.
type NewSessionRequest struct {
	// SessionID is the client-provided session ID (required)
	SessionID   string
	ProjectID   string
	WorkspaceID string
	// Messages is the UIMessage array from the client — each element is a raw JSON object.
	Messages []json.RawMessage
}

// CancelCompletionResponse represents the response from cancelling a completion.
type CancelCompletionResponse struct {
	Success      bool   `json:"success"`
	CompletionID string `json:"completionId"`
	Status       string `json:"status"`
}

// ErrNoActiveCompletion is returned when attempting to cancel with no active completion.
var ErrNoActiveCompletion = errors.New("no active completion to cancel")

// NewSession creates a new chat session and enqueues initialization.
// Uses the client-provided session ID.
func (c *ChatService) NewSession(ctx context.Context, req NewSessionRequest) (string, error) {
	if req.SessionID == "" {
		return "", fmt.Errorf("session ID is required")
	}

	// Validate workspace belongs to project
	workspace, err := c.store.GetWorkspaceByID(ctx, req.WorkspaceID)
	if err != nil {
		return "", fmt.Errorf("workspace not found: %w", err)
	}
	if workspace.ProjectID != req.ProjectID {
		return "", fmt.Errorf("workspace does not belong to this project")
	}

	// Use SessionService to create the session with client-provided ID
	sess, err := c.sessionService.CreateSessionWithID(ctx, req.SessionID, req.ProjectID, req.WorkspaceID, "")
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}

	// Enqueue session initialization job (non-blocking)
	if err := c.jobEnqueuer.Enqueue(ctx, jobs.SessionInitPayload{
		ProjectID:   req.ProjectID,
		SessionID:   sess.ID,
		WorkspaceID: req.WorkspaceID,
	}); err != nil {
		// Log but don't fail - session was created, init can be retried
		fmt.Printf("Warning: failed to enqueue session init for %s: %v\n", sess.ID, err)
	}

	return sess.ID, nil
}

// GetSession retrieves a session and validates it belongs to the project.
func (c *ChatService) GetSession(ctx context.Context, projectID, sessionID string) (*model.Session, error) {
	sess, err := c.store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}
	if sess.ProjectID != projectID {
		return nil, fmt.Errorf("session does not belong to this project")
	}
	return sess, nil
}

// GetSessionByID retrieves a session by ID without project validation.
// Use this when you need to check existence before validating project ownership.
func (c *ChatService) GetSessionByID(ctx context.Context, sessionID string) (*model.Session, error) {
	return c.store.GetSessionByID(ctx, sessionID)
}

// ValidateSessionResources validates that a session's workspace belongs to the project.
func (c *ChatService) ValidateSessionResources(ctx context.Context, projectID string, session *model.Session) error {
	// Validate workspace belongs to project
	workspace, err := c.store.GetWorkspaceByID(ctx, session.WorkspaceID)
	if err != nil {
		return fmt.Errorf("workspace not found: %w", err)
	}
	if workspace.ProjectID != projectID {
		return fmt.Errorf("session's workspace does not belong to this project")
	}

	return nil
}

type preparedChatRequest struct {
	client  *SessionClient
	modelID string
	opts    *RequestOptions
}

func (c *ChatService) prepareChatRequest(ctx context.Context, projectID, sessionID string, requestModel string, reasoning string, mode string) (*preparedChatRequest, error) {
	// Validate session belongs to project
	if _, err := c.GetSession(ctx, projectID, sessionID); err != nil {
		return nil, err
	}

	if c.sandboxService == nil {
		return nil, fmt.Errorf("sandbox provider not available")
	}

	client, err := c.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	return &preparedChatRequest{
		client:  client,
		modelID: requestModel,
		opts: &RequestOptions{
			Reasoning: reasoning,
			Mode:      mode,
		},
	}, nil
}

// StartChat sends messages to the sandbox and returns the message ID and completion metadata.
func (c *ChatService) StartChat(ctx context.Context, projectID, sessionID, threadID string, messages []json.RawMessage, requestModel string, reasoning string, mode string) (string, *sandboxapi.ChatStartedResponse, error) {
	messageID := lastUserMessageID(messages)

	rawMessages, err := json.Marshal(messages)
	if err != nil {
		return "", nil, fmt.Errorf("failed to marshal messages: %w", err)
	}
	prepared, err := c.prepareChatRequest(ctx, projectID, sessionID, requestModel, reasoning, mode)
	if err != nil {
		return "", nil, err
	}
	started, err := prepared.client.StartChat(ctx, threadID, rawMessages, prepared.modelID, prepared.opts)
	if err != nil {
		return "", nil, err
	}
	if err := c.sessionService.ClearTerminalCommitState(ctx, projectID, sessionID); err != nil {
		log.Printf("Warning: failed to clear terminal commit state for %s: %v", sessionID, err)
	}
	return messageID, started, nil
}

// SendToSandbox sends messages to the sandbox and returns a channel of raw SSE lines.
// The sandbox handles message storage - we just proxy the stream without parsing.
// Both messages and responses are passed through as raw data.
// Credentials for the project are automatically included in the request header.
// Git user configuration is automatically included in request headers (cached on first use).
// If the sandbox is not running or doesn't exist, it will be reconciled on-demand.
// Reasoning can be any supported reasoning level string (for example "auto",
// "low", "medium", "high", "xhigh", "none", "default") or "" for
// model/provider default behavior.
// mode can be "plan" for planning mode, or "" for default (build mode).
func (c *ChatService) SendToSandbox(ctx context.Context, projectID, sessionID, threadID string, messages json.RawMessage, requestModel string, reasoning string, mode string) (<-chan SSELine, error) {
	prepared, err := c.prepareChatRequest(ctx, projectID, sessionID, requestModel, reasoning, mode)
	if err != nil {
		return nil, err
	}

	innerCh, err := prepared.client.SendMessages(ctx, threadID, messages, prepared.modelID, prepared.opts)
	if err != nil {
		return nil, err
	}
	if err := c.sessionService.ClearTerminalCommitState(ctx, projectID, sessionID); err != nil {
		log.Printf("Warning: failed to clear terminal commit state for %s: %v", sessionID, err)
	}

	return innerCh, nil
}

// GetStream returns a channel of SSE events for a thread.
// Fresh requests replay persisted history by default; valid Last-Event-ID
// reconnects continue from the requested offset.
// The sandbox is automatically reconciled if not running.
func (c *ChatService) GetStream(ctx context.Context, projectID, sessionID, threadID, lastEventID string) (<-chan SSELine, error) {
	if _, err := c.GetSession(ctx, projectID, sessionID); err != nil {
		return nil, err
	}
	if c.sandboxService == nil {
		return nil, fmt.Errorf("sandbox provider not available")
	}
	client, err := c.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	return client.GetStream(ctx, threadID, &RequestOptions{LastEventID: lastEventID})
}

// ListThreads retrieves all threads for a session from the sandbox agent.
func (c *ChatService) ListThreads(ctx context.Context, projectID, sessionID string) (*sandboxapi.ListThreadsResponse, error) {
	if _, err := c.GetSession(ctx, projectID, sessionID); err != nil {
		return nil, err
	}
	if c.sandboxService == nil {
		return nil, fmt.Errorf("sandbox provider not available")
	}
	client, err := c.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	return client.ListThreads(ctx)
}

// GetThread retrieves a single thread for a session from the sandbox agent.
func (c *ChatService) GetThread(ctx context.Context, projectID, sessionID, threadID string) (*sandboxapi.Thread, error) {
	if _, err := c.GetSession(ctx, projectID, sessionID); err != nil {
		return nil, err
	}
	if c.sandboxService == nil {
		return nil, fmt.Errorf("sandbox provider not available")
	}
	client, err := c.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	return client.GetThread(ctx, threadID)
}

// CreateThread creates a thread for a session in the sandbox agent.
func (c *ChatService) CreateThread(ctx context.Context, projectID, sessionID string, req *sandboxapi.CreateThreadRequest) (*sandboxapi.Thread, error) {
	if _, err := c.GetSession(ctx, projectID, sessionID); err != nil {
		return nil, err
	}
	if c.sandboxService == nil {
		return nil, fmt.Errorf("sandbox provider not available")
	}
	client, err := c.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	return client.CreateThread(ctx, req)
}

// UpdateThread updates a thread for a session in the sandbox agent.
func (c *ChatService) UpdateThread(ctx context.Context, projectID, sessionID, threadID string, req *sandboxapi.UpdateThreadRequest) (*sandboxapi.Thread, error) {
	if _, err := c.GetSession(ctx, projectID, sessionID); err != nil {
		return nil, err
	}
	if c.sandboxService == nil {
		return nil, fmt.Errorf("sandbox provider not available")
	}
	client, err := c.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	return client.UpdateThread(ctx, threadID, req)
}

// DeleteQueuedPrompt removes a queued prompt for a thread in the sandbox agent.
func (c *ChatService) DeleteQueuedPrompt(ctx context.Context, projectID, sessionID, threadID, queuedPromptID string) (*sandboxapi.DeleteQueuedPromptResponse, error) {
	if _, err := c.GetSession(ctx, projectID, sessionID); err != nil {
		return nil, err
	}
	if c.sandboxService == nil {
		return nil, fmt.Errorf("sandbox provider not available")
	}
	client, err := c.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	return client.DeleteQueuedPrompt(ctx, threadID, queuedPromptID)
}

// DeleteThread deletes a thread for a session in the sandbox agent.
func (c *ChatService) DeleteThread(ctx context.Context, projectID, sessionID, threadID string) (*sandboxapi.DeleteThreadResponse, error) {
	if _, err := c.GetSession(ctx, projectID, sessionID); err != nil {
		return nil, err
	}
	if c.sandboxService == nil {
		return nil, fmt.Errorf("sandbox provider not available")
	}
	client, err := c.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	return client.DeleteThread(ctx, threadID)
}

// CancelCompletion cancels an in-progress chat completion in the sandbox.
// Returns ErrNoActiveCompletion if no completion is active.
// The sandbox is automatically reconciled if not running.
func (c *ChatService) CancelCompletion(ctx context.Context, projectID, sessionID, threadID string) (*CancelCompletionResponse, error) {
	if _, err := c.GetSession(ctx, projectID, sessionID); err != nil {
		return nil, err
	}
	if c.sandboxService == nil {
		return nil, fmt.Errorf("sandbox provider not available")
	}
	client, err := c.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return client.CancelCompletion(ctx, threadID)
}

// ============================================================================
// AskUserQuestion Methods
// ============================================================================

// GetQuestion returns the current pending AskUserQuestion from the sandbox.
// When toolUseID is non-empty, queries for a specific question by approval ID.
// Returns nil question if no question is waiting.
func (c *ChatService) GetQuestion(ctx context.Context, projectID, sessionID, threadID, toolUseID string) (*sandboxapi.PendingQuestionResponse, error) {
	if _, err := c.GetSession(ctx, projectID, sessionID); err != nil {
		return nil, err
	}
	if c.sandboxService == nil {
		return nil, fmt.Errorf("sandbox provider not available")
	}
	client, err := c.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return client.GetQuestion(ctx, threadID, toolUseID)
}

// AnswerQuestion submits answers to a pending AskUserQuestion.
func (c *ChatService) AnswerQuestion(ctx context.Context, projectID, sessionID, threadID string, req *sandboxapi.AnswerQuestionRequest) (*sandboxapi.AnswerQuestionResponse, error) {
	if _, err := c.GetSession(ctx, projectID, sessionID); err != nil {
		return nil, err
	}
	if c.sandboxService == nil {
		return nil, fmt.Errorf("sandbox provider not available")
	}
	client, err := c.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return client.AnswerQuestion(ctx, threadID, req)
}

// ============================================================================
// File System Methods
// ============================================================================

// ListFiles lists directory contents in the sandbox.
// The sandbox is automatically reconciled if not running.
func (c *ChatService) ListFiles(ctx context.Context, projectID, sessionID, path string, includeHidden bool) (*sandboxapi.ListFilesResponse, error) {
	if _, err := c.GetSession(ctx, projectID, sessionID); err != nil {
		return nil, err
	}
	if c.sandboxService == nil {
		return nil, fmt.Errorf("sandbox provider not available")
	}
	client, err := c.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return client.ListFiles(ctx, path, includeHidden)
}

// SearchFiles performs a fuzzy search over workspace files in the sandbox.
// The sandbox is automatically reconciled if not running.
func (c *ChatService) SearchFiles(ctx context.Context, projectID, sessionID, query string, limit int) (*sandboxapi.SearchFilesResponse, error) {
	if _, err := c.GetSession(ctx, projectID, sessionID); err != nil {
		return nil, err
	}
	if c.sandboxService == nil {
		return nil, fmt.Errorf("sandbox provider not available")
	}
	client, err := c.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return client.SearchFiles(ctx, query, limit)
}

// ReadFile reads file content from the sandbox.
// The sandbox is automatically reconciled if not running.
func (c *ChatService) ReadFile(ctx context.Context, projectID, sessionID, path string) (*sandboxapi.ReadFileResponse, error) {
	if _, err := c.GetSession(ctx, projectID, sessionID); err != nil {
		return nil, err
	}
	if c.sandboxService == nil {
		return nil, fmt.Errorf("sandbox provider not available")
	}
	client, err := c.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return client.ReadFile(ctx, path)
}

// ReadFileFromBase reads a file from the base commit (for deleted files).
// This is useful for displaying diffs of deleted files.
func (c *ChatService) ReadFileFromBase(ctx context.Context, projectID, sessionID, path string) (*sandboxapi.ReadFileResponse, error) {
	// Validate session belongs to project
	session, err := c.GetSession(ctx, projectID, sessionID)
	if err != nil {
		return nil, err
	}

	if c.gitService == nil {
		return nil, fmt.Errorf("git service not available")
	}

	// Get workspace to find base commit
	workspace, err := c.store.GetWorkspaceByID(ctx, session.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get workspace: %w", err)
	}

	// Use base commit from session if available, otherwise fetch current git HEAD
	var baseCommit string
	if session.BaseCommit != nil {
		baseCommit = *session.BaseCommit
	} else {
		// Fetch current git HEAD as the base commit
		gitStatus, err := c.gitService.Status(ctx, workspace.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get workspace git status: %w", err)
		}
		if gitStatus.Commit == "" {
			return nil, fmt.Errorf("workspace has no commit")
		}
		baseCommit = gitStatus.Commit
	}

	// Read file from git at base commit
	content, err := c.gitService.ReadFile(ctx, workspace.ID, baseCommit, path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file from base commit: %w", err)
	}

	return &sandboxapi.ReadFileResponse{
		Content:  string(content),
		Encoding: "utf-8",
		Size:     int64(len(content)),
	}, nil
}

// WriteFile writes file content to the sandbox.
// The sandbox is automatically reconciled if not running.
func (c *ChatService) WriteFile(ctx context.Context, projectID, sessionID string, req *sandboxapi.WriteFileRequest) (*sandboxapi.WriteFileResponse, error) {
	if _, err := c.GetSession(ctx, projectID, sessionID); err != nil {
		return nil, err
	}
	if c.sandboxService == nil {
		return nil, fmt.Errorf("sandbox provider not available")
	}
	client, err := c.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return client.WriteFile(ctx, req)
}

// DeleteFile deletes a file or directory in the sandbox.
// The sandbox is automatically reconciled if not running.
func (c *ChatService) DeleteFile(ctx context.Context, projectID, sessionID string, req *sandboxapi.DeleteFileRequest) (*sandboxapi.DeleteFileResponse, error) {
	if _, err := c.GetSession(ctx, projectID, sessionID); err != nil {
		return nil, err
	}
	if c.sandboxService == nil {
		return nil, fmt.Errorf("sandbox provider not available")
	}
	client, err := c.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return client.DeleteFile(ctx, req)
}

// RenameFile renames/moves a file or directory in the sandbox.
// The sandbox is automatically reconciled if not running.
func (c *ChatService) RenameFile(ctx context.Context, projectID, sessionID string, req *sandboxapi.RenameFileRequest) (*sandboxapi.RenameFileResponse, error) {
	if _, err := c.GetSession(ctx, projectID, sessionID); err != nil {
		return nil, err
	}
	if c.sandboxService == nil {
		return nil, fmt.Errorf("sandbox provider not available")
	}
	client, err := c.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return client.RenameFile(ctx, req)
}

// GetDiff retrieves diff information from the sandbox.
// If path is non-empty, returns a single file diff.
// If format is "files", returns just file paths.
// Otherwise returns full diff with patches.
// The agent-api calculates the merge-base automatically.
// The sandbox is automatically reconciled if not running.
func (c *ChatService) GetDiff(ctx context.Context, projectID, sessionID, path, format string) (any, error) {
	if _, err := c.GetSession(ctx, projectID, sessionID); err != nil {
		return nil, err
	}
	if c.sandboxService == nil {
		return nil, fmt.Errorf("sandbox provider not available")
	}
	client, err := c.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return client.GetDiff(ctx, path, format)
}

// ============================================================================
// Hook Methods
// ============================================================================

// GetHooksStatus retrieves hook evaluation status from the sandbox.
// The sandbox is automatically reconciled if not running.
func (c *ChatService) GetHooksStatus(ctx context.Context, projectID, sessionID string) (*sandboxapi.HooksStatusResponse, error) {
	if _, err := c.GetSession(ctx, projectID, sessionID); err != nil {
		return nil, err
	}
	if c.sandboxService == nil {
		return nil, fmt.Errorf("sandbox provider not available")
	}
	client, err := c.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return client.GetHooksStatus(ctx)
}

// GetHookOutput retrieves the output log for a specific hook from the sandbox.
// The sandbox is automatically reconciled if not running.
func (c *ChatService) GetHookOutput(ctx context.Context, projectID, sessionID, hookID string) (*sandboxapi.HookOutputResponse, error) {
	if _, err := c.GetSession(ctx, projectID, sessionID); err != nil {
		return nil, err
	}
	if c.sandboxService == nil {
		return nil, fmt.Errorf("sandbox provider not available")
	}
	client, err := c.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return client.GetHookOutput(ctx, hookID)
}

// DownloadHookOutput retrieves the full output log for a specific hook from the sandbox.
// The sandbox is automatically reconciled if not running.
func (c *ChatService) DownloadHookOutput(ctx context.Context, projectID, sessionID, hookID string) ([]byte, error) {
	if _, err := c.GetSession(ctx, projectID, sessionID); err != nil {
		return nil, err
	}
	if c.sandboxService == nil {
		return nil, fmt.Errorf("sandbox provider not available")
	}
	client, err := c.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return client.DownloadHookOutput(ctx, hookID)
}

// RerunHook manually reruns a specific hook in the sandbox.
// The sandbox is automatically reconciled if not running.
func (c *ChatService) RerunHook(ctx context.Context, projectID, sessionID, hookID string) (*sandboxapi.HookRerunResponse, error) {
	if _, err := c.GetSession(ctx, projectID, sessionID); err != nil {
		return nil, err
	}
	if c.sandboxService == nil {
		return nil, fmt.Errorf("sandbox provider not available")
	}
	client, err := c.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return client.RerunHook(ctx, hookID)
}

// ============================================================================
// Service Methods
// ============================================================================

// ListServices retrieves all services from the sandbox.
// The sandbox is automatically reconciled if not running.
func (c *ChatService) ListServices(ctx context.Context, projectID, sessionID string) (*sandboxapi.ListServicesResponse, error) {
	if _, err := c.GetSession(ctx, projectID, sessionID); err != nil {
		return nil, err
	}
	if c.sandboxService == nil {
		return nil, fmt.Errorf("sandbox provider not available")
	}
	client, err := c.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return client.ListServices(ctx)
}

// StartService starts a service in the sandbox.
// The sandbox is automatically reconciled if not running.
func (c *ChatService) StartService(ctx context.Context, projectID, sessionID, serviceID string) (*sandboxapi.StartServiceResponse, error) {
	if _, err := c.GetSession(ctx, projectID, sessionID); err != nil {
		return nil, err
	}
	if c.sandboxService == nil {
		return nil, fmt.Errorf("sandbox provider not available")
	}
	client, err := c.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return client.StartService(ctx, serviceID)
}

// StopService stops a service in the sandbox.
// The sandbox is automatically reconciled if not running.
func (c *ChatService) StopService(ctx context.Context, projectID, sessionID, serviceID string) (*sandboxapi.StopServiceResponse, error) {
	if _, err := c.GetSession(ctx, projectID, sessionID); err != nil {
		return nil, err
	}
	if c.sandboxService == nil {
		return nil, fmt.Errorf("sandbox provider not available")
	}
	client, err := c.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return client.StopService(ctx, serviceID)
}

// GetServiceOutput returns a channel of SSE events for a service's output.
// The sandbox is automatically reconciled if not running.
func (c *ChatService) GetServiceOutput(ctx context.Context, projectID, sessionID, serviceID string) (<-chan SSELine, error) {
	if _, err := c.GetSession(ctx, projectID, sessionID); err != nil {
		return nil, err
	}
	if c.sandboxService == nil {
		return nil, fmt.Errorf("sandbox provider not available")
	}
	client, err := c.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return client.GetServiceOutput(ctx, serviceID)
}

// lastUserMessageID returns the ID of the last user message in the slice, or "".
func lastUserMessageID(messages []json.RawMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		var m struct {
			ID   string `json:"id"`
			Role string `json:"role"`
		}
		if json.Unmarshal(messages[i], &m) == nil && m.Role == "user" && m.ID != "" {
			return m.ID
		}
	}
	return ""
}
