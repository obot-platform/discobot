package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/obot-platform/discobot/server/internal/middleware"
	"github.com/obot-platform/discobot/server/internal/service"
)

// ModelsResponse contains the list of available models
type ModelsResponse struct {
	Models []ModelInfo `json:"models"`
}

// ModelInfo represents a model in the API response
type ModelInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Provider    string `json:"provider"`
	Description string `json:"description,omitempty"`
	Reasoning   bool   `json:"reasoning,omitempty"` // Whether model supports extended thinking
}

// toModelInfos converts service models to API response models.
func toModelInfos(models []service.Model) []ModelInfo {
	modelInfos := make([]ModelInfo, len(models))
	for i, m := range models {
		modelInfos[i] = ModelInfo{
			ID:          m.ID,
			Name:        m.Name,
			Provider:    m.Provider,
			Description: m.Description,
			Reasoning:   m.Reasoning,
		}
	}
	return modelInfos
}

// GetProjectModels returns available models for a project based on configured credentials.
func (h *Handler) GetProjectModels(w http.ResponseWriter, r *http.Request) {
	projectID := middleware.GetProjectID(r.Context())
	if projectID == "" {
		h.Error(w, http.StatusBadRequest, "Project ID is required")
		return
	}

	models, err := h.modelsService.GetModelsForProject(r.Context(), projectID)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, "Failed to get models for project")
		return
	}

	h.JSON(w, http.StatusOK, ModelsResponse{Models: toModelInfos(models)})
}

// GetAgentModels returns available models for an agent based on configured credentials
func (h *Handler) GetAgentModels(w http.ResponseWriter, r *http.Request) {
	projectID := middleware.GetProjectID(r.Context())
	agentID := chi.URLParam(r, "agentId")

	if agentID == "" {
		h.Error(w, http.StatusBadRequest, "Agent ID is required")
		return
	}

	models, err := h.modelsService.GetModelsForAgent(r.Context(), agentID, projectID)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, "Failed to get models for agent")
		return
	}

	h.JSON(w, http.StatusOK, ModelsResponse{Models: toModelInfos(models)})
}

// GetSessionModels returns available models for a session based on its agent and credentials
func (h *Handler) GetSessionModels(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "sessionId")

	if sessionID == "" {
		h.Error(w, http.StatusBadRequest, "Session ID is required")
		return
	}

	models, err := h.modelsService.GetModelsForSession(r.Context(), sessionID)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, "Failed to get models for session")
		return
	}

	h.JSON(w, http.StatusOK, ModelsResponse{Models: toModelInfos(models)})
}
