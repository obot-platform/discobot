package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/obot-platform/discobot/server/internal/middleware"
	"github.com/obot-platform/discobot/server/internal/model"
	"github.com/obot-platform/discobot/server/internal/sandbox/sandboxapi"
	"github.com/obot-platform/discobot/server/internal/service"
	"github.com/obot-platform/discobot/server/internal/store"
)

// ChatRequest represents the request body for the chat endpoint.
// This matches the AI SDK's DefaultChatTransport format.
// The Messages field is kept as raw JSON to pass through to the sandbox
// without requiring the Go server to understand the UIMessage structure.
type ChatRequest struct {
	// ID is the legacy chat/session ID field used by current clients.
	ID string `json:"id"`
	// SessionID is the preferred explicit session identifier.
	SessionID string `json:"sessionId,omitempty"`
	// ThreadID is optional. When omitted, the session ID is used.
	ThreadID string `json:"threadId,omitempty"`
	// Messages is optional for create-only requests. When omitted, null, or [], the
	// handler creates/validates the session and returns an immediate empty SSE completion.
	Messages json.RawMessage `json:"messages"`
	// Trigger indicates the type of request: "submit-message" or "regenerate-message"
	Trigger string `json:"trigger,omitempty"`
	// MessageID is the ID of the message to regenerate (for regenerate-message trigger)
	MessageID string `json:"messageId,omitempty"`
	// WorkspaceID is optional for new sessions.
	// If omitted, the server creates a local workspace under Discobot's data directory.
	WorkspaceID string `json:"workspaceId,omitempty"`
	// AgentID is required for new sessions
	AgentID string `json:"agentId,omitempty"`
	// Model is optional, if not provided uses agent's default model
	Model string `json:"model,omitempty"`
	// Reasoning controls extended thinking: "enabled", "disabled", or "" for default
	Reasoning string `json:"reasoning,omitempty"`
	// Mode is the permission mode: "plan" for planning mode, "" for default (build mode)
	Mode string `json:"mode,omitempty"`
}

type ChatResponse struct {
	WorkspaceID string `json:"workspaceId"`
	SessionID   string `json:"sessionId"`
	ThreadID    string `json:"threadId"`
	MessageID   string `json:"messageId,omitempty"`
}

// Chat handles AI chat initiation.
// POST /api/chat
// Request body: { id, messages, workspaceId?, agentId?, trigger?, messageId? }
// Response: JSON metadata for the initiated chat request
func (h *Handler) Chat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projectID := middleware.GetProjectID(ctx)

	// Parse request
	var req ChatRequest
	if err := h.DecodeJSON(r, &req); err != nil {
		h.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	emptySubmission := isEmptyChatMessages(req.Messages)

	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = req.ID
	}
	threadID := resolveChatThreadID(sessionID, req.ThreadID)

	// session ID is required - client generates IDs
	if sessionID == "" {
		h.Error(w, http.StatusBadRequest, "id is required")
		return
	}
	// Check if session exists
	existingSession, err := h.chatService.GetSessionByID(ctx, sessionID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		// Unexpected error (DB failure, context cancelled, etc.) — don't fall through
		h.Error(w, http.StatusInternalServerError, "failed to look up session")
		return
	}

	sessionWorkspaceID := ""

	if existingSession != nil {
		// Session exists - validate it belongs to this project
		if existingSession.ProjectID != projectID {
			h.Error(w, http.StatusForbidden, "session does not belong to this project")
			return
		}
		// For existing sessions, validate workspace and agent still belong to project
		if err := h.chatService.ValidateSessionResources(ctx, projectID, existingSession); err != nil {
			h.Error(w, http.StatusForbidden, err.Error())
			return
		}
		// Block chat during commit states for real prompt submissions.
		if !emptySubmission && (existingSession.CommitStatus == "pending" || existingSession.CommitStatus == "committing") {
			h.Error(w, http.StatusConflict, "Cannot send messages while session is committing")
			return
		}
		sessionWorkspaceID = existingSession.WorkspaceID
	} else {
		// Session doesn't exist - create it
		if req.AgentID == "" {
			h.Error(w, http.StatusBadRequest, "agentId is required for new sessions")
			return
		}

		workspaceID, err := h.resolveWorkspaceIDForNewSession(ctx, projectID, req.WorkspaceID)
		if err != nil {
			h.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		sessionWorkspaceID = workspaceID

		// NewSession validates that workspace and agent belong to project
		_, err = h.chatService.NewSession(ctx, service.NewSessionRequest{
			SessionID:   sessionID,
			ProjectID:   projectID,
			WorkspaceID: workspaceID,
			AgentID:     req.AgentID,
			Model:       req.Model,
			Reasoning:   req.Reasoning,
			Mode:        req.Mode,
			Messages:    req.Messages,
		})
		if err != nil {
			h.Error(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	response := ChatResponse{
		WorkspaceID: sessionWorkspaceID,
		SessionID:   sessionID,
		ThreadID:    threadID,
		MessageID:   extractSubmittedMessageID(req.Messages, req.MessageID),
	}

	if emptySubmission {
		h.JSON(w, http.StatusOK, response)
		return
	}

	// Use a context that won't be cancelled when the client disconnects.
	// This ensures sandbox creation and message sending complete even if the
	// client aborts the request. The explicit cancel endpoint (/chat/{id}/cancel)
	// remains the way to stop a running chat completion.
	sendCtx := context.WithoutCancel(ctx)
	go func() {
		if _, err := h.chatService.StartChat(sendCtx, projectID, sessionID, threadID, req.Messages, req.Model, req.Reasoning, req.Mode); err != nil {
			log.Printf("[Chat] Failed to start chat for session %s: %v", sessionID, err)
		}
	}()

	h.JSON(w, http.StatusOK, response)
}

// ChatStream handles resuming an in-progress chat stream.
// GET /api/chat/{sessionId}/stream
// Query params:
//   - replay=true: stream the last completed turn even if no completion is active
//
// Response: SSE stream if completion in progress (or replay requested), 204 No Content if not
func (h *Handler) ChatStream(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projectID := middleware.GetProjectID(ctx)
	sessionID, threadID, existingSession, ok := h.resolveSessionAndThread(w, r, projectID, true)
	if !ok {
		return
	}

	replay := r.URL.Query().Get("replay") == "true"
	lastEventID := r.Header.Get("Last-Event-ID")

	// Get the stream from sandbox
	sseCh, err := h.chatService.GetStream(ctx, projectID, sessionID, threadID, replay, lastEventID)
	if err != nil {
		// Sandbox unavailable or error - return 204 (no active stream)
		log.Printf("[ChatStream] Error getting stream: %v", err)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Check if channel is already closed (no active completion).
	// With replay=true this check is skipped — the channel will carry completed chunks.
	// Store the first message if we consume one during this check.
	var firstLine *service.SSELine
	if !replay {
		select {
		case line, ok := <-sseCh:
			if !ok {
				// Channel closed immediately - no active stream
				w.WriteHeader(http.StatusNoContent)
				return
			}
			// We consumed a message - store it to send after setting headers
			firstLine = &line
		default:
			// Channel not ready yet - we have a stream, set up SSE
		}
	}

	// Mark session as running if it isn't already. The session status poller
	// handles resetting back to ready once the agent-api completion finishes.
	if existingSession.Status != model.SessionStatusRunning {
		if _, err := h.sessionService.UpdateStatus(ctx, projectID, sessionID, model.SessionStatusRunning, nil); err != nil {
			log.Printf("[ChatStream] Warning: failed to update session %s status to running: %v", sessionID, err)
		}
	}

	// Set up SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("x-vercel-ai-ui-message-stream", "v1")

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.Error(w, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	// Send the first event if we consumed one during the check
	if firstLine != nil {
		if firstLine.Done {
			log.Printf("[ChatStream] Received done signal from sandbox (first line)")
			writeStreamEvent(w, *firstLine)
			flusher.Flush()
			return
		}
		writeStreamEvent(w, *firstLine)
		flusher.Flush()
	}

	// Pass through remaining SSE events from sandbox
	for {
		select {
		case <-ctx.Done():
			// Client disconnected
			log.Printf("[ChatStream] Client disconnected, stopping SSE stream")
			return
		case line, ok := <-sseCh:
			if !ok {
				// Channel closed without explicit done event.
				writeStreamEvent(w, service.SSELine{Event: "done", Data: `{}`, Done: true})
				flusher.Flush()
				return
			}
			if line.Done {
				log.Printf("[ChatStream] Received done signal from sandbox")
				writeStreamEvent(w, line)
				flusher.Flush()
				return
			}
			writeStreamEvent(w, line)
			flusher.Flush()
		}
	}
}

func writeStreamEvent(w http.ResponseWriter, line service.SSELine) {
	if line.ID != "" {
		_, _ = fmt.Fprintf(w, "id: %s\n", line.ID)
	}
	if line.Event != "" {
		_, _ = fmt.Fprintf(w, "event: %s\n", line.Event)
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", line.Data)
}

// ChatQuestion returns the current pending AskUserQuestion for a session.
// GET /api/chat/{sessionId}/question/{questionId}
// Returns { status: "pending", question: {...} } if that question is still waiting
// Returns { status: "answered", question: null } if already answered or unknown
func (h *Handler) ChatQuestion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projectID := middleware.GetProjectID(ctx)
	sessionID, threadID, _, ok := h.resolveSessionAndThread(w, r, projectID, false)
	if !ok {
		return
	}

	toolUseID := r.PathValue("questionId")

	result, err := h.chatService.GetQuestion(ctx, projectID, sessionID, threadID, toolUseID)
	if err != nil {
		log.Printf("[ChatQuestion] Error: %v", err)
		h.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.JSON(w, http.StatusOK, result)
}

// ChatAnswer submits answers to a pending AskUserQuestion for a session.
// POST /api/chat/{sessionId}/answer/{questionId}
func (h *Handler) ChatAnswer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projectID := middleware.GetProjectID(ctx)
	sessionID, threadID, _, ok := h.resolveSessionAndThread(w, r, projectID, false)
	if !ok {
		return
	}

	questionID := r.PathValue("questionId")
	if questionID == "" {
		h.Error(w, http.StatusBadRequest, "questionId is required")
		return
	}

	var req sandboxapi.AnswerQuestionRequest
	if err := h.DecodeJSON(r, &req); err != nil {
		h.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	req.ToolUseID = questionID

	if req.Answers == nil {
		h.Error(w, http.StatusBadRequest, "answers is required")
		return
	}

	result, err := h.chatService.AnswerQuestion(ctx, projectID, sessionID, threadID, &req)
	if err != nil {
		if errors.Is(err, service.ErrNoActiveCompletion) {
			h.Error(w, http.StatusNotFound, "no pending question for this toolUseID")
			return
		}
		log.Printf("[ChatAnswer] Error: %v", err)
		h.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.JSON(w, http.StatusOK, result)
}

// ChatCancel handles cancelling an in-progress chat completion.
// POST /api/projects/{projectId}/chat/{sessionId}/cancel
func (h *Handler) ChatCancel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projectID := middleware.GetProjectID(ctx)
	sessionID, threadID, _, ok := h.resolveSessionAndThread(w, r, projectID, false)
	if !ok {
		return
	}

	// Cancel the completion
	result, err := h.chatService.CancelCompletion(ctx, projectID, sessionID, threadID)
	if err != nil {
		if errors.Is(err, service.ErrNoActiveCompletion) {
			h.Error(w, http.StatusConflict, "no active completion to cancel")
			return
		}
		h.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Update session status to ready after successful cancellation
	// UpdateStatus now automatically publishes SSE event
	if _, err := h.sessionService.UpdateStatus(ctx, projectID, sessionID, model.SessionStatusReady, nil); err != nil {
		log.Printf("[ChatCancel] Warning: failed to reset session %s status to ready: %v", sessionID, err)
	}

	h.JSON(w, http.StatusOK, result)
}

func resolveChatThreadID(sessionID, threadID string) string {
	if threadID != "" {
		return threadID
	}
	// TODO: Remove the sessionID fallback once clients migrate to explicit
	// thread-scoped chat APIs under /sessions/{sessionId}/threads/{threadId}/...
	return sessionID
}

func (h *Handler) resolveSessionAndThread(w http.ResponseWriter, r *http.Request, projectID string, noContentOnMissing bool) (sessionID, threadID string, session *model.Session, ok bool) {
	ctx := r.Context()
	sessionID = r.PathValue("sessionId")
	if sessionID == "" {
		h.Error(w, http.StatusBadRequest, "sessionId is required")
		return "", "", nil, false
	}

	session, err := h.chatService.GetSessionByID(ctx, sessionID)
	if err != nil {
		if noContentOnMissing {
			w.WriteHeader(http.StatusNoContent)
		} else {
			h.Error(w, http.StatusNotFound, "session not found")
		}
		return "", "", nil, false
	}
	if session.ProjectID != projectID {
		h.Error(w, http.StatusForbidden, "session does not belong to this project")
		return "", "", nil, false
	}

	threadID = resolveChatThreadID(sessionID, r.PathValue("threadId"))
	return sessionID, threadID, session, true
}

func extractSubmittedMessageID(messages json.RawMessage, fallback string) string {
	if fallback != "" {
		return fallback
	}
	if len(messages) == 0 {
		return ""
	}

	var rawMessages []struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	}
	if err := json.Unmarshal(messages, &rawMessages); err != nil {
		return ""
	}

	for i := len(rawMessages) - 1; i >= 0; i-- {
		if rawMessages[i].Role == "user" && rawMessages[i].ID != "" {
			return rawMessages[i].ID
		}
	}
	return ""
}

func isEmptyChatMessages(messages json.RawMessage) bool {
	trimmed := bytes.TrimSpace(messages)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || bytes.Equal(trimmed, []byte("[]"))
}
