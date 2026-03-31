// Package api defines the request/response types for the agent HTTP API.
//
// These types must be kept in sync with:
//   - TypeScript agent: agent-api/src/api/types.ts
//   - Go server sandbox types: server/internal/sandbox/sandboxapi/types.go
package api //nolint:revive

import (
	"github.com/obot-platform/discobot/agent-go/message"
)

// ============================================================================
// Request Types
// ============================================================================

// ChatRequest is the POST /threads/{id}/chat request body.
type ChatRequest struct {
	Messages  []message.UIMessage `json:"messages"`
	Model     string              `json:"model,omitempty"`
	Reasoning string              `json:"reasoning,omitempty"` // "", "auto", "low", "medium", "high", "xhigh", "none", "default"
	Mode      string              `json:"mode,omitempty"`      // "" keeps current thread mode (or defaults to build), "plan" sets plan, "build" sets build
}

// WriteFileRequest is the POST /files/write request body.
type WriteFileRequest struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Encoding string `json:"encoding,omitempty"` // defaults to "utf8"
}

// DeleteFileRequest is the POST /files/delete request body.
type DeleteFileRequest struct {
	Path string `json:"path"`
}

// RenameFileRequest is the POST /files/rename request body.
type RenameFileRequest struct {
	OldPath string `json:"oldPath"`
	NewPath string `json:"newPath"`
}

// AnswerQuestionRequest is the POST /threads/{id}/chat/answer/{questionId} request body.
type AnswerQuestionRequest struct {
	Answers map[string]string `json:"answers"`
}

// ============================================================================
// Response Types
// ============================================================================

// RootResponse is the GET / response.
type RootResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
}

// HealthResponse is the GET /health response.
type HealthResponse struct {
	Healthy   bool `json:"healthy"`
	Connected bool `json:"connected"`
}

// UserResponse is the GET /user response.
type UserResponse struct {
	Username string `json:"username"`
	UID      int    `json:"uid"`
	GID      int    `json:"gid"`
}

// ErrorResponse is returned for 4xx/5xx errors.
type ErrorResponse struct {
	Error string `json:"error"`
}

// Thread represents a single conversation thread.
type Thread struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	LastMessage string         `json:"lastMessage,omitempty"`
	Model       string         `json:"model,omitempty"`     // full "providerId/modelId" ref
	Reasoning   string         `json:"reasoning,omitempty"` // "", "auto", "low", "medium", "high", "xhigh", "none", "default"
	Mode        string         `json:"mode"`                // "build" or "plan"
	State       string         `json:"state,omitempty"`     // "interrupted" or "cancelled"
	PromptQueue []QueuedPrompt `json:"promptQueue,omitempty"`
}

type QueuedPrompt struct {
	ID        string            `json:"id"`
	CreatedAt string            `json:"createdAt,omitempty"`
	Message   message.UIMessage `json:"message"`
	Model     string            `json:"model,omitempty"`
	Reasoning string            `json:"reasoning,omitempty"`
	Mode      string            `json:"mode,omitempty"`
}

// ListThreadsResponse is the GET /threads response.
type ListThreadsResponse struct {
	Threads []Thread `json:"threads"`
}

// CreateThreadRequest is the POST /threads request body.
type CreateThreadRequest struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// UpdateThreadRequest is the PUT /threads/{id} request body.
type UpdateThreadRequest struct {
	Name string `json:"name"`
}

// DeleteThreadResponse is the DELETE /threads/{id} response body.
type DeleteThreadResponse struct {
	Success bool `json:"success"`
}

// DeleteQueuedPromptResponse is the DELETE /threads/{id}/queue/{queueId} response body.
type DeleteQueuedPromptResponse struct {
	Success bool `json:"success"`
}

// ChatStatusResponse is the GET /threads/{id}/chat/status response.
type ChatStatusResponse struct {
	IsRunning bool `json:"isRunning"`
}

// ChatStartedResponse is the POST /threads/{id}/chat response (202 Accepted).
type ChatStartedResponse struct {
	CompletionID   string `json:"completionId,omitempty"`
	Status         string `json:"status"` // "started" or "queued"
	QueuedPromptID string `json:"queuedPromptId,omitempty"`
}

// ChatConflictResponse is the POST /threads/{id}/chat response (409 Conflict).
type ChatConflictResponse struct {
	Error        string `json:"error"` // "completion_in_progress"
	CompletionID string `json:"completionId"`
}

// ChatTurnStateConflictResponse is the POST /threads/{id}/chat response
// when the thread already has persisted turn state that must be resolved first.
type ChatTurnStateConflictResponse struct {
	Error        string `json:"error"`                  // "interrupted_turn_requires_resume" or "pending_question_requires_answer"
	Message      string `json:"message,omitempty"`      // user-facing summary
	QuestionID   string `json:"questionId,omitempty"`   // pending AskUserQuestion approval ID when applicable
	CompletionID string `json:"completionId,omitempty"` // recovery completion ID when resume was started automatically
}

// CancelCompletionResponse is the POST /threads/{id}/chat/cancel response.
type CancelCompletionResponse struct {
	Success      bool   `json:"success"`
	CompletionID string `json:"completionId"`
	Status       string `json:"status"` // "cancelled"
}

// NoActiveCompletionResponse is returned when no completion is active to cancel.
type NoActiveCompletionResponse struct {
	Error string `json:"error"` // "no_active_completion"
}

// ModelInfo represents a model from the AI provider's API.
type ModelInfo struct {
	ID               string   `json:"id"`
	DisplayName      string   `json:"display_name"`
	Provider         string   `json:"provider"`
	CreatedAt        string   `json:"created_at"`
	Type             string   `json:"type"`
	Reasoning        bool     `json:"reasoning"`
	ReasoningLevels  []string `json:"reasoningLevels,omitempty"`
	DefaultReasoning string   `json:"defaultReasoning,omitempty"`
}

// ModelsResponse is the GET /threads/{id}/models response.
type ModelsResponse struct {
	Models []ModelInfo `json:"models"`
}

// ============================================================================
// File System Types
// ============================================================================

// FileEntry represents a single file or directory entry.
type FileEntry struct {
	Name string `json:"name"`
	Type string `json:"type"` // "file" or "directory"
	Size int64  `json:"size,omitempty"`
}

// ListFilesResponse is the GET /files response.
type ListFilesResponse struct {
	Path    string      `json:"path"`
	Entries []FileEntry `json:"entries"`
}

// SearchResultEntry is a single result from a fuzzy file search.
type SearchResultEntry struct {
	Path  string  `json:"path"`
	Type  string  `json:"type"` // "file" or "directory"
	Score float64 `json:"score"`
}

// SearchFilesResponse is the GET /files/search response.
type SearchFilesResponse struct {
	Query   string              `json:"query"`
	Results []SearchResultEntry `json:"results"`
}

// ReadFileResponse is the GET /files/read response.
type ReadFileResponse struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Encoding string `json:"encoding"` // "utf8" or "base64"
	Size     int64  `json:"size"`
}

// WriteFileResponse is the POST /files/write response.
type WriteFileResponse struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// DeleteFileResponse is the POST /files/delete response.
type DeleteFileResponse struct {
	Path string `json:"path"`
	Type string `json:"type"` // "file" or "directory"
}

// RenameFileResponse is the POST /files/rename response.
type RenameFileResponse struct {
	OldPath string `json:"oldPath"`
	NewPath string `json:"newPath"`
}

// ============================================================================
// Diff Types
// ============================================================================

// FileDiffEntry represents a single changed file in the diff.
type FileDiffEntry struct {
	Path      string `json:"path"`
	Status    string `json:"status"` // "added", "modified", "deleted", "renamed"
	OldPath   string `json:"oldPath,omitempty"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Binary    bool   `json:"binary"`
	Patch     string `json:"patch,omitempty"`
}

// DiffStats contains summary statistics for a diff.
type DiffStats struct {
	FilesChanged int `json:"filesChanged"`
	Additions    int `json:"additions"`
	Deletions    int `json:"deletions"`
}

// DiffResponse is the GET /diff response (full diff with patches).
type DiffResponse struct {
	Files []FileDiffEntry `json:"files"`
	Stats DiffStats       `json:"stats"`
}

// DiffFileEntry represents a file entry with status for the files-only diff response.
type DiffFileEntry struct {
	Path    string `json:"path"`
	Status  string `json:"status"` // "added", "modified", "deleted", "renamed"
	OldPath string `json:"oldPath,omitempty"`
}

// DiffFilesResponse is the GET /diff?format=files response.
type DiffFilesResponse struct {
	Files []DiffFileEntry `json:"files"`
	Stats DiffStats       `json:"stats"`
}

// SingleFileDiffResponse is the GET /diff?path=... response.
type SingleFileDiffResponse struct {
	Path      string `json:"path"`
	Status    string `json:"status"` // "added", "modified", "deleted", "renamed", "unchanged"
	OldPath   string `json:"oldPath,omitempty"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Binary    bool   `json:"binary"`
	Patch     string `json:"patch"`
}

// ============================================================================
// Git Commits Types
// ============================================================================

// CommitsResponse is the GET /commits response (success case).
type CommitsResponse struct {
	ReplayBundle string `json:"replayBundle"` // Serialized commit replay bundle JSON
	CommitCount  int    `json:"commitCount"`  // Number of commits in the bundle
}

// CommitsErrorResponse is the GET /commits error response.
type CommitsErrorResponse struct {
	Error      string `json:"error"`                // "parent_mismatch", "no_commits", "invalid_parent", "not_git_repo"
	Message    string `json:"message"`
	IsClean    bool   `json:"isClean,omitempty"`    // Only set for "no_commits": true when working tree has no uncommitted changes
	HeadCommit string `json:"headCommit,omitempty"` // Only set for "no_commits": the current HEAD commit SHA
}

// ============================================================================
// AskUserQuestion Types
// ============================================================================

// AskUserQuestionOption represents a single choice for a clarifying question.
type AskUserQuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// AskUserQuestion represents a single clarifying question from the agent.
type AskUserQuestion struct {
	Question    string                  `json:"question"`
	Header      string                  `json:"header"`
	Options     []AskUserQuestionOption `json:"options"`
	MultiSelect bool                    `json:"multiSelect"`
	Notes       string                  `json:"notes,omitempty"` // optional context shown before options
}

// PendingQuestion is the pending AskUserQuestion payload.
type PendingQuestion struct {
	ToolUseID string            `json:"toolUseID"`
	Questions []AskUserQuestion `json:"questions"`
}

// PendingQuestionResponse is the GET /threads/{id}/chat/question/{questionId} response.
type PendingQuestionResponse struct {
	Status   string           `json:"status,omitempty"` // "pending" or "answered"
	Question *PendingQuestion `json:"question"`
}

// AnswerQuestionResponse is the POST /threads/{id}/chat/answer/{questionId} response.
type AnswerQuestionResponse struct {
	Success bool `json:"success"`
}

// ============================================================================
// Service Types
// ============================================================================

// Service represents a user-defined service in the sandbox.
type Service struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	HTTP        int    `json:"http,omitempty"`
	HTTPS       int    `json:"https,omitempty"`
	Path        string `json:"path"`
	URLPath     string `json:"urlPath,omitempty"`
	Status      string `json:"status"` // "running", "stopped", "starting", "stopping"
	Passive     bool   `json:"passive,omitempty"`
	PID         int    `json:"pid,omitempty"`
	StartedAt   string `json:"startedAt,omitempty"`
	ExitCode    *int   `json:"exitCode,omitempty"`
}

// ListServicesResponse is the GET /services response.
type ListServicesResponse struct {
	Services []Service `json:"services"`
}

// StartServiceResponse is the POST /services/:id/start response.
type StartServiceResponse struct {
	Status    string `json:"status"` // "starting"
	ServiceID string `json:"serviceId"`
}

// StopServiceResponse is the POST /services/:id/stop response.
type StopServiceResponse struct {
	Status    string `json:"status"` // "stopped"
	ServiceID string `json:"serviceId"`
}

// ServiceOutputEvent represents a single output event from a service.
type ServiceOutputEvent struct {
	Type      string `json:"type"` // "stdout", "stderr", "exit", "error"
	Data      string `json:"data,omitempty"`
	ExitCode  *int   `json:"exitCode,omitempty"`
	Error     string `json:"error,omitempty"`
	Timestamp string `json:"timestamp"`
}

// ServiceNotFoundResponse is returned when a service is not found.
type ServiceNotFoundResponse struct {
	Error     string `json:"error"` // "service_not_found"
	ServiceID string `json:"serviceId"`
}

// ServiceAlreadyRunningResponse is returned when a service is already running.
type ServiceAlreadyRunningResponse struct {
	Error     string `json:"error"` // "service_already_running"
	ServiceID string `json:"serviceId"`
	PID       int    `json:"pid"`
}

// ServiceNotRunningResponse is returned when a service is not running.
type ServiceNotRunningResponse struct {
	Error     string `json:"error"` // "service_not_running"
	ServiceID string `json:"serviceId"`
}

// ServiceNoPortResponse is returned when a service has no HTTP/HTTPS port.
type ServiceNoPortResponse struct {
	Error     string `json:"error"` // "service_no_port"
	ServiceID string `json:"serviceId"`
}

// ServiceIsPassiveResponse is returned when trying to control a passive service.
type ServiceIsPassiveResponse struct {
	Error     string `json:"error"` // "service_is_passive"
	ServiceID string `json:"serviceId"`
	Message   string `json:"message"`
}

// ============================================================================
// Hook Types
// ============================================================================

// HookRunStatus is the status of a single hook's runs.
type HookRunStatus struct {
	HookID              string `json:"hookId"`
	HookName            string `json:"hookName"`
	Type                string `json:"type"`
	LastRunAt           string `json:"lastRunAt"`
	LastResult          string `json:"lastResult"` // "success", "failure", "running", or "pending"
	LastExitCode        int    `json:"lastExitCode"`
	OutputPath          string `json:"outputPath"`
	RunCount            int    `json:"runCount"`
	FailCount           int    `json:"failCount"`
	ConsecutiveFailures int    `json:"consecutiveFailures"`
}

// HooksStatusResponse is the GET /hooks/status response.
type HooksStatusResponse struct {
	Hooks           map[string]HookRunStatus `json:"hooks"`
	PendingHooks    []string                 `json:"pendingHooks"`
	LastEvaluatedAt string                   `json:"lastEvaluatedAt"`
}

// HookOutputResponse is the GET /hooks/:hookId/output response.
type HookOutputResponse struct {
	Output string `json:"output"`
}

// HookRerunResponse is the POST /hooks/:hookId/rerun response.
type HookRerunResponse struct {
	Success  bool `json:"success"`
	ExitCode int  `json:"exitCode"`
}
