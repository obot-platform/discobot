package service

import (
	"context"
	"fmt"

	"github.com/obot-platform/discobot/server/internal/providers"
	"github.com/obot-platform/discobot/server/internal/store"
)

// Model represents a model available for selection (for API responses)
type Model struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Provider    string `json:"provider"`
	Description string `json:"description,omitempty"`
	Reasoning   bool   `json:"reasoning,omitempty"` // Whether model supports extended thinking
}

// ModelsService handles model listing operations
type ModelsService struct {
	store             *store.Store
	credentialService *CredentialService
	sandboxService    *SandboxService
}

// NewModelsService creates a new models service
func NewModelsService(s *store.Store, credSvc *CredentialService, sandboxSvc *SandboxService) *ModelsService {
	return &ModelsService{
		store:             s,
		credentialService: credSvc,
		sandboxService:    sandboxSvc,
	}
}

// GetModelsForProject returns available models based only on configured project credentials.
func (s *ModelsService) GetModelsForProject(ctx context.Context, projectID string) ([]Model, error) {
	credentials, err := s.credentialService.List(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to list credentials: %w", err)
	}

	providerIDs := make([]string, 0)
	providerSet := make(map[string]bool)
	for _, cred := range credentials {
		if !cred.IsConfigured {
			continue
		}
		if providerSet[cred.Provider] {
			continue
		}
		providerSet[cred.Provider] = true
		providerIDs = append(providerIDs, cred.Provider)
	}

	if len(providerIDs) == 0 {
		return []Model{}, nil
	}

	providerModels, err := providers.GetModelsForProviders(providerIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get models: %w", err)
	}

	models := make([]Model, len(providerModels))
	for i, pm := range providerModels {
		models[i] = Model{
			ID:        pm.ID,
			Name:      pm.Name,
			Provider:  pm.Provider,
			Reasoning: pm.Reasoning,
		}
	}

	return models, nil
}

// GetModelsForSession returns available models for a session.
// It attempts to query the live Claude API via the sandbox, but falls back to models.dev
// data if that fails (e.g., OAuth tokens can't query the models API as of Jan 2026).
func (s *ModelsService) GetModelsForSession(ctx context.Context, sessionID string) ([]Model, error) {
	// Get the session
	session, err := s.store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	// Get the session client to communicate with the sandbox
	client, err := s.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session client: %w", err)
	}

	// Try to call the sandbox's /models endpoint which queries the Claude API
	modelsResp, err := client.GetModels(ctx)
	if err != nil {
		// Fallback to project credentials if sandbox call fails
		return s.GetModelsForProject(ctx, session.ProjectID)
	}

	// Convert to service Model type and keep only tool-capable models.
	models := make([]Model, 0, len(modelsResp.Models))
	for _, m := range modelsResp.Models {
		if !providers.IsProviderModelToolCallable(m.Provider, m.ID) {
			continue
		}
		models = append(models, Model{
			ID:          m.ID,
			Name:        m.DisplayName,
			Provider:    m.Provider,
			Description: "",          // Claude API doesn't provide description
			Reasoning:   m.Reasoning, // Extended thinking support
		})
	}

	return models, nil
}
