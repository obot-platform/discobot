package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/obot-platform/discobot/agent-go/agent"
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

// taskRecord tracks an in-progress or completed Task.
type taskRecord struct {
	id      string
	status  string // "pending", "in_progress", "completed", "failed"
	output  string
	created time.Time

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

func newTaskID() string {
	return fmt.Sprintf("task-%d", time.Now().UnixNano())
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

	taskID := newTaskID()
	subThreadID := fmt.Sprintf("%s.sub.%s", contextThreadID(toolCtx, e.defaultThreadID), taskID)

	rec := &taskRecord{
		id:      taskID,
		status:  "in_progress",
		created: time.Now(),
		done:    make(chan struct{}),
	}

	globalTasks.mu.Lock()
	globalTasks.tasks[taskID] = rec
	globalTasks.mu.Unlock()

	// Create the context before starting the goroutine so rec.cancel is always
	// set by the time taskHandle.Wait can observe it — no race on cancellation.
	subCtx, cancel := context.WithCancel(context.Background())
	rec.cancel = cancel

	go runSubAgentGoroutine(subCtx, rec, subAgent, subThreadID, prompt, input.SubagentType, input.MaxTurns)

	return thread.ToolExecuteResult{Async: taskHandle(call, rec, taskID)}, nil
}

// resumeTask re-attaches to an in-flight or completed task after a crash.
func (e *Executor) resumeTask(_ context.Context, toolCtx *thread.ToolContext, call message.ToolCallPart, taskID string) (thread.ToolExecuteResult, error) {
	// Fast path: task is still alive in memory.
	globalTasks.mu.Lock()
	rec, ok := globalTasks.tasks[taskID]
	globalTasks.mu.Unlock()
	if ok {
		return thread.ToolExecuteResult{Async: taskHandle(call, rec, taskID)}, nil
	}

	if toolCtx == nil || toolCtx.Agent == nil {
		return thread.ToolExecuteResult{
			Result: errorResult(call, fmt.Sprintf("task %s lost after crash (sub-agent not configured)", taskID)),
		}, nil
	}
	subAgent := toolCtx.Agent

	subThreadID := fmt.Sprintf("%s.sub.%s", contextThreadID(toolCtx, e.defaultThreadID), taskID)

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

	// Sub-agent was mid-turn when the process crashed. Re-parse the original
	// input (persisted in the AsyncTaskInfo) and restart the goroutine.
	// DefaultAgent.Prompt detects the interrupted turn state and resumes it.
	var input taskInput
	if err := unmarshalInput(call, &input); err != nil {
		return thread.ToolExecuteResult{
			Result: errorResult(call, fmt.Sprintf("task %s: cannot recover input after crash: %v", taskID, err)),
		}, nil
	}
	prompt := input.Prompt
	if prompt == "" {
		prompt = input.Description
	}

	rec = &taskRecord{
		id:      taskID,
		status:  "in_progress",
		created: time.Now(),
		done:    make(chan struct{}),
	}
	globalTasks.mu.Lock()
	globalTasks.tasks[taskID] = rec
	globalTasks.mu.Unlock()

	subCtx, cancel := context.WithCancel(context.Background())
	rec.cancel = cancel

	go runSubAgentGoroutine(subCtx, rec, subAgent, subThreadID, prompt, input.SubagentType, input.MaxTurns)

	return thread.ToolExecuteResult{Async: taskHandle(call, rec, taskID)}, nil
}

// runSubAgentGoroutine runs a full agent turn loop for a sub-agent task.
// ctx is created by the caller before the goroutine starts so rec.cancel is
// always populated by the time taskHandle.Wait can fire.
func runSubAgentGoroutine(ctx context.Context, rec *taskRecord, subAgent agent.Agent, subThreadID, prompt, subagentType string, maxTurns int) {
	defer close(rec.done)
	defer rec.cancel()

	req := agent.PromptRequest{
		UserParts:    []message.UIPart{message.UITextPart{Text: prompt}},
		SubagentType: subagentType,
		MaxTurns:     maxTurns,
	}

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

// taskHandle builds the AsyncTaskHandle that the turn loop waits on.
func taskHandle(call message.ToolCallPart, rec *taskRecord, taskID string) *thread.AsyncTaskHandle {
	return &thread.AsyncTaskHandle{
		TaskID: taskID,
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

	out, _ := json.Marshal(map[string]bool{"success": true})
	return textResult(call, string(out)), nil
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
