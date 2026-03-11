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
	agentService      *AgentService
	credentialService *CredentialService
	sandboxService    *SandboxService
	agentTypes        []AgentType // Passed from handler since they're hardcoded there
}

// AgentType is defined in handler/agents.go but we need a copy here
// to avoid import cycles
type AgentType struct {
	ID                     string
	SupportedAuthProviders []string
	NoAuthProvider         string // Provider whose free models are always included without auth
}

// NewModelsService creates a new models service
func NewModelsService(s *store.Store, agentSvc *AgentService, credSvc *CredentialService, sandboxSvc *SandboxService, agentTypes []AgentType) *ModelsService {
	return &ModelsService{
		store:             s,
		agentService:      agentSvc,
		credentialService: credSvc,
		sandboxService:    sandboxSvc,
		agentTypes:        agentTypes,
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

// GetModelsForAgent returns available models based on agent's credentials
func (s *ModelsService) GetModelsForAgent(ctx context.Context, agentID, projectID string) ([]Model, error) {
	// Get the agent
	agent, err := s.agentService.GetAgent(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent: %w", err)
	}

	// Find the agent type configuration
	var agentType *AgentType
	for i := range s.agentTypes {
		if s.agentTypes[i].ID == agent.AgentType {
			agentType = &s.agentTypes[i]
			break
		}
	}
	if agentType == nil {
		return nil, fmt.Errorf("unknown agent type: %s", agent.AgentType)
	}

	// Get all credentials for the project
	credentials, err := s.credentialService.List(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to list credentials: %w", err)
	}

	// Filter credentials by agent's supported providers
	supportedProviders := make(map[string]bool)
	supportsAll := false
	for _, provider := range agentType.SupportedAuthProviders {
		if provider == "*" {
			supportsAll = true
			break
		}
		supportedProviders[provider] = true
	}

	// Collect provider IDs from configured credentials
	providerIDs := make([]string, 0)
	providerSet := make(map[string]bool) // Deduplicate

	for _, cred := range credentials {
		if !cred.IsConfigured {
			continue
		}

		// Check if this provider is supported by the agent
		if !supportsAll && !supportedProviders[cred.Provider] {
			continue
		}

		// Add to provider IDs if not already added
		if !providerSet[cred.Provider] {
			providerSet[cred.Provider] = true
			providerIDs = append(providerIDs, cred.Provider)
		}
	}

	// Get models for credential-based providers from models.dev data
	var models []Model
	if len(providerIDs) > 0 {
		providerModels, err := providers.GetModelsForProviders(providerIDs)
		if err != nil {
			return nil, fmt.Errorf("failed to get models: %w", err)
		}

		models = make([]Model, len(providerModels))
		for i, pm := range providerModels {
			models[i] = Model{
				ID:        pm.ID,
				Name:      pm.Name,
				Provider:  pm.Provider,
				Reasoning: pm.Reasoning,
			}
		}
	}

	// Always include free models from the no-auth provider (if configured)
	if agentType.NoAuthProvider != "" {
		freeModels, err := providers.GetFreeModelsForProvider(agentType.NoAuthProvider)
		if err == nil {
			seen := make(map[string]bool, len(models))
			for _, m := range models {
				seen[m.ID] = true
			}
			for _, fm := range freeModels {
				if !seen[fm.ID] {
					models = append(models, Model{
						ID:        fm.ID,
						Name:      fm.Name,
						Provider:  fm.Provider,
						Reasoning: fm.Reasoning,
					})
				}
			}
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

	// If session has no agent, return empty list
	if session.AgentID == nil {
		return []Model{}, nil
	}

	// Get the session client to communicate with the sandbox
	client, err := s.sandboxService.GetClient(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session client: %w", err)
	}

	// Try to call the sandbox's /models endpoint which queries the Claude API
	modelsResp, err := client.GetModels(ctx)
	if err != nil {
		// Fallback to models.dev data if sandbox call fails
		// This happens with OAuth tokens which can't query the models API
		return s.GetModelsForAgent(ctx, *session.AgentID, session.ProjectID)
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
