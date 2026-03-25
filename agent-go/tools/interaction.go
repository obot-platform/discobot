package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/obot-platform/discobot/agent-go/internal/api"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/thread"
)

// AskUserQuestion — pauses the turn and presents questions to the user.
// The LLM sends a questions array; we route this through the ApprovalRequest
// mechanism so the handler can surface it to the client.

type askUserQuestionInput struct {
	Questions json.RawMessage `json:"questions"`
}

func (e *Executor) executeAskUserQuestion(call message.ToolCallPart) (thread.ToolExecuteResult, error) {
	var input askUserQuestionInput
	if err := unmarshalInput(call, &input); err != nil {
		return errResult(call, err.Error()), nil
	}
	if len(input.Questions) == 0 {
		return errResult(call, "questions is required"), nil
	}

	return thread.ToolExecuteResult{
		Approval: &thread.ApprovalRequest{
			Questions: input.Questions,
		},
	}, nil
}

// resolveAskUserQuestion converts the user's answers back into a tool result.
// answers is a map of question → answer strings.
func (e *Executor) resolveAskUserQuestion(call message.ToolCallPart, req api.AnswerQuestionRequest) (message.ToolResultPart, error) {
	// Re-parse the original questions so we can format a nice result.
	var input askUserQuestionInput
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
		return message.ToolResultPart{}, fmt.Errorf("re-parse AskUserQuestion input: %w", err)
	}

	// Merge questions with answers as JSON so the LLM can read them.
	type qaItem struct {
		Question string `json:"question"`
		Answer   string `json:"answer"`
	}

	// Parse the questions array to extract question texts.
	var questions []struct {
		Question string `json:"question"`
		Header   string `json:"header"`
	}
	_ = json.Unmarshal(input.Questions, &questions)

	// Build the merged Q&A output.
	merged := make([]qaItem, 0, len(req.Answers))
	for _, q := range questions {
		answer, ok := req.Answers[q.Question]
		if !ok {
			answer = req.Answers[q.Header]
		}
		merged = append(merged, qaItem{Question: q.Question, Answer: answer})
	}

	// Fall back: if no questions parsed, just include raw answers.
	if len(merged) == 0 {
		for q, a := range req.Answers {
			merged = append(merged, qaItem{Question: q, Answer: a})
		}
	}

	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return message.ToolResultPart{}, fmt.Errorf("marshal Q&A: %w", err)
	}

	return message.ToolResultPart{
		ToolCallID: call.ToolCallID,
		ToolName:   call.ToolName,
		Output:     message.TextOutput{Value: string(out)},
	}, nil
}

// EnterPlanMode — immediately activates plan mode and returns instructions.
// No user approval is required; the agent switches to plan mode unconditionally.

func (e *Executor) executeEnterPlanMode(toolCtx *thread.ToolContext, call message.ToolCallPart) (thread.ToolExecuteResult, error) {
	if toolCtx != nil {
		toolCtx.PlanMode = true
	}
	planFile := e.newPlanFilePath(toolCtx)
	result := fmt.Sprintf(`Plan mode activated. Plan file: %s

IMPORTANT: Do NOT output any text to the user right now. Make your next action a tool call (Glob, Grep, Read, etc.) to begin exploring the codebase. Do not narrate or announce your plans.

Workflow:
1. Use Glob, Grep, Read tools to explore silently
2. Use AskUserQuestion only if you need to clarify requirements
3. Write your complete plan to the plan file path above
4. Call ExitPlanMode when done — it will show the plan to the user for approval

Do NOT write code. Do NOT output text — start immediately with tool calls.`, planFile)

	return thread.ToolExecuteResult{
		Result: message.ToolResultPart{
			ToolCallID: call.ToolCallID,
			ToolName:   call.ToolName,
			Output:     message.TextOutput{Value: result},
		},
	}, nil
}

func (e *Executor) resolveEnterPlanMode(call message.ToolCallPart, _ api.AnswerQuestionRequest) (message.ToolResultPart, error) {
	// EnterPlanMode no longer uses the approval flow; this resolver is unreachable
	// but kept to satisfy the ResolveApproval dispatch table.
	return message.ToolResultPart{
		ToolCallID: call.ToolCallID,
		ToolName:   call.ToolName,
		Output:     message.TextOutput{Value: "Plan mode activated."},
	}, nil
}

// ExitPlanMode — signals the agent is done with the plan and ready to implement.

type exitPlanModeInput struct {
	AllowedPrompts []json.RawMessage `json:"allowedPrompts"`
}

func (e *Executor) executeExitPlanMode(toolCtx *thread.ToolContext, call message.ToolCallPart) (thread.ToolExecuteResult, error) {
	var input exitPlanModeInput
	_ = json.Unmarshal([]byte(call.Input), &input) // optional fields

	// Read the plan file — it must exist and have non-empty content.
	planFile := e.resolveActivePlanFile(toolCtx)
	planContent, err := os.ReadFile(planFile)
	if err != nil || len(strings.TrimSpace(string(planContent))) == 0 {
		return errResult(call, fmt.Sprintf(
			"No plan written yet. Write your complete plan to %s before calling ExitPlanMode.", planFile,
		)), nil
	}

	if toolCtx != nil && !toolCtx.PromptRequestPlanMode {
		toolCtx.PlanMode = false
		mode := "build"
		toolCtx.ModeChange = &mode

		result := "Plan mode exited. Continue forward and implement the plan now."
		if len(planContent) > 0 {
			result = fmt.Sprintf("Plan mode exited. Continue forward and implement the plan now.\n\nCurrent plan:\n\n%s", string(planContent))
		}

		return thread.ToolExecuteResult{
			Result: message.ToolResultPart{
				ToolCallID: call.ToolCallID,
				ToolName:   call.ToolName,
				Output:     message.TextOutput{Value: result},
			},
		}, nil
	}

	q := api.AskUserQuestion{
		Question: "Approve the plan and proceed with implementation?",
		Header:   "Plan approval",
		Options: []api.AskUserQuestionOption{
			{Label: "Approve", Description: "Approve the plan and let the agent implement it"},
			{Label: "Reject", Description: "Reject the plan and ask the agent to revise it"},
		},
		Notes: string(planContent),
	}
	prompt, err := json.Marshal([]api.AskUserQuestion{q})
	if err != nil {
		return thread.ToolExecuteResult{}, fmt.Errorf("marshal exit plan mode prompt: %w", err)
	}

	return thread.ToolExecuteResult{
		Approval: &thread.ApprovalRequest{
			Questions: prompt,
		},
	}, nil
}

func (e *Executor) resolveExitPlanMode(toolCtx *thread.ToolContext, call message.ToolCallPart, req api.AnswerQuestionRequest) (message.ToolResultPart, error) {
	approved := false
	var customFeedback string
	for _, v := range req.Answers {
		switch v {
		case "Approve", "approve", "yes", "true":
			approved = true
		case "Reject", "reject", "no", "false", "":
			// explicit rejection — no feedback
		default:
			// Custom text entered via the "Other" option.
			customFeedback = v
		}
	}

	planFile := e.resolveActivePlanFile(toolCtx)

	var result string
	if approved {
		if toolCtx != nil {
			toolCtx.PlanMode = false
			mode := "build"
			toolCtx.ModeChange = &mode
		}
		if planContent, err := os.ReadFile(planFile); err == nil {
			result = fmt.Sprintf("Plan approved. Continue forward and implement the plan now.\n\nApproved plan:\n\n%s", string(planContent))
		} else {
			result = "Plan approved. Continue forward and implement the plan now."
		}
	} else if customFeedback != "" {
		result = fmt.Sprintf("Plan feedback from user: %s\n\nRevise your plan file and call ExitPlanMode again when ready.", customFeedback)
	} else {
		result = "Plan rejected. Revise your plan file and call ExitPlanMode again when ready."
	}

	return message.ToolResultPart{
		ToolCallID: call.ToolCallID,
		ToolName:   call.ToolName,
		Output:     message.TextOutput{Value: result},
	}, nil
}
