package providers

import (
	"strings"

	"github.com/obot-platform/discobot/modelsdev"
)

// ModelInfo represents a model with its metadata.
type ModelInfo struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Family             string   `json:"family,omitempty"`
	Provider           string   `json:"provider"` // Set during parsing from provider ID
	Description        string   `json:"description,omitempty"`
	Reasoning          bool     `json:"reasoning"` // Whether model supports extended thinking
	ReasoningLevels    []string `json:"reasoningLevels,omitempty"`
	DefaultReasonLevel string   `json:"defaultReasonLevel,omitempty"`
}

func rawModelID(modelID string) string {
	if idx := strings.Index(modelID, "/"); idx >= 0 {
		return modelID[idx+1:]
	}
	return modelID
}

// toServerModelInfo converts a modelsdev.ModelInfo into the server's ModelInfo,
// setting the qualified ID and provider display name.
func toServerModelInfo(providerID, providerName string, md modelsdev.ModelInfo) ModelInfo {
	return ModelInfo{
		ID:                 providerID + "/" + md.ID,
		Name:               md.Name,
		Family:             md.Family,
		Provider:           providerName,
		Reasoning:          md.Reasoning,
		ReasoningLevels:    append([]string(nil), md.ReasoningLevels...),
		DefaultReasonLevel: md.DefaultReasonLevel,
	}
}

// providerDisplayName returns the display name for the given provider ID.
func providerDisplayName(providerID string) string {
	if pi := modelsdev.LookupProvider(providerID); pi != nil {
		return pi.Name
	}
	return providerID
}

// ── public API ────────────────────────────────────────────────────────────────

// IsProviderModelToolCallable reports whether a model supports tool calling.
func IsProviderModelToolCallable(providerID, modelID string) bool {
	md := modelsdev.Lookup(providerID, rawModelID(modelID))
	return md != nil && md.ToolCall
}

// GetModelsForProviders returns all tool-callable models for the given provider IDs.
func GetModelsForProviders(providerIDs []string) ([]ModelInfo, error) {
	providerMap := make(map[string]bool, len(providerIDs))
	for _, id := range providerIDs {
		providerMap[id] = true
	}

	var models []ModelInfo
	seen := make(map[string]bool)

	for providerID := range providerMap {
		name := providerDisplayName(providerID)
		for _, md := range modelsdev.AllForProvider(providerID) {
			if !md.ToolCall {
				continue
			}
			qualifiedID := providerID + "/" + md.ID
			if seen[qualifiedID] {
				continue
			}
			seen[qualifiedID] = true
			models = append(models, toServerModelInfo(providerID, name, md))
		}
	}

	return models, nil
}

// GetFreeModelsForProvider returns tool-callable models with zero cost for a
// specific provider.
func GetFreeModelsForProvider(providerID string) ([]ModelInfo, error) {
	name := providerDisplayName(providerID)

	var models []ModelInfo
	for _, md := range modelsdev.AllForProvider(providerID) {
		if md.ToolCall && md.Cost.Input == 0 && md.Cost.Output == 0 {
			models = append(models, toServerModelInfo(providerID, name, md))
		}
	}
	return models, nil
}

// GetAllModels returns all tool-callable models across all providers.
func GetAllModels() ([]ModelInfo, error) {
	var models []ModelInfo
	seen := make(map[string]bool)

	for _, provider := range modelsdev.AllProviders() {
		for _, md := range modelsdev.AllForProvider(provider.ID) {
			if !md.ToolCall {
				continue
			}
			qualifiedID := provider.ID + "/" + md.ID
			if seen[qualifiedID] {
				continue
			}
			seen[qualifiedID] = true
			models = append(models, toServerModelInfo(provider.ID, provider.Name, md))
		}
	}
	return models, nil
}
