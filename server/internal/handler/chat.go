package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

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
	// ID is the chat/session ID (AI SDK sends this as "id")
	ID string `json:"id"`
	// Messages is the raw UIMessage array - passed through to sandbox as-is
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

// Chat handles AI chat streaming.
// POST /api/chat
// Request body: { id, messages, workspaceId?, agentId?, trigger?, messageId? }
// Response: SSE stream with AI SDK UI message protocol
func (h *Handler) Chat(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projectID := middleware.GetProjectID(ctx)

	// Parse request
	var req ChatRequest
	if err := h.DecodeJSON(r, &req); err != nil {
		h.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate messages is provided and not empty
	if len(req.Messages) == 0 || string(req.Messages) == "null" {
		h.Error(w, http.StatusBadRequest, "messages array required")
		return
	}

	// id (chat ID) is required - client generates IDs
	if req.ID == "" {
		h.Error(w, http.StatusBadRequest, "id is required")
		return
	}
	sessionID := req.ID

	// Check if session exists
	existingSession, err := h.chatService.GetSessionByID(ctx, sessionID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		// Unexpected error (DB failure, context cancelled, etc.) — don't fall through
		h.Error(w, http.StatusInternalServerError, "failed to look up session")
		return
	}

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
		// Block chat during commit states
		if existingSession.CommitStatus == "pending" || existingSession.CommitStatus == "committing" {
			h.Error(w, http.StatusConflict, "Cannot send messages while session is committing")
			return
		}
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

	// Set up SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering
	w.Header().Set("x-vercel-ai-ui-message-stream", "v1")

	// Create a context that won't be cancelled when the client disconnects.
	// This ensures sandbox creation and message sending complete even if the
	// client aborts the SSE request. The explicit cancel endpoint (/chat/{id}/cancel)
	// is the only way to stop a running chat completion.
	//
	// We derive a cancellable child context (streamCtx) so that when the handler
	// exits, we clean up the SSE reader goroutine via streamCancel().
	sendCtx := context.WithoutCancel(ctx)
	streamCtx, streamCancel := context.WithCancel(sendCtx)

	// Send messages to sandbox and get raw SSE stream
	// ChatService handles session state reconciliation (starting stopped containers, etc.)
	// Pass the model and reasoning flag from the request
	sseCh, err := h.chatService.SendToSandbox(streamCtx, projectID, sessionID, req.Messages, req.Model, req.Reasoning, req.Mode)
	if err != nil {
		streamCancel()
		writeSSEErrorAndDone(w, err.Error())
		return
	}

	// Track whether the completion finished naturally (DONE or channel closed).
	// Only reset session status to "ready" when it actually finishes.
	// If the client disconnects early, status stays "running" because
	// the sandbox is still processing the completion.
	completionDone := false
	defer func() {
		streamCancel()
		if completionDone {
			// Reset session status to ready after chat completion
			// Use sendCtx (not request ctx) since the request may already be cancelled
			if _, err := h.sessionService.UpdateStatus(sendCtx, projectID, sessionID, model.SessionStatusReady, nil); err != nil {
				log.Printf("[Chat] Warning: failed to reset session %s status to ready: %v", sessionID, err)
			}
		} else {
			log.Printf("[Chat] Client disconnected before completion finished for session %s, status remains running", sessionID)
		}
	}()

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.Error(w, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	// Pass through raw SSE lines from sandbox
	for {
		select {
		case <-ctx.Done():
			// Client disconnected
			log.Printf("[Chat] Client disconnected, stopping SSE stream")
			return
		case line, ok := <-sseCh:
			if !ok {
				// Channel closed without explicit DONE
				completionDone = true
				_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				return
			}
			if line.Done {
				// Container sent [DONE] signal
				completionDone = true
				log.Printf("[Chat] Received [DONE] signal from sandbox")
				_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				return
			}
			// Log error events for debugging
			if strings.Contains(line.Data, `"type":"error"`) {
				log.Printf("[Chat] Passing through error event: %s", line.Data)
			}
			// Pass through raw data line without parsing
			_, _ = fmt.Fprintf(w, "data: %s\n\n", line.Data)
			flusher.Flush()
		}
	}
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
	sessionID := r.PathValue("sessionId")

	if sessionID == "" {
		h.Error(w, http.StatusBadRequest, "sessionId is required")
		return
	}

	replay := r.URL.Query().Get("replay") == "true"

	// Validate session belongs to this project
	existingSession, err := h.chatService.GetSessionByID(ctx, sessionID)
	if err != nil {
		// No session = no stream
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if existingSession.ProjectID != projectID {
		h.Error(w, http.StatusForbidden, "session does not belong to this project")
		return
	}

	// Get the stream from sandbox
	sseCh, err := h.chatService.GetStream(ctx, projectID, sessionID, replay)
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

	// Send the first message if we consumed one during the check
	if firstLine != nil {
		if firstLine.Done {
			log.Printf("[ChatStream] Received [DONE] signal from sandbox (first line)")
			_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			return
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", firstLine.Data)
		flusher.Flush()
	}

	// Pass through remaining SSE lines from sandbox
	for {
		select {
		case <-ctx.Done():
			// Client disconnected
			log.Printf("[ChatStream] Client disconnected, stopping SSE stream")
			return
		case line, ok := <-sseCh:
			if !ok {
				// Channel closed without explicit DONE
				_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				return
			}
			if line.Done {
				log.Printf("[ChatStream] Received [DONE] signal from sandbox")
				_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				return
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", line.Data)
			flusher.Flush()
		}
	}
}

// ChatQuestion returns the current pending AskUserQuestion for a session.
// GET /api/chat/{sessionId}/question/{questionId}
// Returns { status: "pending", question: {...} } if that question is still waiting
// Returns { status: "answered", question: null } if already answered or unknown
func (h *Handler) ChatQuestion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projectID := middleware.GetProjectID(ctx)
	sessionID := r.PathValue("sessionId")

	if sessionID == "" {
		h.Error(w, http.StatusBadRequest, "sessionId is required")
		return
	}

	toolUseID := r.PathValue("questionId")

	result, err := h.chatService.GetQuestion(ctx, projectID, sessionID, toolUseID)
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
	sessionID := r.PathValue("sessionId")

	if sessionID == "" {
		h.Error(w, http.StatusBadRequest, "sessionId is required")
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

	result, err := h.chatService.AnswerQuestion(ctx, projectID, sessionID, &req)
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

// writeSSEError sends an error SSE event in UIMessage Stream format.
// This is used for Go server-originated errors (not pass-through from sandbox).
func writeSSEError(w http.ResponseWriter, errorText string) {
	data := map[string]string{"type": "error", "errorText": errorText}
	jsonData, err := json.Marshal(data)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", jsonData)
}

// ChatCancel handles cancelling an in-progress chat completion.
// POST /api/projects/{projectId}/chat/{sessionId}/cancel
func (h *Handler) ChatCancel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projectID := middleware.GetProjectID(ctx)
	sessionID := r.PathValue("sessionId")

	if sessionID == "" {
		h.Error(w, http.StatusBadRequest, "sessionId is required")
		return
	}

	// Validate session belongs to this project
	existingSession, err := h.chatService.GetSessionByID(ctx, sessionID)
	if err != nil {
		h.Error(w, http.StatusNotFound, "session not found")
		return
	}
	if existingSession.ProjectID != projectID {
		h.Error(w, http.StatusForbidden, "session does not belong to this project")
		return
	}

	// Cancel the completion
	result, err := h.chatService.CancelCompletion(ctx, projectID, sessionID)
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

// writeSSEErrorAndDone sends an error SSE event followed by the [DONE] signal.
// This ensures the AI SDK properly closes the stream after receiving the error.
func writeSSEErrorAndDone(w http.ResponseWriter, errorText string) {
	writeSSEError(w, errorText)
	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}
