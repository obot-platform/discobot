package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/obot-platform/discobot/server/internal/middleware"
	"github.com/obot-platform/discobot/server/internal/providers"
	"github.com/obot-platform/discobot/server/internal/service"
)

// ModelsResponse contains the list of available models
type ModelsResponse struct {
	Models []ModelInfo `json:"models"`
}

// ModelInfo represents a model in the API response
type ModelInfo struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Provider         string   `json:"provider"`
	Description      string   `json:"description,omitempty"`
	Reasoning        bool     `json:"reasoning,omitempty"` // Whether model supports extended thinking
	ReasoningLevels  []string `json:"reasoningLevels,omitempty"`
	DefaultReasoning string   `json:"defaultReasoning,omitempty"`
}

// toModelInfos converts service models to API response models.
func toModelInfos(models []service.Model) []ModelInfo {
	modelInfos := make([]ModelInfo, len(models))
	for i, m := range models {
		modelInfos[i] = ModelInfo{
			ID:               m.ID,
			Name:             m.Name,
			Provider:         m.Provider,
			Description:      m.Description,
			Reasoning:        m.Reasoning,
			ReasoningLevels:  m.ReasoningLevels,
			DefaultReasoning: m.DefaultReasoning,
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

// GetSessionModels returns available models for a session based on its credentials
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

// GetAuthProviders returns available auth providers from models.dev data
func (h *Handler) GetAuthProviders(w http.ResponseWriter, _ *http.Request) {
	h.JSON(w, http.StatusOK, map[string]any{"authProviders": providers.GetAll()})
}
