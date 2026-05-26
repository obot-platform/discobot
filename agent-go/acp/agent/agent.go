// Package agent adapts an Agent Client Protocol server to Discobot's Agent
// interface. It intentionally lives outside acp/client so the low-level ACP
// transport/protocol package stays separate from Discobot's agent semantics.
package agent

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	acpclient "github.com/obot-platform/discobot/agent-go/acp/client"
	"github.com/obot-platform/discobot/agent-go/acp/protocol"
	discobotagent "github.com/obot-platform/discobot/agent-go/agent"
	"github.com/obot-platform/discobot/agent-go/internal/api"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/thread"
)

const protocolVersion protocol.ProtocolVersion = 4

var errUnsupported = errors.New("acp agent: unsupported operation")
var errStreamStopped = errors.New("acp agent: stream stopped")

type promptEvent struct {
	chunk message.MessageChunk
	err   error
	park  bool
}

type promptResult struct {
	response protocol.PromptResponse
	err      error
}

type parkedPrompt struct {
	events      chan promptEvent
	done        chan promptResult
	cancel      context.CancelFunc
	projection  *sessionProjection
	userMessage message.UIMessage
}

// Agent is a minimal ACP-backed implementation of agent.Agent. It maps
// Discobot thread IDs to ACP session IDs and currently supports only the basic
// initialize, session/new, session/prompt, and session/cancel flow.
type Agent struct {
	client *acpclient.Client
	cwd    string
	store  ThreadStore

	sessionManager *sessionManager
}

// ThreadStore is the ACP adapter's minimal persistence boundary. It keeps ACP
// session mapping logic independent from the concrete thread.Store
// implementation while still using thread.Config as Discobot's typed thread
// data structure.
type ThreadStore interface {
	CreateThread(threadID string) error
	DeleteThread(threadID string) error
	ThreadExists(threadID string) (bool, error)
	ListThreads() ([]string, error)
	ListThreadInfos() ([]thread.Info, error)
	GetThreadInfo(threadID string) (thread.Info, error)
	CreateThreadInfo(defaultCWD string, req thread.CreateThreadRequest) (thread.Info, error)
	UpdateThreadInfo(threadID string, req thread.UpdateThreadRequest) (thread.Info, error)
	DeleteThreadInfo(threadID string) error
	LoadConfig(threadID string) (thread.Config, error)
	SaveConfig(threadID string, cfg thread.Config) error
}

var _ discobotagent.Agent = (*Agent)(nil)

// Connect creates an ACP client over transport, initializes it, and returns the
// Discobot adapter. The returned agent owns the ACP client and should be closed
// when the caller is done with it.
func Connect(ctx context.Context, transport mcp.Transport, cwd string, store ThreadStore) (*Agent, error) {
	acpClient, err := acpclient.Connect(ctx, transport)
	if err != nil {
		return nil, err
	}
	agent := New(acpClient, cwd, store)
	if err := agent.Initialize(ctx); err != nil {
		_ = acpClient.Close()
		return nil, err
	}
	return agent, nil
}

// New wraps an existing ACP client. Call Initialize before using Prompt unless
// the caller has already initialized the ACP connection.
func New(client *acpclient.Client, cwd string, store ThreadStore) *Agent {
	return &Agent{
		client:         client,
		cwd:            cwd,
		store:          store,
		sessionManager: newSessionManager(client, cwd, store),
	}
}

func (a *Agent) Store() ThreadStore {
	return a.store
}

// Initialize performs the ACP initialize handshake with Discobot's currently
// supported client capability surface. File-system and terminal capabilities are
// deliberately omitted until Discobot maps those features explicitly.
func (a *Agent) Initialize(ctx context.Context) error {
	result, err := a.client.Initialize(ctx, protocol.InitializeRequest{
		ProtocolVersion: protocolVersion,
		ClientInfo: &protocol.Implementation{
			Name:    "discobot-agent-go",
			Version: "dev",
		},
		ClientCapabilities: protocol.ClientCapabilities{},
	})
	if err != nil {
		return err
	}
	a.sessionManager.setCapabilities(
		result.AgentCapabilities.LoadSession,
		result.AgentCapabilities.SessionCapabilities.Resume != nil,
		result.AgentCapabilities.SessionCapabilities.List != nil,
	)
	return nil
}

// Close closes the underlying ACP client connection.
func (a *Agent) Close() error {
	return a.client.Close()
}

// Prompt sends a Discobot user prompt to the ACP session mapped to threadID.
func (a *Agent) Prompt(ctx context.Context, threadID string, req discobotagent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
	return func(yield func(message.MessageChunk, error) bool) {
		sessionID, err := a.sessionManager.ensure(ctx, threadID)
		if err != nil {
			thread.PersistError(a.store, threadID, err)
			yield(nil, err)
			return
		}
		thread.ClearError(a.store, threadID)
		prompt, err := contentBlocks(req.UserParts)
		if err != nil {
			thread.PersistError(a.store, threadID, err)
			yield(nil, err)
			return
		}

		stopCancel := context.AfterFunc(ctx, func() {
			cancelCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = a.client.Cancel(cancelCtx, protocol.CancelNotification{SessionID: sessionID})
		})
		defer stopCancel()

		if !yield(message.StartChunk{}, nil) {
			return
		}
		parked := a.startPrompt(ctx, threadID, sessionID, prompt, req)
		a.streamPromptUntilParked(threadID, parked, a.withThreadErrorPersistence(threadID, yield))
	}
}

func (a *Agent) Compact(_ context.Context, threadID string, _ discobotagent.PromptRequest) iter.Seq2[message.MessageChunk, error] {
	return func(yield func(message.MessageChunk, error) bool) {
		err := fmt.Errorf("%w: compact", errUnsupported)
		thread.PersistError(a.store, threadID, err)
		yield(nil, err)
	}
}

func (a *Agent) withThreadErrorPersistence(threadID string, yield func(message.MessageChunk, error) bool) func(message.MessageChunk, error) bool {
	return func(chunk message.MessageChunk, err error) bool {
		thread.PersistError(a.store, threadID, err)
		return yield(chunk, err)
	}
}

func (a *Agent) startPrompt(ctx context.Context, threadID string, sessionID protocol.SessionID, prompt []protocol.ContentBlock, req discobotagent.PromptRequest) *parkedPrompt {
	promptCtx, cancel := context.WithCancel(ctx)
	parked := &parkedPrompt{
		events:     make(chan promptEvent, 128),
		done:       make(chan promptResult, 1),
		cancel:     cancel,
		projection: newSessionProjection(),
		userMessage: message.UIMessage{
			ID:       "acp-" + discobotagent.GenerateID(),
			Role:     "user",
			Parts:    append([]message.UIPart(nil), req.UserParts...),
			Metadata: req.Metadata,
		},
	}
	go a.runPrompt(promptCtx, threadID, sessionID, prompt, parked)
	return parked
}

func (a *Agent) runPrompt(ctx context.Context, threadID string, sessionID protocol.SessionID, prompt []protocol.ContentBlock, parked *parkedPrompt) {
	defer close(parked.events)
	stopCancel := context.AfterFunc(ctx, func() {
		cancelCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.client.Cancel(cancelCtx, protocol.CancelNotification{SessionID: sessionID})
	})
	defer stopCancel()

	result, err := a.client.Prompt(ctx, protocol.PromptRequest{
		SessionID: sessionID,
		Prompt:    prompt,
	}, acpclient.WithOnUpdate(func(notification protocol.SessionNotification) error {
		threadUpdate, hasThreadUpdate := a.sessionManager.applyUpdate(threadID, notification)
		chunks, err := parked.projection.push(notification.Update)
		if err != nil {
			return err
		}
		for _, chunk := range chunks {
			if !sendPromptEvent(ctx, parked.events, promptEvent{chunk: chunk}) {
				return errStreamStopped
			}
		}
		if hasThreadUpdate && !sendPromptEvent(ctx, parked.events, promptEvent{chunk: threadUpdate}) {
			return errStreamStopped
		}
		return nil
	}), acpclient.WithOnRequestPermission(func(ctx context.Context, request protocol.RequestPermissionRequest) (protocol.RequestPermissionResponse, error) {
		chunks, approvalID, toolCallID, err := a.sessionManager.startPermissionRequest(threadID, request, parked)
		if err != nil {
			return protocol.RequestPermissionResponse{}, err
		}
		for _, chunk := range chunks {
			if !sendPromptEvent(ctx, parked.events, promptEvent{chunk: chunk}) {
				a.sessionManager.clearPendingPermission(threadID, approvalID)
				return protocol.RequestPermissionResponse{}, errStreamStopped
			}
		}
		if !sendPromptEvent(ctx, parked.events, promptEvent{park: true}) {
			a.sessionManager.clearPendingPermission(threadID, approvalID)
			return protocol.RequestPermissionResponse{}, errStreamStopped
		}
		response, approved, err := a.sessionManager.waitPermissionResponse(ctx, threadID, approvalID)
		if err != nil {
			return response, err
		}
		if !sendPromptEvent(ctx, parked.events, promptEvent{chunk: message.ToolApprovalResponseChunk{
			ApprovalID: approvalID,
			ToolCallID: toolCallID,
			Approved:   approved,
		}}) {
			return response, errStreamStopped
		}
		return response, nil
	}))
	if err == nil {
		for _, chunk := range parked.projection.closeAll() {
			if !sendPromptEvent(ctx, parked.events, promptEvent{chunk: chunk}) {
				err = errStreamStopped
				break
			}
		}
	}
	parked.done <- promptResult{response: result, err: err}
}

func (a *Agent) streamPromptUntilParked(threadID string, parked *parkedPrompt, yield func(message.MessageChunk, error) bool) {
	for event := range parked.events {
		if event.park {
			return
		}
		if !yield(event.chunk, event.err) || event.err != nil {
			parked.cancel()
			return
		}
	}
	a.finishPrompt(threadID, parked, <-parked.done, yield)
	parked.cancel()
}

func (a *Agent) streamPromptToCompletion(threadID string, parked *parkedPrompt, yield func(message.MessageChunk, error) bool) {
	defer parked.cancel()
	for event := range parked.events {
		if event.park {
			continue
		}
		if !yield(event.chunk, event.err) || event.err != nil {
			return
		}
	}
	a.finishPrompt(threadID, parked, <-parked.done, yield)
}

func (a *Agent) finishPrompt(threadID string, parked *parkedPrompt, result promptResult, yield func(message.MessageChunk, error) bool) {
	if errors.Is(result.err, errStreamStopped) {
		return
	}
	if appendErr := a.sessionManager.appendPromptMessages(threadID, parked.userMessage, parked.projection); appendErr != nil {
		yield(nil, appendErr)
		return
	}
	if result.err != nil {
		yield(nil, result.err)
		return
	}
	yield(message.ResponseFinishChunk{FinishReason: string(result.response.StopReason)}, nil)
}

func sendPromptEvent(ctx context.Context, events chan<- promptEvent, event promptEvent) bool {
	select {
	case events <- event:
		return true
	case <-ctx.Done():
		return false
	}
}

func (a *Agent) Cancel(threadID string) bool {
	return a.sessionManager.Cancel(threadID)
}

func (a *Agent) Messages(threadID, leafID string) ([]message.UIMessage, error) {
	return a.sessionManager.Messages(threadID, leafID)
}

func (a *Agent) ListThreads() ([]string, error) {
	return a.sessionManager.ListThreads()
}

func (a *Agent) Resume(_ context.Context, threadID string, req discobotagent.PromptRequest) (discobotagent.ResumeResult, error) {
	if len(req.UserParts) > 0 {
		return discobotagent.ResumeResult{}, fmt.Errorf("%w: resume with prompt", errUnsupported)
	}
	parked, err := a.sessionManager.resumePermission(threadID)
	if err != nil {
		return discobotagent.ResumeResult{}, err
	}
	thread.ClearError(a.store, threadID)
	return discobotagent.ResumeResult{
		Stream: func(yield func(message.MessageChunk, error) bool) {
			a.streamPromptToCompletion(threadID, parked, a.withThreadErrorPersistence(threadID, yield))
		},
	}, nil
}

func (a *Agent) HasInterruptedTurn(string) (bool, error) {
	return false, nil
}

func (a *Agent) PendingQuestion(threadID string) (*discobotagent.PendingQuestion, error) {
	return a.sessionManager.PendingQuestion(threadID)
}

func (a *Agent) SubmitAnswer(threadID, approvalID string, req api.AnswerQuestionRequest) error {
	return a.sessionManager.SubmitAnswer(threadID, approvalID, req)
}

func (a *Agent) FinalResponse(threadID string) (string, error) {
	messages, err := a.Messages(threadID, "")
	if err != nil {
		return "", err
	}
	for _, v := range slices.Backward(messages) {
		if v.Role != "assistant" {
			continue
		}
		var text strings.Builder
		for _, part := range v.Parts {
			if part, ok := part.(message.UITextPart); ok {
				text.WriteString(part.Text)
			}
		}
		return text.String(), nil
	}
	return "", nil
}

func (a *Agent) ListCommands() ([]discobotagent.Command, error) {
	return nil, nil
}
