package handler

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/obot-platform/discobot/server/internal/middleware"
	"github.com/obot-platform/discobot/server/internal/sandbox/sandboxapi"
)

func threadErrorStatus(err error) int {
	if err == nil {
		return http.StatusInternalServerError
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "status 404"), strings.Contains(msg, "thread not found"), strings.Contains(msg, "not found"):
		return http.StatusNotFound
	case strings.Contains(msg, "status 409"), strings.Contains(msg, "already exists"), strings.Contains(msg, "conflict"):
		return http.StatusConflict
	case strings.Contains(msg, "status 400"), strings.Contains(msg, "invalid"), strings.Contains(msg, "required"):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// ListThreads lists all threads in a session's sandbox.
// GET /api/projects/{projectId}/sessions/{sessionId}/threads
func (h *Handler) ListThreads(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projectID := middleware.GetProjectID(ctx)
	sessionID := chi.URLParam(r, "sessionId")

	result, err := h.chatService.ListThreads(ctx, projectID, sessionID)
	if err != nil {
		h.Error(w, threadErrorStatus(err), err.Error())
		return
	}

	h.JSON(w, http.StatusOK, result)
}

// GetThread returns a single thread from a session's sandbox.
// GET /api/projects/{projectId}/sessions/{sessionId}/threads/{threadId}
func (h *Handler) GetThread(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projectID := middleware.GetProjectID(ctx)
	sessionID := chi.URLParam(r, "sessionId")
	threadID := chi.URLParam(r, "threadId")
	if threadID == "" {
		h.Error(w, http.StatusBadRequest, "threadId is required")
		return
	}

	result, err := h.chatService.GetThread(ctx, projectID, sessionID, threadID)
	if err != nil {
		h.Error(w, threadErrorStatus(err), err.Error())
		return
	}

	h.JSON(w, http.StatusOK, result)
}

// ListThreadMessages returns messages for a specific thread by querying the sandbox.
// GET /api/projects/{projectId}/sessions/{sessionId}/threads/{threadId}/messages
func (h *Handler) ListThreadMessages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projectID := middleware.GetProjectID(ctx)
	sessionID := chi.URLParam(r, "sessionId")
	threadID := chi.URLParam(r, "threadId")
	if threadID == "" {
		h.Error(w, http.StatusBadRequest, "threadId is required")
		return
	}

	messages, err := h.chatService.GetThreadMessages(ctx, projectID, sessionID, threadID)
	if err != nil {
		h.Error(w, threadErrorStatus(err), err.Error())
		return
	}

	h.JSON(w, http.StatusOK, map[string]any{"messages": messages})
}

// CreateThread creates a thread in a session's sandbox.
// POST /api/projects/{projectId}/sessions/{sessionId}/threads
func (h *Handler) CreateThread(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projectID := middleware.GetProjectID(ctx)
	sessionID := chi.URLParam(r, "sessionId")

	var req sandboxapi.CreateThreadRequest
	if err := h.DecodeJSON(r, &req); err != nil {
		h.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if strings.TrimSpace(req.ID) == "" {
		h.Error(w, http.StatusBadRequest, "id is required")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		req.Name = req.ID
	}

	result, err := h.chatService.CreateThread(ctx, projectID, sessionID, &req)
	if err != nil {
		h.Error(w, threadErrorStatus(err), err.Error())
		return
	}

	h.JSON(w, http.StatusCreated, result)
}

// UpdateThread updates a thread in a session's sandbox.
// PUT/PATCH /api/projects/{projectId}/sessions/{sessionId}/threads/{threadId}
func (h *Handler) UpdateThread(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projectID := middleware.GetProjectID(ctx)
	sessionID := chi.URLParam(r, "sessionId")
	threadID := chi.URLParam(r, "threadId")
	if threadID == "" {
		h.Error(w, http.StatusBadRequest, "threadId is required")
		return
	}

	var req sandboxapi.UpdateThreadRequest
	if err := h.DecodeJSON(r, &req); err != nil {
		h.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		h.Error(w, http.StatusBadRequest, "name is required")
		return
	}

	result, err := h.chatService.UpdateThread(ctx, projectID, sessionID, threadID, &req)
	if err != nil {
		h.Error(w, threadErrorStatus(err), err.Error())
		return
	}

	h.JSON(w, http.StatusOK, result)
}

// DeleteThread deletes a thread in a session's sandbox.
// DELETE /api/projects/{projectId}/sessions/{sessionId}/threads/{threadId}
func (h *Handler) DeleteThread(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projectID := middleware.GetProjectID(ctx)
	sessionID := chi.URLParam(r, "sessionId")
	threadID := chi.URLParam(r, "threadId")
	if threadID == "" {
		h.Error(w, http.StatusBadRequest, "threadId is required")
		return
	}

	result, err := h.chatService.DeleteThread(ctx, projectID, sessionID, threadID)
	if err != nil {
		h.Error(w, threadErrorStatus(err), err.Error())
		return
	}

	h.JSON(w, http.StatusOK, result)
}
