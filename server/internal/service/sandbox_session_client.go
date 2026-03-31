package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/obot-platform/discobot/server/internal/conntrack"
	"github.com/obot-platform/discobot/server/internal/sandbox"
	"github.com/obot-platform/discobot/server/internal/sandbox/sandboxapi"
)

// SessionInitializer breaks the circular dependency between SandboxService and SessionService.
// SessionService implements this interface via its existing Initialize method.
type SessionInitializer interface {
	Initialize(ctx context.Context, sessionID string) error
}

// SessionClient is a session-bound wrapper around SandboxChatClient.
// It removes the need to pass sessionID on every call and automatically
// reconciles the sandbox on unavailability errors.
type SessionClient struct {
	sessionID       string
	inner           *SandboxChatClient
	sandboxSvc      *SandboxService
	activityTracker func(string)
	connTracker     *conntrack.Tracker
}

// trackStream wraps ch so that the session is registered as having an active
// connection for the full lifetime of the stream. This prevents the idle monitor
// from stopping the sandbox while data is still flowing, even if the completion
// finished more than idleTimeout ago.
// If connTracker is nil the original channel is returned unchanged.
func (c *SessionClient) trackStream(ch <-chan SSELine) <-chan SSELine {
	if c.connTracker == nil {
		return ch
	}
	release := c.connTracker.Track(c.sessionID)
	wrapped := make(chan SSELine)
	go func() {
		defer release()
		defer close(wrapped)
		for line := range ch {
			wrapped <- line
		}
	}()
	return wrapped
}

// withReconciliation wraps a sandbox operation with error handling that
// triggers reconciliation on sandbox unavailable errors, then retries once.
// On successful operations, it records activity for idle timeout tracking.
func withReconciliation[T any](ctx context.Context, c *SessionClient, operation func() (T, error)) (T, error) {
	result, err := operation()
	if err == nil {
		// Record activity on successful operation
		if c.activityTracker != nil {
			c.activityTracker(c.sessionID)
		}
		return result, nil
	}

	if errors.Is(err, sandbox.ErrNotFound) || errors.Is(err, sandbox.ErrNotRunning) || isSandboxUnavailableError(err) {
		log.Printf("Sandbox unavailable for session %s, reconciling: %v", c.sessionID, err)

		if reconcileErr := c.sandboxSvc.ReconcileSandbox(ctx, c.sessionID); reconcileErr != nil {
			var zero T
			return zero, fmt.Errorf("sandbox unavailable and failed to reconcile: %w", reconcileErr)
		}

		// Retry the operation after reconciliation
		result, err = operation()
		if err == nil && c.activityTracker != nil {
			// Record activity on successful retry
			c.activityTracker(c.sessionID)
		}
		return result, err
	}

	var zero T
	return zero, err
}

// isSandboxUnavailableError checks if the error indicates the sandbox is unavailable
// and should be recreated.
func isSandboxUnavailableError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "sandbox not found") ||
		strings.Contains(errStr, "sandbox is not running") ||
		strings.Contains(errStr, "container not found") ||
		strings.Contains(errStr, "No such container")
}

// StartChat sends messages to the sandbox and returns completion metadata.
func (c *SessionClient) StartChat(ctx context.Context, threadID string, messages json.RawMessage, model string, opts *RequestOptions) (*sandboxapi.ChatStartedResponse, error) {
	return withReconciliation(ctx, c, func() (*sandboxapi.ChatStartedResponse, error) {
		return c.inner.StartChat(ctx, c.sessionID, threadID, messages, model, opts)
	})
}

// SendMessages sends messages to the sandbox.
func (c *SessionClient) SendMessages(ctx context.Context, threadID string, messages json.RawMessage, model string, opts *RequestOptions) (<-chan SSELine, error) {
	ch, err := withReconciliation(ctx, c, func() (<-chan SSELine, error) {
		return c.inner.SendMessages(ctx, c.sessionID, threadID, messages, model, opts)
	})
	if err != nil {
		return nil, err
	}
	return c.trackStream(ch), nil
}

// GetStream returns a channel of SSE events for a thread.
func (c *SessionClient) GetStream(ctx context.Context, threadID string, opts *RequestOptions) (<-chan SSELine, error) {
	ch, err := withReconciliation(ctx, c, func() (<-chan SSELine, error) {
		return c.inner.GetStream(ctx, c.sessionID, threadID, opts)
	})
	if err != nil {
		return nil, err
	}
	return c.trackStream(ch), nil
}

// ListThreads retrieves all threads from the sandbox.
func (c *SessionClient) ListThreads(ctx context.Context) (*sandboxapi.ListThreadsResponse, error) {
	return withReconciliation(ctx, c, func() (*sandboxapi.ListThreadsResponse, error) {
		return c.inner.ListThreads(ctx, c.sessionID)
	})
}

// GetThread retrieves a specific thread from the sandbox.
func (c *SessionClient) GetThread(ctx context.Context, threadID string) (*sandboxapi.Thread, error) {
	return withReconciliation(ctx, c, func() (*sandboxapi.Thread, error) {
		return c.inner.GetThread(ctx, c.sessionID, threadID)
	})
}

// CreateThread creates a new thread in the sandbox.
func (c *SessionClient) CreateThread(ctx context.Context, req *sandboxapi.CreateThreadRequest) (*sandboxapi.Thread, error) {
	return withReconciliation(ctx, c, func() (*sandboxapi.Thread, error) {
		return c.inner.CreateThread(ctx, c.sessionID, req)
	})
}

// UpdateThread updates a thread in the sandbox.
func (c *SessionClient) UpdateThread(ctx context.Context, threadID string, req *sandboxapi.UpdateThreadRequest) (*sandboxapi.Thread, error) {
	return withReconciliation(ctx, c, func() (*sandboxapi.Thread, error) {
		return c.inner.UpdateThread(ctx, c.sessionID, threadID, req)
	})
}

// DeleteQueuedPrompt removes a queued prompt from a thread in the sandbox.
func (c *SessionClient) DeleteQueuedPrompt(ctx context.Context, threadID, queuedPromptID string) (*sandboxapi.DeleteQueuedPromptResponse, error) {
	return withReconciliation(ctx, c, func() (*sandboxapi.DeleteQueuedPromptResponse, error) {
		return c.inner.DeleteQueuedPrompt(ctx, c.sessionID, threadID, queuedPromptID)
	})
}

// DeleteThread removes a thread from the sandbox.
func (c *SessionClient) DeleteThread(ctx context.Context, threadID string) (*sandboxapi.DeleteThreadResponse, error) {
	return withReconciliation(ctx, c, func() (*sandboxapi.DeleteThreadResponse, error) {
		return c.inner.DeleteThread(ctx, c.sessionID, threadID)
	})
}

// GetChatStatus retrieves the completion status from the sandbox.
func (c *SessionClient) GetChatStatus(ctx context.Context) (*sandboxapi.ChatStatusResponse, error) {
	return withReconciliation(ctx, c, func() (*sandboxapi.ChatStatusResponse, error) {
		return c.inner.GetChatStatus(ctx, c.sessionID)
	})
}

// CancelCompletion cancels an in-progress completion in the sandbox.
func (c *SessionClient) CancelCompletion(ctx context.Context, threadID string) (*CancelCompletionResponse, error) {
	return withReconciliation(ctx, c, func() (*CancelCompletionResponse, error) {
		return c.inner.CancelCompletion(ctx, c.sessionID, threadID)
	})
}

// GetQuestion returns the pending AskUserQuestion, or nil question if none is waiting.
// When toolUseID is non-empty, queries for a specific question by approval ID.
func (c *SessionClient) GetQuestion(ctx context.Context, threadID string, toolUseID string) (*sandboxapi.PendingQuestionResponse, error) {
	return withReconciliation(ctx, c, func() (*sandboxapi.PendingQuestionResponse, error) {
		return c.inner.GetQuestion(ctx, c.sessionID, threadID, toolUseID)
	})
}

// AnswerQuestion submits the user's answer to a pending AskUserQuestion.
func (c *SessionClient) AnswerQuestion(ctx context.Context, threadID string, req *sandboxapi.AnswerQuestionRequest) (*sandboxapi.AnswerQuestionResponse, error) {
	return withReconciliation(ctx, c, func() (*sandboxapi.AnswerQuestionResponse, error) {
		return c.inner.AnswerQuestion(ctx, c.sessionID, threadID, req)
	})
}

// ListFiles lists directory contents in the sandbox.
func (c *SessionClient) ListFiles(ctx context.Context, path string, includeHidden bool) (*sandboxapi.ListFilesResponse, error) {
	return withReconciliation(ctx, c, func() (*sandboxapi.ListFilesResponse, error) {
		return c.inner.ListFiles(ctx, c.sessionID, path, includeHidden)
	})
}

// SearchFiles performs a fuzzy search over workspace files in the sandbox.
func (c *SessionClient) SearchFiles(ctx context.Context, query string, limit int) (*sandboxapi.SearchFilesResponse, error) {
	return withReconciliation(ctx, c, func() (*sandboxapi.SearchFilesResponse, error) {
		return c.inner.SearchFiles(ctx, c.sessionID, query, limit)
	})
}

// ReadFile reads file content from the sandbox.
func (c *SessionClient) ReadFile(ctx context.Context, path string) (*sandboxapi.ReadFileResponse, error) {
	return withReconciliation(ctx, c, func() (*sandboxapi.ReadFileResponse, error) {
		return c.inner.ReadFile(ctx, c.sessionID, path)
	})
}

// WriteFile writes file content to the sandbox.
func (c *SessionClient) WriteFile(ctx context.Context, req *sandboxapi.WriteFileRequest) (*sandboxapi.WriteFileResponse, error) {
	return withReconciliation(ctx, c, func() (*sandboxapi.WriteFileResponse, error) {
		return c.inner.WriteFile(ctx, c.sessionID, req)
	})
}

// DeleteFile deletes a file or directory in the sandbox.
func (c *SessionClient) DeleteFile(ctx context.Context, req *sandboxapi.DeleteFileRequest) (*sandboxapi.DeleteFileResponse, error) {
	return withReconciliation(ctx, c, func() (*sandboxapi.DeleteFileResponse, error) {
		return c.inner.DeleteFile(ctx, c.sessionID, req)
	})
}

// RenameFile renames/moves a file or directory in the sandbox.
func (c *SessionClient) RenameFile(ctx context.Context, req *sandboxapi.RenameFileRequest) (*sandboxapi.RenameFileResponse, error) {
	return withReconciliation(ctx, c, func() (*sandboxapi.RenameFileResponse, error) {
		return c.inner.RenameFile(ctx, c.sessionID, req)
	})
}

// GetDiff retrieves diff information from the sandbox.
func (c *SessionClient) GetDiff(ctx context.Context, path, format string) (any, error) {
	return withReconciliation(ctx, c, func() (any, error) {
		return c.inner.GetDiff(ctx, c.sessionID, path, format)
	})
}

// GetCommits retrieves a serialized commit replay bundle from the sandbox.
func (c *SessionClient) GetCommits(ctx context.Context, parentCommit string) (*sandboxapi.CommitsResponse, error) {
	return withReconciliation(ctx, c, func() (*sandboxapi.CommitsResponse, error) {
		return c.inner.GetCommits(ctx, c.sessionID, parentCommit)
	})
}

// GetUserInfo retrieves the default user info from the sandbox.
func (c *SessionClient) GetUserInfo(ctx context.Context) (*sandboxapi.UserResponse, error) {
	return withReconciliation(ctx, c, func() (*sandboxapi.UserResponse, error) {
		return c.inner.GetUserInfo(ctx, c.sessionID)
	})
}

// GetModels retrieves available models from the Claude API via the sandbox.
func (c *SessionClient) GetModels(ctx context.Context) (*sandboxapi.ModelsResponse, error) {
	return withReconciliation(ctx, c, func() (*sandboxapi.ModelsResponse, error) {
		return c.inner.GetModels(ctx, c.sessionID)
	})
}

// GetHooksStatus retrieves hook evaluation status from the sandbox.
func (c *SessionClient) GetHooksStatus(ctx context.Context) (*sandboxapi.HooksStatusResponse, error) {
	return withReconciliation(ctx, c, func() (*sandboxapi.HooksStatusResponse, error) {
		return c.inner.GetHooksStatus(ctx, c.sessionID)
	})
}

// GetHookOutput retrieves the output log for a specific hook from the sandbox.
func (c *SessionClient) GetHookOutput(ctx context.Context, hookID string) (*sandboxapi.HookOutputResponse, error) {
	return withReconciliation(ctx, c, func() (*sandboxapi.HookOutputResponse, error) {
		return c.inner.GetHookOutput(ctx, c.sessionID, hookID)
	})
}

// RerunHook manually reruns a specific hook in the sandbox.
func (c *SessionClient) RerunHook(ctx context.Context, hookID string) (*sandboxapi.HookRerunResponse, error) {
	return withReconciliation(ctx, c, func() (*sandboxapi.HookRerunResponse, error) {
		return c.inner.RerunHook(ctx, c.sessionID, hookID)
	})
}

// ListServices retrieves all services from the sandbox.
func (c *SessionClient) ListServices(ctx context.Context) (*sandboxapi.ListServicesResponse, error) {
	return withReconciliation(ctx, c, func() (*sandboxapi.ListServicesResponse, error) {
		return c.inner.ListServices(ctx, c.sessionID)
	})
}

// StartService starts a service in the sandbox.
func (c *SessionClient) StartService(ctx context.Context, serviceID string) (*sandboxapi.StartServiceResponse, error) {
	return withReconciliation(ctx, c, func() (*sandboxapi.StartServiceResponse, error) {
		return c.inner.StartService(ctx, c.sessionID, serviceID)
	})
}

// StopService stops a service in the sandbox.
func (c *SessionClient) StopService(ctx context.Context, serviceID string) (*sandboxapi.StopServiceResponse, error) {
	return withReconciliation(ctx, c, func() (*sandboxapi.StopServiceResponse, error) {
		return c.inner.StopService(ctx, c.sessionID, serviceID)
	})
}

// GetServiceOutput returns a channel of SSE events for a service's output.
func (c *SessionClient) GetServiceOutput(ctx context.Context, serviceID string) (<-chan SSELine, error) {
	ch, err := withReconciliation(ctx, c, func() (<-chan SSELine, error) {
		return c.inner.GetServiceOutput(ctx, c.sessionID, serviceID)
	})
	if err != nil {
		return nil, err
	}
	return c.trackStream(ch), nil
}
