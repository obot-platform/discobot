package agent

import (
	"context"
	"encoding/json"
	"errors"
	"iter"

	"github.com/obot-platform/discobot/agent-go/internal/api"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
)

var (
	ErrInterruptedTurnRequiresResume = errors.New("interrupted turn requires resume")
	ErrPendingQuestionRequiresAnswer = errors.New("pending question requires answer")
)

// CommandKind indicates the origin of a Command.
type CommandKind string

const (
	// CommandKindSkill is a user-defined skill from a skills/ directory,
	// invoked by the LLM via the Skill tool.
	CommandKindSkill CommandKind = "skill"

	// CommandKindCommand is a legacy user-defined command from a commands/
	// directory, expanded programmatically when the user types /name.
	CommandKindCommand CommandKind = "command"

	// CommandKindScript is an executable user-defined slash command from a
	// scripts/ directory.
	CommandKindScript CommandKind = "script"

	// CommandKindBuiltin is a command handled natively by the agent (e.g. /clear).
	CommandKindBuiltin CommandKind = "built-in"
)

// Command represents a slash command available to the user.
type Command struct {
	// Name is the command's slash-command name without the leading slash (e.g., "commit").
	Name string

	// Description is a short human-readable description of what the command does.
	Description string

	// Kind indicates the origin of the command.
	Kind CommandKind

	// Discobot carries optional Discobot-specific command metadata.
	Discobot DiscobotCommandMetadata
}

type DiscobotCommandMetadata struct {
	UI                bool
	Label             string
	ActiveLabel       string
	Icon              string
	Group             string
	Order             int
	CredentialRequest []DiscobotCredentialRequest
}

type DiscobotCredentialRequest struct {
	EnvVar        string
	Name          string
	Justification string
	ApprovedUses  []DiscobotCredentialApprovedUse
}

type DiscobotCredentialApprovedUse struct {
	Description string
}

// PendingQuestion represents an outstanding approval request that needs user input.
type PendingQuestion struct {
	ApprovalID  string
	Questions   []api.AskUserQuestion
	Credentials []api.RequestedCredential
	Metadata    json.RawMessage
	Context     string
}

// Agent abstracts the underlying agent implementation.
// It mirrors the TypeScript Agent interface from agent-api,
// adapted to Go patterns using iter.Seq2 for streaming.
//
// All methods are thread-safe. The threadID parameter maps to
// the TS sessionId concept (a conversation thread).
type Agent interface {
	// Prompt sends a user message and streams the response as an iterator.
	// The iterator yields MessageChunks until the turn is complete.
	//
	// If the thread has an interrupted turn from a previous crash,
	// Prompt returns ErrInterruptedTurnRequiresResume.
	//
	// If the thread is paused waiting for an AskUserQuestion answer,
	// Prompt returns ErrPendingQuestionRequiresAnswer.
	//
	// Only one Prompt may be active per threadID. Calling Prompt while
	// another is active for the same thread returns an error.
	Prompt(ctx context.Context, threadID string, req PromptRequest) iter.Seq2[message.MessageChunk, error]

	// Compact forces conversation compaction without running a normal prompt.
	Compact(ctx context.Context, threadID string, req PromptRequest) iter.Seq2[message.MessageChunk, error]

	// Reset clears the active leaf for a thread so the next prompt starts with
	// no active conversation history.
	Reset(ctx context.Context, threadID string) (ThreadInfo, error)

	// Resume continues or finalizes an interrupted turn from persisted disk state.
	// Request-scoped overrides such as model, reasoning, and mode may be applied
	// before the turn resumes.
	//
	// When req.UserParts is non-empty, Resume abandons the interrupted turn and
	// starts a fresh completion for the new prompt instead. ReplayLeafID reports
	// the highest persisted message that should be replayed before the returned
	// live stream starts emitting chunks.
	Resume(ctx context.Context, threadID string, req PromptRequest) (ResumeResult, error)

	// Cancel cancels the active prompt for a thread.
	// Implementations may also cancel a turn paused waiting for AskUserQuestion
	// answers so the thread is no longer blocked.
	// Returns true if a prompt/paused turn was cancelled, false otherwise.
	Cancel(threadID string) bool

	// Messages returns the conversation history for a thread as
	// UI-projected messages.
	Messages(threadID, leafID string) ([]message.UIMessage, error)

	// ListThreads returns all thread IDs.
	ListThreads() ([]string, error)

	// ListThreadInfos returns metadata for all threads.
	ListThreadInfos() ([]ThreadInfo, error)

	// GetThreadInfo returns metadata for a single thread.
	GetThreadInfo(threadID string) (ThreadInfo, error)

	// GetThreadTokenUsageDetails returns per-turn and per-step token usage for
	// a single thread.
	GetThreadTokenUsageDetails(threadID string) (ThreadTokenUsageDetails, error)

	// CreateThread creates a thread and returns its metadata.
	CreateThread(ctx context.Context, req CreateThreadRequest) (ThreadInfo, error)

	// UpdateThread updates thread metadata and returns the updated metadata.
	UpdateThread(ctx context.Context, threadID string, req UpdateThreadRequest) (ThreadInfo, error)

	// DeleteThread deletes a thread.
	DeleteThread(ctx context.Context, threadID string) error

	// HasInterruptedTurn reports whether threadID has an unfinished turn from a
	// previous crash. Threads paused for AskUserQuestion return false.
	HasInterruptedTurn(threadID string) (bool, error)

	// PendingQuestion returns the pending AskUserQuestion for a thread,
	// or nil if no question is pending. Used by GET /chat/question.
	PendingQuestion(threadID string) (*PendingQuestion, error)

	// SubmitAnswer persists the user's response for a pending approval.
	// The turn is resumed by calling Resume.
	SubmitAnswer(threadID, approvalID string, req api.AnswerQuestionRequest) error

	// FinalResponse returns the last assistant text from a completed thread turn.
	// Returns empty string (no error) if the thread has no content yet or if a
	// turn is currently in progress.
	FinalResponse(threadID string) (string, error)

	// ListCommands returns all slash commands available to the user, including
	// user-defined skills, legacy commands, and built-in commands.
	ListCommands() ([]Command, error)
}

type ThreadInfo struct {
	ID              string
	Name            string
	CWD             string
	LastMessage     string
	ErrorMessage    string
	Model           string
	Reasoning       string
	ServiceTier     string
	State           ThreadState
	TokenUsage      TokenUsageInfo
	PendingQuestion bool
	ActiveCommand   string
	Metadata        json.RawMessage
}

type TokenUsageInfo struct {
	Total           message.Usage
	LastStep        message.Usage
	LastTurn        message.Usage
	ModelMaxTokens  int
	MaxOutputTokens int
	Prices          message.TokenPrices
}

type ThreadTokenUsageDetails struct {
	ThreadID string
	Summary  TokenUsageInfo
	Turns    []TokenUsageTurn
}

type TokenUsageTurn struct {
	ID              string
	Model           string
	Reasoning       string
	ServiceTier     string
	ModelMaxTokens  int
	MaxOutputTokens int
	Prices          message.TokenPrices
	Usage           message.Usage
	StartedAt       string
	FinishedAt      string
	Steps           []TokenUsageStep
}

type TokenUsageStep struct {
	Index              int
	AssistantMessageID string
	ToolCalls          []TokenUsageToolCall
	Usage              message.Usage
}

type TokenUsageToolCall struct {
	ID   string
	Name string
}

// ThreadState is the user-visible terminal state shown for a thread.
type ThreadState string

const (
	// ThreadStateNone means the thread has no special terminal state.
	ThreadStateNone ThreadState = ""
	// ThreadStateInterrupted means a previous turn stopped before completion and can be resumed.
	ThreadStateInterrupted ThreadState = "interrupted"
	// ThreadStateCancelled means the last turn was cancelled.
	ThreadStateCancelled ThreadState = "cancelled"
)

type CreateThreadRequest struct {
	ID          string
	Name        string
	CWD         string
	LastMessage string
	Metadata    json.RawMessage
}

type UpdateThreadRequest struct {
	Name              *string
	CWD               *string
	LastMessage       *string
	ErrorMessage      *string
	ClearErrorMessage bool
	Metadata          json.RawMessage
}

// ResumeResult describes a prepared resumed completion.
type ResumeResult struct {
	// ReplayLeafID is the highest persisted message that should be replayed as
	// history before Stream is consumed live.
	ReplayLeafID string

	// Stream yields the resumed completion chunks.
	Stream iter.Seq2[message.MessageChunk, error]
}

// PromptRequest holds the parameters for a Prompt call.
type PromptRequest struct {
	// UserParts is the user message content in UI message format.
	UserParts []message.UIPart

	// Metadata is attached to the persisted user message for UI rendering.
	Metadata json.RawMessage

	// Model overrides the default model for this request.
	Model string

	// SupportingModels overrides task-specific auxiliary models used internally
	// by agent-go (for example, thread summarization). Keys are known
	// SupportingModelType values and values are model refs understood by
	// ProviderRegistry.ResolveModel.
	SupportingModels providers.SupportingModels

	// Reasoning controls extended thinking ("enabled" or "").
	Reasoning string

	// ServiceTier optionally selects a provider latency tier, such as "fast".
	ServiceTier string

	// FreshContext forces the next prompt to ignore the current leaf and start a
	// fresh branch within the thread.
	FreshContext bool

	// Tools overrides the default tool set. Nil means use agent defaults.
	Tools []providers.ToolDefinition

	// SubagentType names a SubAgentConfig defined in .claude/agents/*.md.
	// When set, the agent applies that config's tool restrictions, model
	// override, and system prompt instead of the session defaults.
	SubagentType string

	// ParentTaskID identifies the Task/Agent tool call that created this thread.
	// Empty for top-level threads.
	ParentTaskID string

	// SubagentDepth is the current nesting depth relative to the top-level
	// thread. Top-level threads are depth 0, their direct children are depth 1,
	// and so on.
	SubagentDepth int

	// MaxTurns caps the number of LLM calls in this turn; 0 means unlimited.
	// The sub-agent config's MaxTurns is also applied; the stricter of the two wins.
	MaxTurns int
}
