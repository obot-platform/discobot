package providers

import (
	"encoding/json"
	"log"
	"strings"
	"sync"

	"github.com/obot-platform/discobot/server/static"
)

// ModelInfo represents a model with its metadata
type ModelInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Family      string `json:"family,omitempty"`
	Provider    string `json:"provider"` // Set during parsing from provider ID
	Description string `json:"description,omitempty"`
	Reasoning   bool   `json:"reasoning"` // Whether model supports extended thinking
}

// modelsDevData represents the entire models.dev api.json structure
type modelsDevData map[string]providerWithModels

// providerWithModels represents a provider entry with its models
type providerWithModels struct {
	ID     string                   `json:"id"`
	Name   string                   `json:"name"`
	Models map[string]modelMetadata `json:"models"`
}

// modelCost represents the cost per million tokens
type modelCost struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}

// modelMetadata represents the raw model data from models.dev
type modelMetadata struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Family    string    `json:"family,omitempty"`
	Reasoning bool      `json:"reasoning"`
	ToolCall  bool      `json:"tool_call"`
	Cost      modelCost `json:"cost"`
}

// Provider aliases for model lookups in models-dev-api.json.
// Example: Codex credentials should surface OpenAI models.
var modelProviderAliases = map[string]string{
	"codex": "openai",
}

func resolveModelProviderID(providerID string) string {
	if alias, ok := modelProviderAliases[providerID]; ok {
		return alias
	}
	return providerID
}

func resolveProviderAndModelID(providerID, modelID string) (string, string) {
	resolvedProviderID := resolveModelProviderID(providerID)
	rawModelID := modelID
	if idx := strings.Index(rawModelID, "/"); idx >= 0 {
		rawModelID = rawModelID[idx+1:]
	}
	return resolvedProviderID, rawModelID
}

// IsProviderModelToolCallable reports whether a model supports tool calling.
func IsProviderModelToolCallable(providerID, modelID string) bool {
	loadModelsData()
	if modelsLoadErr != nil {
		return false
	}

	resolvedProviderID, rawModelID := resolveProviderAndModelID(providerID, modelID)
	provider, exists := cachedModels[resolvedProviderID]
	if !exists {
		return false
	}

	modelData, exists := provider.Models[rawModelID]
	if !exists {
		return false
	}

	return modelData.ToolCall
}

// Cached models data
var (
	modelsOnce    sync.Once
	cachedModels  modelsDevData
	modelsLoadErr error
)

// loadModelsData loads and caches the models.dev data
func loadModelsData() {
	modelsOnce.Do(func() {
		data, err := static.Files.ReadFile("models-dev-api.json")
		if err != nil {
			log.Printf("Warning: Failed to load models-dev-api.json: %v", err)
			modelsLoadErr = err
			return
		}

		if err := json.Unmarshal(data, &cachedModels); err != nil {
			log.Printf("Warning: Failed to parse models-dev-api.json: %v", err)
			modelsLoadErr = err
			return
		}
	})
}

// GetModelsForProviders returns all models for the given provider IDs
func GetModelsForProviders(providerIDs []string) ([]ModelInfo, error) {
	loadModelsData()

	if modelsLoadErr != nil {
		return nil, modelsLoadErr
	}

	// Create a map for fast provider lookup
	providerMap := make(map[string]bool)
	for _, id := range providerIDs {
		providerMap[resolveModelProviderID(id)] = true
	}

	var models []ModelInfo
	seen := make(map[string]bool) // Deduplicate models by ID

	for providerID, provider := range cachedModels {
		// Skip providers not in the requested list
		if !providerMap[providerID] {
			continue
		}

		// Extract all models for this provider
		for _, modelData := range provider.Models {
			if !modelData.ToolCall {
				continue
			}

			// Create fully qualified model ID: provider-id/model-id
			qualifiedID := providerID + "/" + modelData.ID

			// Skip if we've already seen this model ID
			if seen[qualifiedID] {
				continue
			}
			seen[qualifiedID] = true

			models = append(models, ModelInfo{
				ID:        qualifiedID,
				Name:      modelData.Name,
				Family:    modelData.Family,
				Provider:  provider.Name, // Use provider name, not ID
				Reasoning: modelData.Reasoning,
			})
		}
	}

	return models, nil
}

// GetFreeModelsForProvider returns models with zero cost for a specific provider
func GetFreeModelsForProvider(providerID string) ([]ModelInfo, error) {
	loadModelsData()

	if modelsLoadErr != nil {
		return nil, modelsLoadErr
	}

	resolvedProviderID := resolveModelProviderID(providerID)
	provider, exists := cachedModels[resolvedProviderID]
	if !exists {
		return nil, nil
	}

	var models []ModelInfo
	for _, modelData := range provider.Models {
		if modelData.Cost.Input == 0 && modelData.Cost.Output == 0 && modelData.ToolCall {
			models = append(models, ModelInfo{
				ID:        resolvedProviderID + "/" + modelData.ID,
				Name:      modelData.Name,
				Family:    modelData.Family,
				Provider:  provider.Name,
				Reasoning: modelData.Reasoning,
			})
		}
	}

	return models, nil
}

// GetAllModels returns all models across all providers
func GetAllModels() ([]ModelInfo, error) {
	loadModelsData()

	if modelsLoadErr != nil {
		return nil, modelsLoadErr
	}

	var models []ModelInfo
	seen := make(map[string]bool)

	for providerID, provider := range cachedModels {
		for _, modelData := range provider.Models {
			if !modelData.ToolCall {
				continue
			}

			// Create fully qualified model ID: provider-id/model-id
			qualifiedID := providerID + "/" + modelData.ID

			if seen[qualifiedID] {
				continue
			}
			seen[qualifiedID] = true

			models = append(models, ModelInfo{
				ID:        qualifiedID,
				Name:      modelData.Name,
				Family:    modelData.Family,
				Provider:  provider.Name,
				Reasoning: modelData.Reasoning,
			})
		}
	}

	return models, nil
}
