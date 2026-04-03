// Package sandboxapi defines the request/response types for the sandbox HTTP API.
//
// These types must be kept in sync with the TypeScript agent's API types
// located at: agent/src/api/types.ts
//
// API Endpoints:
//
//	GET  /        - Health check
//	GET  /health  - Detailed health status
//	GET  /chat    - Get all messages
//	POST /chat    - Send messages and stream response (SSE)
package sandboxapi

import (
	"encoding/json"
	"time"
)

// ============================================================================
// Request Types
// ============================================================================

// ChatRequest is the POST /chat request body.
type ChatRequest struct {
	// Messages is the array of UIMessages to send.
	// Kept as raw JSON to pass through without requiring Go to understand
	// the full UIMessage structure from the AI SDK.
	Messages json.RawMessage `json:"messages"`
	// Model is the optional model to use for this chat request.
	Model string `json:"model,omitempty"`
	// Reasoning controls extended thinking. This is passed through as a string
	// reasoning level such as "auto", "low", "medium", "high", "xhigh",
	// "none", "default", or "" for model/provider default behavior.
	Reasoning string `json:"reasoning,omitempty"`
	// Mode is the permission mode: "plan" for planning mode, "" for default (build mode).
	Mode string `json:"mode,omitempty"`
}

// ============================================================================
// Response Types
// ============================================================================

// RootResponse is the GET / response.
type RootResponse struct {
	Status  string `json:"status"`  // Always "ok"
	Service string `json:"service"` // Always "agent"
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

// ModelInfo represents a model from the Claude API.
type ModelInfo struct {
	ID               string   `json:"id"`
	DisplayName      string   `json:"display_name"`
	Provider         string   `json:"provider"`
	CreatedAt        string   `json:"created_at"`
	Type             string   `json:"type"`
	Reasoning        bool     `json:"reasoning"` // Whether this model supports extended thinking
	ReasoningLevels  []string `json:"reasoningLevels,omitempty"`
	DefaultReasoning string   `json:"defaultReasoning,omitempty"`
}

// ModelsResponse is the GET /models response.
type ModelsResponse struct {
	Models []ModelInfo `json:"models"`
}

// ChatStatusResponse is the GET /chat/status response.
type ChatStatusResponse struct {
	IsRunning    bool    `json:"isRunning"`
	CompletionID *string `json:"completionId"`
	StartedAt    *string `json:"startedAt"`
	Error        *string `json:"error"`
}

// ChatStartedResponse is the POST /chat response.
type ChatStartedResponse struct {
	CompletionID   string `json:"completionId,omitempty"`
	Status         string `json:"status,omitempty"`
	QueuedPromptID string `json:"queuedPromptId,omitempty"`
}

// ChatConflictResponse is the POST /chat response when another completion is already active.
type ChatConflictResponse struct {
	Error        string `json:"error"`
	CompletionID string `json:"completionId"`
}

// ChatTurnStateConflictResponse is the POST /chat response when the thread must
// resolve existing persisted turn state before starting a new prompt.
type ChatTurnStateConflictResponse struct {
	Error        string `json:"error"`
	Message      string `json:"message,omitempty"`
	QuestionID   string `json:"questionId,omitempty"`
	CompletionID string `json:"completionId,omitempty"`
}

// ErrorResponse is returned for 4xx/5xx errors.
type ErrorResponse struct {
	Error string `json:"error"`
}

// ============================================================================
// Shared Types
// ============================================================================

// Thread represents a conversation thread persisted by the agent.
type Thread struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	LastMessage  string         `json:"lastMessage,omitempty"`
	ErrorMessage string         `json:"errorMessage,omitempty"`
	Model        string         `json:"model,omitempty"`     // full "providerId/modelId" ref
	Reasoning    string         `json:"reasoning,omitempty"` // "", "auto", "low", "medium", "high", "xhigh", "none", or "default"
	Mode         string         `json:"mode"`                // "build" or "plan"
	State        string         `json:"state,omitempty"`     // "interrupted" or "cancelled"
	Pending      bool           `json:"pending,omitempty"`   // true when the thread exists in concept but the sandbox hasn't created it yet
	PromptQueue  []QueuedPrompt `json:"promptQueue,omitempty"`
}

type QueuedPrompt struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt,omitempty"`
	Message   UIMessage `json:"message"`
	Model     string    `json:"model,omitempty"`
	Reasoning string    `json:"reasoning,omitempty"`
	Mode      string    `json:"mode,omitempty"`
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
	Name string `json:"name,omitempty"`
}

// DeleteThreadResponse is the DELETE /threads/{id} response body.
type DeleteThreadResponse struct {
	Success bool `json:"success"`
}

type DeleteQueuedPromptResponse struct {
	Success bool `json:"success"`
}

// UIMessage represents a message in AI SDK UIMessage format.
// This is a minimal representation - the full structure is passed through
// as raw JSON where possible to avoid tight coupling with AI SDK internals.
type UIMessage struct {
	ID        string          `json:"id"`
	Role      string          `json:"role"` // "user", "assistant", "system"
	Parts     json.RawMessage `json:"parts"`
	CreatedAt string          `json:"createdAt,omitempty"`
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

// WriteFileRequest is the POST /files/write request body.
type WriteFileRequest struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Encoding string `json:"encoding,omitempty"` // defaults to "utf8"
}

// WriteFileResponse is the POST /files/write response.
type WriteFileResponse struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// DeleteFileRequest is the POST /files/delete request body.
type DeleteFileRequest struct {
	Path string `json:"path"`
}

// DeleteFileResponse is the POST /files/delete response.
type DeleteFileResponse struct {
	Path string `json:"path"`
	Type string `json:"type"` // "file" or "directory"
}

// RenameFileRequest is the POST /files/rename request body.
type RenameFileRequest struct {
	OldPath string `json:"oldPath"`
	NewPath string `json:"newPath"`
}

// RenameFileResponse is the POST /files/rename response.
type RenameFileResponse struct {
	OldPath string `json:"oldPath"`
	NewPath string `json:"newPath"`
}

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

// DiffFilesResponse is the GET /diff?format=files response (file paths with status).
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
// Git Commits Types (for commit workflow)
// ============================================================================

// CommitsResponse is the GET /commits response (success case).
// Returns a serialized commit replay bundle for commits since a parent.
type CommitsResponse struct {
	ReplayBundle string `json:"replayBundle"` // Serialized commit replay bundle JSON
	CommitCount  int    `json:"commitCount"`  // Number of commits in the bundle
}

// CommitsErrorResponse is the GET /commits error response.
type CommitsErrorResponse struct {
	Error      string `json:"error"`                // "parent_mismatch", "no_commits", "invalid_parent", "not_git_repo"
	Message    string `json:"message"`              // Human-readable error message
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

// AskUserQuestion represents a single clarifying question from Claude.
type AskUserQuestion struct {
	Question    string                  `json:"question"`
	Header      string                  `json:"header"`
	Options     []AskUserQuestionOption `json:"options"`
	MultiSelect bool                    `json:"multiSelect"`
	Notes       string                  `json:"notes,omitempty"` // optional context shown before options
}

// PendingQuestion is the pending AskUserQuestion payload returned by GET /chat/question.
// Question is nil when no question is pending.
type PendingQuestion struct {
	ToolUseID string            `json:"toolUseID"`
	Questions []AskUserQuestion `json:"questions"`
	Context   string            `json:"context,omitempty"`
}

// PendingQuestionResponse is the GET /chat/question response body.
// When queried with toolUseID, includes a Status field ("pending" or "answered").
type PendingQuestionResponse struct {
	Status   string           `json:"status,omitempty"` // "pending" or "answered" (when toolUseID query param is used)
	Question *PendingQuestion `json:"question"`         // nil if no question is pending
}

// AnswerQuestionRequest is the POST /chat/answer request body.
type AnswerQuestionRequest struct {
	ToolUseID string            `json:"toolUseID"`
	Answers   map[string]string `json:"answers"`
}

// AnswerQuestionResponse is the POST /chat/answer response body.
type AnswerQuestionResponse struct {
	Success bool `json:"success"`
}

// ============================================================================
// Service Types
// ============================================================================

// Service represents a user-defined service in the sandbox.
type Service struct {
	ID          string `json:"id"`                    // Filename in .discobot/services/
	Name        string `json:"name"`                  // Display name (from config or id)
	Description string `json:"description,omitempty"` // Description from config
	Order       *int   `json:"order,omitempty"`       // Optional UI ordering hint (lower first)
	HTTP        int    `json:"http,omitempty"`        // HTTP port if http service
	HTTPS       int    `json:"https,omitempty"`       // HTTPS port if https service
	Path        string `json:"path"`                  // Absolute path to service file
	URLPath     string `json:"urlPath,omitempty"`     // Default URL path for web preview (e.g., "/app")
	Status      string `json:"status"`                // "running", "stopped", "starting", "stopping"
	Passive     bool   `json:"passive,omitempty"`     // True if passive service (external HTTP endpoint, not started/stopped)
	PID         int    `json:"pid,omitempty"`         // Process ID if running
	StartedAt   string `json:"startedAt,omitempty"`   // ISO timestamp when started
	ExitCode    *int   `json:"exitCode,omitempty"`    // Exit code if stopped after running
}

// ListServicesResponse is the GET /services response.
type ListServicesResponse struct {
	Services []Service `json:"services"`
}

// StartServiceResponse is the POST /services/:id/start response.
type StartServiceResponse struct {
	Status    string `json:"status"`    // "starting"
	ServiceID string `json:"serviceId"` // The service ID
}

// StopServiceResponse is the POST /services/:id/stop response.
type StopServiceResponse struct {
	Status    string `json:"status"`    // "stopped"
	ServiceID string `json:"serviceId"` // The service ID
}

// ServiceOutputEvent represents a single output event from a service.
type ServiceOutputEvent struct {
	Type      string `json:"type"`               // "stdout", "stderr", "exit", "error"
	Data      string `json:"data,omitempty"`     // Output data for stdout/stderr
	ExitCode  *int   `json:"exitCode,omitempty"` // Exit code for exit event
	Error     string `json:"error,omitempty"`    // Error message for error event
	Timestamp string `json:"timestamp"`          // ISO timestamp
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
