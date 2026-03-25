package mcp

import (
	"context"
	"encoding/json"

	"github.com/obot-platform/discobot/agent-go/internal/api"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/thread"
)

// Executor wraps a ToolExecutor and routes "servername__toolname" calls to the MCP Manager.
// All other tool calls are delegated to the inner executor.
type Executor struct {
	inner   thread.ToolExecutor
	manager *Manager
}

// NewExecutor creates an Executor that routes MCP tool calls via manager.
func NewExecutor(inner thread.ToolExecutor, manager *Manager) *Executor {
	return &Executor{inner: inner, manager: manager}
}

// Execute routes MCP tool calls (name contains "__") to the Manager.
// All other calls are forwarded to the inner executor.
func (e *Executor) Execute(ctx context.Context, toolCtx *thread.ToolContext, call message.ToolCallPart) (thread.ToolExecuteResult, error) {
	if !IsMCPTool(call.ToolName) {
		return e.inner.Execute(ctx, toolCtx, call)
	}
	result, err := e.manager.CallTool(ctx, call.ToolName, json.RawMessage(call.Input), call.ToolCallID)
	if err != nil {
		return thread.ToolExecuteResult{}, err
	}
	return thread.ToolExecuteResult{Result: result}, nil
}

// ResolveAnswer delegates to the inner executor.
// MCP tools are synchronous and never require user approval.
func (e *Executor) ResolveAnswer(toolCtx *thread.ToolContext, call message.ToolCallPart, req api.AnswerQuestionRequest) (thread.ToolExecuteResult, error) {
	return e.inner.ResolveAnswer(toolCtx, call, req)
}

// ResumeAsync delegates to the inner executor.
// MCP tools are synchronous and never produce async tasks.
func (e *Executor) ResumeAsync(ctx context.Context, toolCtx *thread.ToolContext, call message.ToolCallPart, taskID string, req *api.AnswerQuestionRequest) (thread.ToolExecuteResult, error) {
	return e.inner.ResumeAsync(ctx, toolCtx, call, taskID, req)
}
