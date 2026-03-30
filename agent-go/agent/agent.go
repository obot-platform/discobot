package agent

import (
	"context"
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
}

// PendingQuestion represents an outstanding approval request that needs user input.
type PendingQuestion struct {
	ApprovalID string
	Questions  []api.AskUserQuestion
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

	// Resume continues or finalizes an interrupted turn from persisted disk state.
	Resume(ctx context.Context, threadID string) iter.Seq2[message.MessageChunk, error]

	// Cancel cancels the active prompt for a thread.
	// Returns true if a prompt was active and cancelled, false otherwise.
	Cancel(threadID string) bool

	// Messages returns the conversation history for a thread as
	// UI-projected messages.
	Messages(threadID, leafID string) ([]message.UIMessage, error)

	// ListModels returns available models from the underlying provider.
	ListModels(ctx context.Context) ([]providers.ModelInfo, error)

	// ListThreads returns all thread IDs.
	ListThreads() ([]string, error)

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

// PromptRequest holds the parameters for a Prompt call.
type PromptRequest struct {
	// LeafID is the parent message to branch from. Empty for new conversations.
	LeafID string

	// UserParts is the user message content in UI message format.
	UserParts []message.UIPart

	// Model overrides the default model for this request.
	Model string

	// SupportingModels overrides task-specific auxiliary models used internally
	// by agent-go (for example, thread summarization). Keys are known
	// SupportingModelType values and values are model refs understood by
	// ProviderRegistry.ResolveModel.
	SupportingModels providers.SupportingModels

	// Reasoning controls extended thinking ("enabled" or "").
	Reasoning string

	// Mode controls the requested permission mode.
	// "" keeps the current thread mode when one exists, otherwise defaults to build.
	// "plan" switches to plan mode.
	// "build" switches to build mode.
	Mode string

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
