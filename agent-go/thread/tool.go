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
	// when the user provides answers via Continue.
	Approval *ApprovalRequest

	// Async is non-nil when the tool launched asynchronous work that will be
	// resumed later through an async continuation handle.
	// The step loop will continue processing other tools and then wait for all
	// async continuations to complete before advancing to the next step.
	Async *AsyncContinuationHandle
}

// AsyncWaitResult is returned when waiting on an in-flight async continuation.
// Exactly one of Result or Approval should be set.
type AsyncWaitResult struct {
	// Result holds the completed tool result, including denied execution results.
	Result message.ToolResultPart

	// Approval is non-nil when the async continuation reached a point where it
	// requires
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

// AsyncContinuationHandle represents in-flight asynchronous tool work.
// The Continuation payload is what TURN persists for crash recovery, and Wait
// blocks until the continuation produces either a final result or another
// approval boundary.
type AsyncContinuationHandle struct {
	// Continuation is an executor-owned opaque payload that TURN persists and
	// passes back into Continue when this async work must be resumed later.
	Continuation json.RawMessage

	// Wait blocks until the async continuation completes or reaches an approval
	// gate.
	// The context is the step loop's context; cancellation propagates.
	Wait func(ctx context.Context) (AsyncWaitResult, error)
}

// ApprovalRequest signals that tool execution requires user input.
type ApprovalRequest struct {
	// Questions is the raw JSON data presented to the user.
	Questions json.RawMessage
	// Continuation is an executor-owned opaque payload that TURN persists and
	// passes back into Continue when this approval is answered later.
	Continuation json.RawMessage
}

// ToolExecutor executes a tool call and returns the result.
// Implementations handle the actual tool logic (bash, file read, etc.).
type ToolExecutor interface {
	// Execute runs a tool call. Returns a completed result, an approval request,
	// or an async continuation handle.
	Execute(ctx context.Context, toolCtx *ToolContext, call message.ToolCallPart) (ToolExecuteResult, error)

	// Continue resumes a previously paused or asynchronous tool execution using
	// executor-owned continuation state. req is nil for async recovery without a
	// newly submitted answer, and non-nil when continuing after user input.
	Continue(ctx context.Context, toolCtx *ToolContext, call message.ToolCallPart, continuation json.RawMessage, req *api.AnswerQuestionRequest) (ToolExecuteResult, error)
}
