package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/obot-platform/discobot/agent-go/agent"
	"github.com/obot-platform/discobot/agent-go/internal/api"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/thread"
)

// Task/Agent tool — launches a sub-agent for a complex task.
// Each task runs as an async operation with its own mini turn loop.

type taskInput struct {
	Description string `json:"description"`
	Prompt      string `json:"prompt"`

	// Agent-specific fields (from the Agent/Task tool schema).
	SubagentType string `json:"subagent_type"`
	MaxTurns     int    `json:"max_turns"`
}

type taskContinuation struct {
	SubThreadID string `json:"subThreadId"`
}

// taskRecord tracks an in-progress or completed Task.
type taskRecord struct {
	status  string // "pending", "in_progress", "waiting_for_answer", "completed", "failed"
	output  string
	created time.Time

	parentThreadID    string
	parentTaskID      string
	depth             int
	subThreadID       string
	pendingApprovalID string
	pendingQuestions  json.RawMessage

	mu     sync.Mutex
	done   chan struct{}
	cancel context.CancelFunc // non-nil for Task/Agent sub-agent tasks; called by TaskStop
}

// taskStore holds all tasks for this executor instance.
type taskStore struct {
	mu    sync.Mutex
	tasks map[string]*taskRecord
}

var globalTasks = &taskStore{tasks: make(map[string]*taskRecord)}

func newSubThreadID(parentThreadID string) string {
	return fmt.Sprintf("%s.sub.%d", parentThreadID, time.Now().UnixNano())
}

func subagentDepthFromThreadID(threadID string) int {
	return strings.Count(threadID, ".sub.")
}

func (e *Executor) executeTask(_ context.Context, toolCtx *thread.ToolContext, call message.ToolCallPart) (thread.ToolExecuteResult, error) {
	var input taskInput
	if err := unmarshalInput(call, &input); err != nil {
		return errResult(call, err.Error()), nil
	}

	prompt := input.Prompt
	if prompt == "" {
		prompt = input.Description
	}
	if prompt == "" {
		return errResult(call, "prompt or description is required for Task/Agent tool"), nil
	}

	if toolCtx == nil || toolCtx.Agent == nil {
		return errResult(call, "Task tool is not available: no sub-agent configured"), nil
	}
	subAgent := toolCtx.Agent
	currentThreadID := contextThreadID(toolCtx, e.defaultThreadID)
	childDepth := toolCtx.SubagentDepth + 1
	if toolCtx.MaxSubagentDepth > 0 && childDepth > toolCtx.MaxSubagentDepth {
		return errResult(call, fmt.Sprintf("Task tool is not available: max sub-agent depth %d reached", toolCtx.MaxSubagentDepth)), nil
	}

	subThreadID := newSubThreadID(currentThreadID)

	rec := &taskRecord{
		status:         "in_progress",
		created:        time.Now(),
		parentThreadID: currentThreadID,
		parentTaskID:   toolCtx.CurrentTaskID,
		depth:          childDepth,
		subThreadID:    subThreadID,
	}

	globalTasks.mu.Lock()
	globalTasks.tasks[subThreadID] = rec
	globalTasks.mu.Unlock()

	startSubAgentRun(rec, subAgent, subThreadID, agent.PromptRequest{
		UserParts:     []message.UIPart{message.UITextPart{Text: prompt}},
		SubagentType:  input.SubagentType,
		ParentTaskID:  subThreadID,
		SubagentDepth: rec.depth,
		MaxTurns:      input.MaxTurns,
	})

	return thread.ToolExecuteResult{Async: taskHandle(call, rec, subThreadID)}, nil
}

func marshalTaskContinuation(subThreadID string) json.RawMessage {
	if subThreadID == "" {
		return nil
	}
	data, err := json.Marshal(taskContinuation{SubThreadID: subThreadID})
	if err != nil {
		return nil
	}
	return data
}

func unmarshalTaskContinuation(data json.RawMessage) (string, error) {
	var cont taskContinuation
	if err := json.Unmarshal(data, &cont); err != nil {
		return "", fmt.Errorf("invalid task continuation: %w", err)
	}
	if cont.SubThreadID == "" {
		return "", fmt.Errorf("invalid task continuation: missing subThreadId")
	}
	return cont.SubThreadID, nil
}

// continueTask re-attaches to an in-flight or completed task after a crash or
// after a question answer arrives.
func (e *Executor) continueTask(_ context.Context, toolCtx *thread.ToolContext, call message.ToolCallPart, continuation json.RawMessage, req *api.AnswerQuestionRequest) (thread.ToolExecuteResult, error) {
	subThreadID, err := unmarshalTaskContinuation(continuation)
	if err != nil {
		return thread.ToolExecuteResult{}, err
	}
	if toolCtx == nil || toolCtx.Agent == nil {
		return thread.ToolExecuteResult{
			Result: errorResult(call, fmt.Sprintf("sub-thread %s lost after crash (sub-agent not configured)", subThreadID)),
		}, nil
	}
	subAgent := toolCtx.Agent

	// Fast path: sub-thread is still alive in memory.
	globalTasks.mu.Lock()
	rec, ok := globalTasks.tasks[subThreadID]
	globalTasks.mu.Unlock()
	if ok {
		if req != nil {
			rec.mu.Lock()
			status := rec.status
			subThreadID := rec.subThreadID
			approvalID := rec.pendingApprovalID
			rec.mu.Unlock()

			if status != "waiting_for_answer" {
				return thread.ToolExecuteResult{
					Result: errorResult(call, fmt.Sprintf("sub-thread %s is not waiting for an answer", subThreadID)),
				}, nil
			}
			if approvalID == "" {
				pending, err := subAgent.PendingQuestion(subThreadID)
				if err != nil {
					return thread.ToolExecuteResult{}, fmt.Errorf("load sub-agent pending question: %w", err)
				}
				if pending == nil {
					return thread.ToolExecuteResult{
						Result: errorResult(call, fmt.Sprintf("sub-thread %s has no pending question", subThreadID)),
					}, nil
				}
				approvalID = pending.ApprovalID
			}
			if err := subAgent.SubmitAnswer(subThreadID, approvalID, *req); err != nil {
				return thread.ToolExecuteResult{}, fmt.Errorf("submit sub-agent answer: %w", err)
			}
			startSubAgentRun(rec, subAgent, subThreadID, agent.PromptRequest{ParentTaskID: subThreadID, SubagentDepth: rec.depth})
		}
		return thread.ToolExecuteResult{Async: taskHandle(call, rec, subThreadID)}, nil
	}

	// Check whether the sub-agent already completed before the crash.
	if output, err := subAgent.FinalResponse(subThreadID); err == nil && output != "" {
		return thread.ToolExecuteResult{
			Result: message.ToolResultPart{
				ToolCallID: call.ToolCallID,
				ToolName:   call.ToolName,
				Output:     message.TextOutput{Value: output},
			},
		}, nil
	}

	if req != nil {
		pending, err := subAgent.PendingQuestion(subThreadID)
		if err != nil {
			return thread.ToolExecuteResult{}, fmt.Errorf("load sub-agent pending question: %w", err)
		}
		if pending == nil {
			return thread.ToolExecuteResult{
				Result: errorResult(call, fmt.Sprintf("sub-thread %s has no pending question", subThreadID)),
			}, nil
		}

		rec = &taskRecord{
			status:         "in_progress",
			created:        time.Now(),
			parentThreadID: contextThreadID(toolCtx, e.defaultThreadID),
			parentTaskID:   toolCtx.CurrentTaskID,
			depth:          subagentDepthFromThreadID(subThreadID),
			subThreadID:    subThreadID,
		}
		globalTasks.mu.Lock()
		globalTasks.tasks[subThreadID] = rec
		globalTasks.mu.Unlock()

		if err := subAgent.SubmitAnswer(subThreadID, pending.ApprovalID, *req); err != nil {
			return thread.ToolExecuteResult{}, fmt.Errorf("submit sub-agent answer: %w", err)
		}
		startSubAgentRun(rec, subAgent, subThreadID, agent.PromptRequest{ParentTaskID: subThreadID, SubagentDepth: rec.depth})
		return thread.ToolExecuteResult{Async: taskHandle(call, rec, subThreadID)}, nil
	}

	// Sub-agent was mid-turn when the process crashed. Re-parse the original
	// input (persisted in the AsyncTaskInfo) and restart the goroutine.
	// DefaultAgent.Prompt detects the interrupted turn state and resumes it.
	var input taskInput
	if err := unmarshalInput(call, &input); err != nil {
		return thread.ToolExecuteResult{
			Result: errorResult(call, fmt.Sprintf("sub-thread %s: cannot recover input after crash: %v", subThreadID, err)),
		}, nil
	}
	prompt := input.Prompt
	if prompt == "" {
		prompt = input.Description
	}

	rec = &taskRecord{
		status:      "in_progress",
		created:     time.Now(),
		subThreadID: subThreadID,
	}
	globalTasks.mu.Lock()
	globalTasks.tasks[subThreadID] = rec
	globalTasks.mu.Unlock()

	startSubAgentRun(rec, subAgent, subThreadID, agent.PromptRequest{
		UserParts:     []message.UIPart{message.UITextPart{Text: prompt}},
		SubagentType:  input.SubagentType,
		ParentTaskID:  subThreadID,
		SubagentDepth: rec.depth,
		MaxTurns:      input.MaxTurns,
	})

	return thread.ToolExecuteResult{Async: taskHandle(call, rec, subThreadID)}, nil
}

func startSubAgentRun(rec *taskRecord, subAgent agent.Agent, subThreadID string, req agent.PromptRequest) {
	// Create the context before starting the goroutine so rec.cancel is always
	// set by the time taskHandle.Wait can observe it — no race on cancellation.
	subCtx, cancel := context.WithCancel(context.Background())

	rec.mu.Lock()
	rec.status = "in_progress"
	rec.output = ""
	rec.subThreadID = subThreadID
	rec.pendingApprovalID = ""
	rec.pendingQuestions = nil
	rec.done = make(chan struct{})
	rec.cancel = cancel
	rec.mu.Unlock()

	go runSubAgentGoroutine(subCtx, rec, subAgent, subThreadID, req)
}

// runSubAgentGoroutine runs a full agent turn loop for a sub-agent task.
// ctx is created by the caller before the goroutine starts so rec.cancel is
// always populated by the time taskHandle.Wait can fire.
func runSubAgentGoroutine(ctx context.Context, rec *taskRecord, subAgent agent.Agent, subThreadID string, req agent.PromptRequest) {
	defer close(rec.done)
	defer rec.cancel()

	// Drain the Prompt iterator to run the full multi-step turn loop.
	var runErr error
	for _, err := range subAgent.Prompt(ctx, subThreadID, req) {
		if err != nil {
			runErr = err
			break
		}
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()

	if runErr != nil {
		rec.status = "failed"
		rec.output = fmt.Sprintf("sub-agent failed: %v", runErr)
		return
	}

	pending, err := subAgent.PendingQuestion(subThreadID)
	if err != nil {
		rec.status = "failed"
		rec.output = fmt.Sprintf("sub-agent question lookup failed: %v", err)
		return
	}
	if pending != nil {
		questions, err := json.Marshal(pending.Questions)
		if err != nil {
			rec.status = "failed"
			rec.output = fmt.Sprintf("sub-agent question marshal failed: %v", err)
			return
		}
		rec.status = "waiting_for_answer"
		rec.pendingApprovalID = pending.ApprovalID
		rec.pendingQuestions = questions
		rec.output = ""
		return
	}

	// Read the final assistant text from the completed thread.
	output, err := subAgent.FinalResponse(subThreadID)
	if err != nil {
		rec.status = "failed"
		rec.output = fmt.Sprintf("sub-agent completed but result unavailable: %v", err)
		return
	}

	rec.status = "completed"
	rec.output = output
}

// taskHandle builds the AsyncContinuationHandle that the turn loop waits on.
func taskHandle(call message.ToolCallPart, rec *taskRecord, subThreadID string) *thread.AsyncContinuationHandle {
	continuation := marshalTaskContinuation(subThreadID)
	return &thread.AsyncContinuationHandle{
		Continuation: continuation,
		Wait: func(ctx context.Context) (thread.AsyncWaitResult, error) {
			select {
			case <-rec.done:
			case <-ctx.Done():
				// Cancel the sub-agent goroutine when the parent is cancelled.
				rec.mu.Lock()
				if rec.cancel != nil {
					rec.cancel()
				}
				rec.mu.Unlock()
				return thread.AsyncWaitResult{
					Result: errorResult(call, "task cancelled"),
				}, nil
			}
			rec.mu.Lock()
			defer rec.mu.Unlock()
			if rec.status == "failed" {
				return thread.AsyncWaitResult{
					Result: errorResult(call, rec.output),
				}, nil
			}
			if rec.status == "waiting_for_answer" {
				return thread.AsyncWaitResult{
					Approval: &thread.ApprovalRequest{
						Questions:    rec.pendingQuestions,
						Continuation: continuation,
					},
				}, nil
			}
			return thread.AsyncWaitResult{
				Result: message.ToolResultPart{
					ToolCallID: call.ToolCallID,
					ToolName:   call.ToolName,
					Output:     message.TextOutput{Value: rec.output},
				},
			}, nil
		},
	}
}

// --- TodoWrite ---

// globalTodos holds the current todo list managed by the TodoWrite tool.
type todoStore struct {
	mu    sync.Mutex
	todos []todoItem
}

type todoItem struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"activeForm"`
}

var globalTodos = &todoStore{}

type todoWriteInput struct {
	Todos []todoItem `json:"todos"`
}

func renderTodoWriteSummary(todos []todoItem) string {
	completed := 0
	inProgress := 0
	pending := 0

	for _, todo := range todos {
		switch todo.Status {
		case "completed":
			completed++
		case "in_progress":
			inProgress++
		default:
			pending++
		}
	}

	var b strings.Builder
	b.WriteString("Todo list updated.\n\n")
	if len(todos) == 0 {
		b.WriteString("Current status is empty.")
		return b.String()
	}

	b.WriteString(fmt.Sprintf(
		"Current status is %d completed, %d in progress, and %d pending.\n\n",
		completed,
		inProgress,
		pending,
	))
	b.WriteString("### Current tasks\n")
	for _, todo := range todos {
		content := strings.TrimSpace(todo.Content)
		if content == "" {
			content = "Untitled task"
		}

		switch todo.Status {
		case "completed":
			b.WriteString("- [x] ")
			b.WriteString(content)
		case "in_progress":
			b.WriteString("- [ ] ")
			b.WriteString(content)
			activeForm := strings.TrimSpace(todo.ActiveForm)
			if activeForm != "" && activeForm != content {
				b.WriteString(" _(in progress: ")
				b.WriteString(activeForm)
				b.WriteString(")_")
			} else {
				b.WriteString(" _(in progress)_")
			}
		default:
			b.WriteString("- [ ] ")
			b.WriteString(content)
		}
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

func (e *Executor) executeTodoWrite(_ context.Context, toolCtx *thread.ToolContext, call message.ToolCallPart) (thread.ToolExecuteResult, error) {
	var input todoWriteInput
	if err := unmarshalInput(call, &input); err != nil {
		return errResult(call, err.Error()), nil
	}

	globalTodos.mu.Lock()
	globalTodos.todos = input.Todos
	globalTodos.mu.Unlock()

	// Persist to {dataDir}/todos/{threadID}.json so the API can read it.
	threadID := contextThreadID(toolCtx, e.defaultThreadID)
	dir := filepath.Join(e.dataDir, "todos")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errResult(call, fmt.Sprintf("failed to create todos dir: %v", err)), nil
	}
	data, _ := json.Marshal(input.Todos)
	if err := os.WriteFile(filepath.Join(dir, threadID+".json"), data, 0o644); err != nil {
		return errResult(call, fmt.Sprintf("failed to write todos: %v", err)), nil
	}

	return textResult(call, renderTodoWriteSummary(input.Todos)), nil
}

// --- TaskOutput ---

type taskOutputInput struct {
	ID string `json:"id"`
}

func (e *Executor) executeTaskOutput(call message.ToolCallPart) (thread.ToolExecuteResult, error) {
	var input taskOutputInput
	if err := unmarshalInput(call, &input); err != nil {
		return errResult(call, err.Error()), nil
	}

	globalTasks.mu.Lock()
	rec, ok := globalTasks.tasks[input.ID]
	globalTasks.mu.Unlock()

	if !ok {
		return errResult(call, fmt.Sprintf("task %s not found", input.ID)), nil
	}

	rec.mu.Lock()
	output := rec.output
	rec.mu.Unlock()

	if output == "" {
		return textResult(call, "(no output yet)"), nil
	}
	return textResult(call, output), nil
}

// --- TaskStop ---

type taskStopInput struct {
	ID string `json:"id"`
}

func (e *Executor) executeTaskStop(call message.ToolCallPart) (thread.ToolExecuteResult, error) {
	var input taskStopInput
	if err := unmarshalInput(call, &input); err != nil {
		return errResult(call, err.Error()), nil
	}

	globalTasks.mu.Lock()
	rec, ok := globalTasks.tasks[input.ID]
	globalTasks.mu.Unlock()

	if !ok {
		return errResult(call, fmt.Sprintf("task %s not found", input.ID)), nil
	}

	rec.mu.Lock()
	defer rec.mu.Unlock()

	if rec.status != "completed" && rec.status != "failed" {
		rec.status = "failed"
		rec.output += "\n[Task stopped by agent]"
		if rec.cancel != nil {
			rec.cancel()
		}
		select {
		case <-rec.done:
		default:
			close(rec.done)
		}
	}

	return textResult(call, fmt.Sprintf("Task %s stopped", input.ID)), nil
}
