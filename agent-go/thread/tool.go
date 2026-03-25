package thread

import (
	"context"
	"encoding/json"

	"github.com/obot-platform/discobot/agent-go/agent"
	"github.com/obot-platform/discobot/agent-go/internal/api"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
)

// ToolExecuteResult is returned by ToolExecutor.Execute.
// Exactly one of Result, Approval, or Async should be set.
type ToolExecuteResult struct {
	// Result holds the completed tool result. Zero value if Approval or Async is set.
	Result message.ToolResultPart

	// Approval is non-nil when the tool requires user approval before proceeding.
	// The step loop will pause the turn, persist the approval request, and resume
	// when the user provides answers via ResolveApproval.
	Approval *ApprovalRequest

	// Async is non-nil when the tool launched an asynchronous background task.
	// The step loop will continue processing other tools and then wait for all
	// async tasks to complete before advancing to the next step.
	Async *AsyncTaskHandle
}

// AsyncWaitResult is returned when waiting on an in-flight async task.
// Exactly one of Result or Approval should be set.
type AsyncWaitResult struct {
	// Result holds the completed tool result, including denied execution results.
	Result message.ToolResultPart

	// Approval is non-nil when the async task reached a point where it requires
	// user approval before it can continue.
	Approval *ApprovalRequest
}

// ToolContext carries per-turn state required by tool execution.
//
// It is passed explicitly to every tool call so executor implementations stay
// stateless across threads and concurrent turns.
type ToolContext struct {
	ThreadID              string
	PlanMode              bool
	PlanFilePath          string
	PromptRequestPlanMode bool
	ProviderID            string
	ModelID               string
	SubagentDepth         int
	MaxSubagentDepth      int
	CurrentTaskID         string
	ProviderResolver      providers.ProviderResolver
	Agent                 agent.Agent
	ModeChange            *string // set by a tool that changes the mode; consumed by the turn loop
}

// AsyncTaskHandle represents an in-flight asynchronous tool execution.
// The TaskID is persisted for crash recovery; the Wait function blocks
// until the task completes.
type AsyncTaskHandle struct {
	// TaskID is a stable identifier for the async task, chosen by the executor.
	// It must be unique within a step and is persisted to disk for crash recovery.
	TaskID string

	// Wait blocks until the async task completes or reaches an approval gate.
	// The context is the step loop's context; cancellation propagates.
	Wait func(ctx context.Context) (AsyncWaitResult, error)
}

// ApprovalRequest signals that tool execution requires user input.
type ApprovalRequest struct {
	// ApprovalID is the external ID used to query and answer this approval.
	// When empty, the turn loop generates a fresh ID for this specific approval
	// prompt. One tool call may therefore produce multiple approval IDs over
	// time; consumers should treat this as an opaque request identifier.
	ApprovalID string
	// Questions is the raw JSON data presented to the user.
	Questions json.RawMessage
}

// ToolExecutor executes a tool call and returns the result.
// Implementations handle the actual tool logic (bash, file read, etc.).
type ToolExecutor interface {
	// Execute runs a tool call. Returns a completed result, an approval request,
	// or an async task handle.
	Execute(ctx context.Context, toolCtx *ToolContext, call message.ToolCallPart) (ToolExecuteResult, error)

	// ResolveAnswer converts the user's response into the next tool execution
	// result. It may return a final result, another approval request, or a new
	// async task handle.
	ResolveAnswer(toolCtx *ToolContext, call message.ToolCallPart, req api.AnswerQuestionRequest) (ToolExecuteResult, error)

	// ResumeAsync re-attaches to a previously launched async task after a crash.
	// Returns either a completed Result (if the task finished while down) or
	// a new Async handle with a Wait function (if the task is still running).
	ResumeAsync(ctx context.Context, toolCtx *ToolContext, call message.ToolCallPart, taskID string, req *api.AnswerQuestionRequest) (ToolExecuteResult, error)
}
