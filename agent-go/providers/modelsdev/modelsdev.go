// Package modelsdev provides access to embedded models.dev metadata.
// It loads model metadata (context window, max output tokens, reasoning
// support, display name) from the bundled models-dev-api.json file.
//
// Providers call Lookup(providerID, modelID) to enrich their model lists
// with data from models.dev rather than hardcoding it.
package modelsdev

import (
	"embed"
	"encoding/json"
	"log"
	"strings"
	"sync"
)

//go:embed models-dev-api.json
var fs embed.FS

// ModelInfo contains the metadata for a single model from models.dev.
type ModelInfo struct {
	ID               string
	Name             string
	Family           string
	Reasoning        bool
	ContextWindow    int
	MaxOutputTokens  int
	InputModalities  []string
	OutputModalities []string
}

type modelsDevData map[string]providerEntry

type providerEntry struct {
	ID     string                   `json:"id"`
	Name   string                   `json:"name"`
	API    string                   `json:"api"` // default base URL
	Env    []string                 `json:"env"` // required env var names (e.g. ["ANTHROPIC_API_KEY"])
	NPM    string                   `json:"npm"` // npm package used by models.dev (e.g. "@ai-sdk/openai-compatible")
	Models map[string]modelMetadata `json:"models"`
}

type modelMetadata struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Family     string          `json:"family"`
	Reasoning  bool            `json:"reasoning"`
	Limit      modelLimit      `json:"limit"`
	Modalities modelModalities `json:"modalities"`
}

type modelModalities struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

type modelLimit struct {
	Context int `json:"context"`
	Output  int `json:"output"`
}

var (
	once    sync.Once
	data    modelsDevData
	loadErr error
)

func load() {
	once.Do(func() {
		raw, err := fs.ReadFile("models-dev-api.json")
		if err != nil {
			log.Printf("modelsdev: failed to read embedded data: %v", err)
			loadErr = err
			return
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			log.Printf("modelsdev: failed to parse embedded data: %v", err)
			loadErr = err
		}
	})
}

// ProviderInfo holds provider-level metadata from models.dev.
type ProviderInfo struct {
	ID      string
	Name    string
	API     string   // default base URL
	NPM     string   // npm package used by models.dev (e.g. "@ai-sdk/openai-compatible")
	EnvVars []string // required env var names (e.g. ["ANTHROPIC_API_KEY"])
}

// LookupProvider returns provider-level metadata for the given provider ID.
// Returns nil if the provider is not found.
func LookupProvider(providerID string) *ProviderInfo {
	load()
	if loadErr != nil {
		return nil
	}
	p, ok := data[providerID]
	if !ok {
		return nil
	}
	return &ProviderInfo{
		ID:      p.ID,
		Name:    p.Name,
		API:     p.API,
		NPM:     p.NPM,
		EnvVars: p.Env,
	}
}

// ProvidersByNPM returns all providers whose npm field matches the given package name.
// This is used to bulk-register providers that share a common API implementation.
func ProvidersByNPM(npmPackage string) []ProviderInfo {
	load()
	if loadErr != nil {
		return nil
	}
	var result []ProviderInfo
	for _, p := range data {
		if p.NPM == npmPackage {
			result = append(result, ProviderInfo{
				ID:      p.ID,
				Name:    p.Name,
				API:     p.API,
				NPM:     p.NPM,
				EnvVars: p.Env,
			})
		}
	}
	return result
}

// Lookup returns model metadata for a specific provider and model ID.
// Returns nil if the provider or model is not found.
func Lookup(providerID, modelID string) *ModelInfo {
	load()
	if loadErr != nil {
		return nil
	}
	provider, ok := data[providerID]
	if !ok {
		return nil
	}
	m, ok := provider.Models[modelID]
	if !ok {
		return nil
	}
	return &ModelInfo{
		ID:               m.ID,
		Name:             m.Name,
		Family:           m.Family,
		Reasoning:        m.Reasoning,
		ContextWindow:    m.Limit.Context,
		MaxOutputTokens:  m.Limit.Output,
		InputModalities:  append([]string(nil), m.Modalities.Input...),
		OutputModalities: append([]string(nil), m.Modalities.Output...),
	}
}

// AllForProvider returns all model metadata for a given provider ID.
// Returns nil if the provider is not found.
func AllForProvider(providerID string) []ModelInfo {
	load()
	if loadErr != nil {
		return nil
	}
	provider, ok := data[providerID]
	if !ok {
		return nil
	}
	models := make([]ModelInfo, 0, len(provider.Models))
	for _, m := range provider.Models {
		models = append(models, ModelInfo{
			ID:               m.ID,
			Name:             m.Name,
			Family:           m.Family,
			Reasoning:        m.Reasoning,
			ContextWindow:    m.Limit.Context,
			MaxOutputTokens:  m.Limit.Output,
			InputModalities:  append([]string(nil), m.Modalities.Input...),
			OutputModalities: append([]string(nil), m.Modalities.Output...),
		})
	}
	return models
}

// SupportsInputModality reports whether the model accepts the specified input
// modality (for example: "image" or "pdf").
func (m *ModelInfo) SupportsInputModality(modality string) bool {
	if m == nil {
		return false
	}
	modality = strings.TrimSpace(strings.ToLower(modality))
	if modality == "" {
		return false
	}
	for _, candidate := range m.InputModalities {
		if strings.EqualFold(candidate, modality) {
			return true
		}
	}
	return false
}
