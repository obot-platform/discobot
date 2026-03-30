package handler

import (
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
// Each element is a single UIMessage encoded as JSON.
type ChatRequest struct {
	// Messages is optional for create-only requests. When omitted or [], the
	// handler creates/validates the session and returns an immediate empty SSE completion.
	Messages []json.RawMessage `json:"messages"`
	// Trigger indicates the type of request: "submit-message" or "regenerate-message"
	Trigger string `json:"trigger,omitempty"`
	// WorkspaceID is optional for new sessions.
	// If omitted, the server creates a local workspace under Discobot's data directory.
	WorkspaceID string `json:"workspaceId,omitempty"`
	// Model is optional for new sessions.
	Model string `json:"model,omitempty"`
	// Reasoning controls extended thinking. This is passed through as a string
	// reasoning level such as "auto", "low", "medium", "high", "xhigh",
	// "none", "default", or "" for model/provider default behavior.
	Reasoning string `json:"reasoning,omitempty"`
	// Mode is the permission mode: "plan" for planning mode, "" for default (build mode)
	Mode string `json:"mode,omitempty"`
}

type ChatResponse struct {
	WorkspaceID  string `json:"workspaceId"`
	SessionID    string `json:"sessionId"`
	ThreadID     string `json:"threadId"`
	MessageID    string `json:"messageId,omitempty"`
	CompletionID string `json:"completionId,omitempty"`
}

// Chat handles AI chat initiation.
// POST /api/projects/{projectId}/sessions/{sessionId}/threads/{threadId}/chat
// Request body: { messages, workspaceId?, trigger?, messageId?, model?, reasoning?, mode? }
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

	emptySubmission := len(req.Messages) == 0

	sessionID := r.PathValue("sessionId")
	if sessionID == "" {
		h.Error(w, http.StatusBadRequest, "sessionId is required")
		return
	}
	threadID := r.PathValue("threadId")
	if threadID == "" {
		h.Error(w, http.StatusBadRequest, "threadId is required")
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
		// For existing sessions, validate session resources still belong to project
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
		workspaceID, err := h.resolveWorkspaceIDForNewSession(ctx, projectID, req.WorkspaceID)
		if err != nil {
			h.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		sessionWorkspaceID = workspaceID

		_, err = h.chatService.NewSession(ctx, service.NewSessionRequest{
			SessionID:   sessionID,
			ProjectID:   projectID,
			WorkspaceID: workspaceID,
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
		MessageID:   lastUserMessageID(req.Messages),
	}

	if emptySubmission {
		h.JSON(w, http.StatusOK, response)
		return
	}

	// Use a context that won't be cancelled when the client disconnects.
	// This ensures sandbox creation and message sending complete even if the
	// client aborts the request. The explicit cancel endpoint
	// (/sessions/{sessionId}/threads/{threadId}/cancel) remains the way to stop a running chat completion.
	sendCtx := context.WithoutCancel(ctx)
	_, started, err := h.chatService.StartChat(sendCtx, projectID, sessionID, threadID, req.Messages, req.Model, req.Reasoning, req.Mode)
	if err != nil {
		log.Printf("[Chat] Failed to start chat for session %s: %v", sessionID, err)
		var startErr *service.SandboxChatStartError
		if errors.As(err, &startErr) {
			switch startErr.ErrorCode {
			case "completion_in_progress":
				h.JSON(w, http.StatusConflict, sandboxapi.ChatConflictResponse{
					Error:        startErr.ErrorCode,
					CompletionID: startErr.CompletionID,
				})
				return
			case "interrupted_turn_requires_resume", "pending_question_requires_answer":
				h.JSON(w, http.StatusConflict, sandboxapi.ChatTurnStateConflictResponse{
					Error:        startErr.ErrorCode,
					Message:      startErr.Message,
					QuestionID:   startErr.QuestionID,
					CompletionID: startErr.CompletionID,
				})
				return
			}
		}
		h.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	response.CompletionID = started.CompletionID

	h.JSON(w, http.StatusOK, response)
}

// ChatStream handles resuming a thread chat stream.
// GET /api/projects/{projectId}/sessions/{sessionId}/threads/{threadId}/stream
//
// Response: long-lived SSE stream for the thread, or 204 No Content if the
// thread/session cannot currently provide a stream
func (h *Handler) ChatStream(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projectID := middleware.GetProjectID(ctx)
	sessionID, threadID, _, ok := h.resolveSessionAndThread(w, r, projectID, true)
	if !ok {
		return
	}

	lastEventID := r.Header.Get("Last-Event-ID")

	// Get the stream from sandbox. Fresh requests replay persisted history by
	// default; valid Last-Event-ID reconnects continue from the requested offset.
	sseCh, err := h.chatService.GetStream(ctx, projectID, sessionID, threadID, lastEventID)
	if err != nil {
		// Sandbox unavailable or error - return 204 (no active stream)
		log.Printf("[ChatStream] Error getting stream: %v", err)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Store the first message if we consume one during this initial readiness check.
	var firstLine *service.SSELine
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
		if !firstLine.Done {
			writeStreamEvent(w, *firstLine)
			flusher.Flush()
		}
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
				return
			}
			if line.Done {
				continue
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

// ChatQuestion returns the current pending AskUserQuestion for a session thread.
// GET /api/projects/{projectId}/sessions/{sessionId}/threads/{threadId}/question/{questionId}
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

// ChatAnswer submits answers to a pending AskUserQuestion for a session thread.
// POST /api/projects/{projectId}/sessions/{sessionId}/threads/{threadId}/answer/{questionId}
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
// POST /api/projects/{projectId}/sessions/{sessionId}/threads/{threadId}/cancel
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

	h.JSON(w, http.StatusOK, result)
}

func (h *Handler) resolveSessionAndThread(w http.ResponseWriter, r *http.Request, projectID string, noContentOnMissing bool) (sessionID, threadID string, session *model.Session, ok bool) {
	ctx := r.Context()
	sessionID = r.PathValue("sessionId")
	if sessionID == "" {
		h.Error(w, http.StatusBadRequest, "sessionId is required")
		return "", "", nil, false
	}
	threadID = r.PathValue("threadId")
	if threadID == "" {
		h.Error(w, http.StatusBadRequest, "threadId is required")
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

	return sessionID, threadID, session, true
}

// lastUserMessageID returns the ID of the last user message in the slice, or "".
func lastUserMessageID(messages []json.RawMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		var m struct {
			ID   string `json:"id"`
			Role string `json:"role"`
		}
		if json.Unmarshal(messages[i], &m) == nil && m.Role == "user" && m.ID != "" {
			return m.ID
		}
	}
	return ""
}
