package handler

import (
	"net/http"

	"github.com/obot-platform/discobot/agent-go/internal/api"
	"github.com/obot-platform/discobot/agent-go/providers"
)

// ListModels handles GET /threads/{id}/models — lists available models.
func (h *Handler) ListModels(w http.ResponseWriter, r *http.Request) {
	models, err := h.completions.ListModels(r.Context())
	if err != nil {
		h.Error(w, http.StatusInternalServerError, "failed to list models: "+err.Error())
		return
	}

	resp := api.ModelsResponse{
		Models: make([]api.ModelInfo, len(models)),
	}
	for i, m := range models {
		resp.Models[i] = api.ModelInfo{
			ID:               m.ID,
			DisplayName:      m.DisplayName,
			Provider:         m.ProviderID,
			Type:             "model",
			Reasoning:        m.Reasoning,
			ReasoningLevels:  reasoningLevelsToStrings(m.ReasoningLevels),
			DefaultReasoning: string(m.DefaultReasoning),
		}
	}

	h.JSON(w, http.StatusOK, resp)
}

// reasoningLevelsToStrings converts []providers.Reasoning to []string for the API layer.
func reasoningLevelsToStrings(levels []providers.Reasoning) []string {
	if len(levels) == 0 {
		return nil
	}
	out := make([]string, len(levels))
	for i, l := range levels {
		out[i] = string(l)
	}
	return out
}
