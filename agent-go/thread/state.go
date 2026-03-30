package thread

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/obot-platform/discobot/agent-go/message"
)

// TurnPhase represents the phase of an active turn's lifecycle.
type TurnPhase string

const (
	PhaseStreaming        TurnPhase = "streaming"
	PhaseTools            TurnPhase = "tools"
	PhaseWaitingForAsync  TurnPhase = "waiting_for_async"
	PhaseWaitingForAnswer TurnPhase = "waiting_for_answer"
)

// TurnState is the on-disk record of an active turn.
// It exists in {threadID}/turn.json only while a turn is running.
// On clean completion, it is deleted.
type TurnState struct {
	ID                string     `json:"id"`
	ThreadID          string     `json:"threadId"`
	LeafID            string     `json:"leafId"`
	Config            TurnConfig `json:"config"`
	CurrentStep       int        `json:"currentStep"`
	Phase             TurnPhase  `json:"phase"`
	LeafMsgID         string     `json:"leafMsgId"`                   // updated as messages are saved
	AssistantMsgID    string     `json:"assistantMsgId"`              // pre-generated ID for the first assistant message
	PendingApprovalID string     `json:"pendingApprovalId,omitempty"` // tool call ID of the pending approval request
	StartedAt         *time.Time `json:"startedAt,omitempty"`
	UpdatedAt         *time.Time `json:"updatedAt,omitempty"`
	FinishedAt        *time.Time `json:"finishedAt,omitempty"`
}

// PendingQuestionState persists a pending approval to disk.
// Stored at {turnDir}/approve-{approvalId}.json while the turn is paused waiting for user input.
// ApprovalID identifies one specific approval prompt; ToolCallID identifies the
// underlying paused tool invocation. A single tool call may emit multiple
// approval prompts over time, each with its own ApprovalID.
type PendingQuestionState struct {
	ApprovalID   string          `json:"approvalId"`
	ToolCallID   string          `json:"toolCallId"`
	StepIndex    int             `json:"stepIndex"`
	ResumePhase  TurnPhase       `json:"resumePhase,omitempty"`  // phase to resume after this approval is answered
	Continuation json.RawMessage `json:"continuation,omitempty"` // executor-owned continuation payload for resuming this approval
	Questions    json.RawMessage `json:"questions,omitempty"`    // raw JSON array from tool input
}

// QuestionAnswer persists the user's response to a pending approval.
type QuestionAnswer struct {
	ApprovalID string            `json:"approvalId"`
	Answers    map[string]string `json:"answers"`
}

// StepAsyncContinuations holds executor-owned continuation metadata for async
// tool work that is still in progress for a step.
// Persisted to step-NNN-async.json so those continuations can be resumed after
// a crash.
type StepAsyncContinuations struct {
	Continuations []AsyncContinuationInfo `json:"continuations"`
}

// StepEventMessages tracks immutable message IDs written for a step after the
// assistant message is persisted. These are typically approval request,
// approval response, and tool result messages in execution order.
type StepEventMessages struct {
	MessageIDs []string `json:"messageIds"`
}

// AsyncContinuationInfo identifies one persisted async continuation owned by a
// tool executor.
type AsyncContinuationInfo struct {
	ToolCallID   string          `json:"toolCallId"`
	ToolName     string          `json:"toolName"`
	Continuation json.RawMessage `json:"continuation,omitempty"` // executor-owned continuation payload for resuming async work
	Input        string          `json:"input"`                  // original tool input JSON, passed back to Continue
}

// StepResult is written after a step's streaming completes.
// It contains the accumulated assistant Message and the
// list of tool calls that need execution.
type StepResult struct {
	AssistantMessageID string          `json:"assistantMessageId,omitempty"`
	AssistantMessage   message.Message `json:"assistantMessage"`
	ToolCalls          []ToolCallInfo  `json:"toolCalls,omitempty"`
}

// ToolCallInfo identifies a tool call extracted from an assistant message.
type ToolCallInfo struct {
	ToolCallID string `json:"toolCallId"`
	ToolName   string `json:"toolName"`
	Input      string `json:"input"`
}

// StepToolResults holds the tool results accumulated so far for a step.
// It is overwritten after each tool completes, growing the Results slice.
// Custom JSON handling because ToolResultPart.Output uses json:"-".
type StepToolResults struct {
	Results []message.ToolResultPart
}

func (s StepToolResults) MarshalJSON() ([]byte, error) {
	items := make([]json.RawMessage, len(s.Results))
	for i, r := range s.Results {
		var outputData json.RawMessage
		if r.Output != nil {
			var err error
			outputData, err = message.MarshalToolResultOutput(r.Output)
			if err != nil {
				return nil, fmt.Errorf("marshal tool result %d output: %w", i, err)
			}
		}
		data, err := json.Marshal(struct {
			ToolCallID string          `json:"toolCallId"`
			ToolName   string          `json:"toolName"`
			Output     json.RawMessage `json:"output,omitempty"`
		}{r.ToolCallID, r.ToolName, outputData})
		if err != nil {
			return nil, err
		}
		items[i] = data
	}
	return json.Marshal(struct {
		Results []json.RawMessage `json:"results"`
	}{items})
}

func (s *StepToolResults) UnmarshalJSON(data []byte) error {
	var raw struct {
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.Results = make([]message.ToolResultPart, len(raw.Results))
	for i, itemData := range raw.Results {
		var item struct {
			ToolCallID string          `json:"toolCallId"`
			ToolName   string          `json:"toolName"`
			Output     json.RawMessage `json:"output"`
		}
		if err := json.Unmarshal(itemData, &item); err != nil {
			return fmt.Errorf("unmarshal tool result %d: %w", i, err)
		}
		var output message.ToolResultOutput
		if len(item.Output) > 0 {
			var err error
			output, err = message.UnmarshalToolResultOutput(item.Output)
			if err != nil {
				return fmt.Errorf("unmarshal tool result %d output: %w", i, err)
			}
		}
		s.Results[i] = message.ToolResultPart{
			ToolCallID: item.ToolCallID,
			ToolName:   item.ToolName,
			Output:     output,
		}
	}
	return nil
}
