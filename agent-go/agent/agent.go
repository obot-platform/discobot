package agent

import (
	"context"
	"iter"

	"github.com/obot-platform/discobot/agent-go/internal/api"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
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
	// the interrupted turn is resumed instead of starting a new one.
	//
	// Only one Prompt may be active per threadID. Calling Prompt while
	// another is active for the same thread returns an error.
	Prompt(ctx context.Context, threadID string, req PromptRequest) iter.Seq2[message.MessageChunk, error]

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
	// The turn is resumed by calling Prompt again (which detects the
	// waiting_for_answer state and resumes with the answer).
	SubmitAnswer(threadID, approvalID string, req api.AnswerQuestionRequest) error

	// FinalResponse returns the last assistant text from a completed thread turn.
	// Returns empty string (no error) if the thread has no content yet or if a
	// turn is currently in progress.
	FinalResponse(threadID string) (string, error)

	// ListCommands returns all slash commands available to the user, including
	// user-defined skills, legacy commands, and built-in commands.
	ListCommands() ([]Command, error)

	// IsLeaf reports whether msgID is a valid leaf in the thread's message tree.
	// Implementations may use cached state (e.g. ActiveLeafID from thread config)
	// before falling back to a full store scan.
	// Returns false (no error) when msgID does not exist.
	IsLeaf(threadID, msgID string) (bool, error)
}

// PromptRequest holds the parameters for a Prompt call.
type PromptRequest struct {
	// LeafID is the parent message to branch from. Empty for new conversations.
	LeafID string

	// UserParts is the user message content in UI message format.
	UserParts []message.UIPart

	// Model overrides the default model for this request.
	Model string

	// Reasoning controls extended thinking ("enabled" or "").
	Reasoning string

	// Mode is the permission mode ("plan" or "").
	Mode string

	// ReplayTurn forces a resumed turn to replay its cached prefix even when the
	// persisted phase would normally continue live without re-emitting earlier
	// chunks. Used when an answered waiting_for_answer turn is resumed after the
	// in-memory completion cache has been lost.
	ReplayTurn bool

	// Tools overrides the default tool set. Nil means use agent defaults.
	Tools []providers.ToolDefinition

	// SubagentType names a SubAgentConfig defined in .claude/agents/*.md.
	// When set, the agent applies that config's tool restrictions, model
	// override, and system prompt instead of the session defaults.
	SubagentType string

	// MaxTurns caps the number of LLM calls in this turn; 0 means unlimited.
	// The sub-agent config's MaxTurns is also applied; the stricter of the two wins.
	MaxTurns int
}
