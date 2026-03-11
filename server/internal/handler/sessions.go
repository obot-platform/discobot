package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/obot-platform/discobot/server/internal/jobs"
	"github.com/obot-platform/discobot/server/internal/middleware"
	"github.com/obot-platform/discobot/server/internal/service"
)

// GetSession returns a single session
func (h *Handler) GetSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")

	session, err := h.sessionService.GetSession(r.Context(), sessionID)
	if err != nil {
		h.Error(w, http.StatusNotFound, "Session not found")
		return
	}

	h.JSON(w, http.StatusOK, session)
}

// UpdateSession updates a session
func (h *Handler) UpdateSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")

	// Parse as map first to detect which fields are present
	var rawReq map[string]interface{}
	if err := h.DecodeJSON(r, &rawReq); err != nil {
		h.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Extract fields
	name, _ := rawReq["name"].(string)
	status, _ := rawReq["status"].(string)

	// Handle displayName: only process if key is present
	var displayName *string
	if displayNameValue, hasDisplayName := rawReq["displayName"]; hasDisplayName {
		if displayNameValue == nil {
			// Explicitly set to null - pass empty string to clear it
			emptyStr := ""
			displayName = &emptyStr
		} else if str, ok := displayNameValue.(string); ok {
			displayName = &str
		}
	}

	session, err := h.sessionService.UpdateSession(r.Context(), sessionID, name, displayName, status)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, "Failed to update session")
		return
	}

	h.JSON(w, http.StatusOK, session)
}

// DeleteSession initiates async deletion of a session
func (h *Handler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	ctx := r.Context()
	projectID := middleware.GetProjectID(ctx)

	if err := h.sessionService.DeleteSession(ctx, projectID, sessionID, h.jobQueue); err != nil {
		if strings.Contains(err.Error(), "not found") {
			h.Error(w, http.StatusNotFound, "Session not found")
			return
		}
		h.Error(w, http.StatusInternalServerError, "Failed to initiate session deletion")
		return
	}

	h.JSON(w, http.StatusOK, map[string]bool{"success": true})
}

// ListMessages returns messages for a session by querying the container.
func (h *Handler) ListMessages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projectID := middleware.GetProjectID(ctx)
	sessionID := chi.URLParam(r, "sessionId")

	messages, err := h.chatService.GetMessages(ctx, projectID, sessionID)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.JSON(w, http.StatusOK, map[string]any{"messages": messages})
}

// ListSessionsByWorkspace returns all sessions for a workspace.
func (h *Handler) ListSessionsByWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID := chi.URLParam(r, "workspaceId")

	sessions, err := h.sessionService.ListSessionsByWorkspace(r.Context(), workspaceID)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, "Failed to list sessions")
		return
	}

	h.JSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

// CommitSession initiates async commit of a session
func (h *Handler) CommitSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	ctx := r.Context()
	projectID := middleware.GetProjectID(ctx)

	if err := h.sessionService.CommitSession(ctx, projectID, sessionID, h.jobQueue); err != nil {
		if strings.Contains(err.Error(), "not found") {
			h.Error(w, http.StatusNotFound, "Session not found")
			return
		}
		if errors.Is(err, service.ErrSessionOperationInProgress) {
			h.Error(w, http.StatusConflict, "Session operation already in progress")
			return
		}
		h.Error(w, http.StatusInternalServerError, "Failed to initiate session commit")
		return
	}

	h.JSON(w, http.StatusOK, map[string]bool{"success": true})
}

// RebaseSession initiates async rebase of a session
func (h *Handler) RebaseSession(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")
	ctx := r.Context()
	projectID := middleware.GetProjectID(ctx)

	if err := h.sessionService.RebaseSession(ctx, projectID, sessionID, h.jobQueue); err != nil {
		if strings.Contains(err.Error(), "not found") {
			h.Error(w, http.StatusNotFound, "Session not found")
			return
		}
		if errors.Is(err, service.ErrSessionOperationInProgress) {
			h.Error(w, http.StatusConflict, "Session operation already in progress")
			return
		}
		h.Error(w, http.StatusInternalServerError, "Failed to initiate session rebase")
		return
	}

	h.JSON(w, http.StatusOK, map[string]bool{"success": true})
}

// CreateSessionRequest represents the request body for creating a session without sending a message.
type CreateSessionRequest struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId,omitempty"`
	AgentID     string `json:"agentId"`
	Model       string `json:"model,omitempty"`
	Reasoning   string `json:"reasoning,omitempty"`
}

func (h *Handler) resolveWorkspaceIDForNewSession(
	ctx context.Context,
	projectID string,
	workspaceID string,
) (string, error) {
	if workspaceID != "" {
		return workspaceID, nil
	}

	if h.cfg == nil || h.workspaceService == nil || h.jobQueue == nil || h.store == nil {
		return "", fmt.Errorf("automatic workspace creation is unavailable")
	}
	if strings.TrimSpace(h.cfg.WorkspaceDir) == "" {
		return "", fmt.Errorf("automatic workspace creation is unavailable: workspace dir is not configured")
	}

	autoPath := filepath.Join(h.cfg.WorkspaceDir, fmt.Sprintf("empty-workspace-%s", uuid.NewString()))
	workspace, err := h.workspaceService.CreateWorkspace(ctx, projectID, autoPath, "local", "")
	if err != nil {
		return "", fmt.Errorf("failed to create automatic workspace: %w", err)
	}

	workspaceModel, err := h.store.GetWorkspaceByID(ctx, workspace.ID)
	if err != nil {
		return "", fmt.Errorf("failed to load automatic workspace: %w", err)
	}
	workspaceModel.AutoGenerated = true
	if err := h.store.UpdateWorkspace(ctx, workspaceModel); err != nil {
		return "", fmt.Errorf("failed to flag automatic workspace: %w", err)
	}

	if err := h.jobQueue.Enqueue(ctx, jobs.WorkspaceInitPayload{ProjectID: projectID, WorkspaceID: workspace.ID}); err != nil {
		return "", fmt.Errorf("failed to enqueue automatic workspace initialization: %w", err)
	}

	return workspace.ID, nil
}

// CreateSession creates a new session without sending a chat message.
// This spins up a sandbox but does not invoke the LLM.
// POST /api/projects/{projectId}/sessions
func (h *Handler) CreateSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projectID := middleware.GetProjectID(ctx)

	var req CreateSessionRequest
	if err := h.DecodeJSON(r, &req); err != nil {
		h.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.ID == "" {
		h.Error(w, http.StatusBadRequest, "id is required")
		return
	}
	if req.AgentID == "" {
		h.Error(w, http.StatusBadRequest, "agentId is required")
		return
	}

	workspaceID, err := h.resolveWorkspaceIDForNewSession(ctx, projectID, req.WorkspaceID)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	sessionID, err := h.chatService.NewSession(ctx, service.NewSessionRequest{
		SessionID:   req.ID,
		ProjectID:   projectID,
		WorkspaceID: workspaceID,
		AgentID:     req.AgentID,
		Model:       req.Model,
		Reasoning:   req.Reasoning,
		Messages:    nil,
	})
	if err != nil {
		h.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	h.JSON(w, http.StatusOK, map[string]string{"id": sessionID})
}
