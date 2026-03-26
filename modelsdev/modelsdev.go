// Package modelsdev provides access to merged model metadata from models.dev
// combined with a static overlay of custom fields (reasoning levels, default
// reasoning level, custom tool support) that models.dev does not supply.
//
// Both the base models.dev API snapshot (models-dev-api.json) and the overlay
// (model-overlay.json) are embedded at compile time. The overlay is applied
// once on first use; callers see a single, already-merged view.
//
// Typical usage:
//
//	if md := modelsdev.Lookup("openai", "gpt-5.2"); md != nil {
//	    fmt.Println(md.ReasoningLevels)   // ["low","medium","high","xhigh"]
//	    fmt.Println(md.DefaultReasonLevel) // "medium"
//	}
package modelsdev

import (
	_ "embed"
	"encoding/json"
	"log"
	"strings"
	"sync"
)

// providerOverlayMeta holds optional provider-level fields that can be added or
// overridden from model-overlay.json. Use the reserved key "$provider" inside a
// provider's overlay block to supply these fields.
type providerOverlayMeta struct {
	Name string   `json:"name"`
	API  string   `json:"api"`
	Env  []string `json:"env"`
	Doc  string   `json:"doc"`
}

//go:embed models-dev-api.json
var baseJSON []byte

//go:embed model-overlay.json
var overlayJSON []byte

// ModelCost holds the per-token cost for a model.
type ModelCost struct {
	Input  float64
	Output float64
}

// ModelInfo contains the merged metadata for a single model.
type ModelInfo struct {
	ID                 string
	Name               string
	Family             string
	Reasoning          bool
	ReasoningLevels    []string // valid reasoning level values for this model
	DefaultReasonLevel string   // default reasoning level (empty = provider decides)
	ContextWindow      int
	MaxOutputTokens    int
	InputModalities    []string
	OutputModalities   []string
	ToolCall           bool // whether this model supports tool/function calling
	CustomTools        bool // whether this model supports provider-specific custom grammar tools
	Cost               ModelCost
}

// ProviderInfo holds provider-level metadata from models.dev.
type ProviderInfo struct {
	ID      string
	Name    string
	API     string   // default base URL
	NPM     string   // npm package used by models.dev (e.g. "@ai-sdk/openai-compatible")
	EnvVars []string // required env var names (e.g. ["ANTHROPIC_API_KEY"])
	Doc     string   // documentation URL or description
}

// ── internal types ────────────────────────────────────────────────────────────

type rawData map[string]providerEntry

type providerEntry struct {
	ID     string                   `json:"id"`
	Name   string                   `json:"name"`
	API    string                   `json:"api"`
	Env    []string                 `json:"env"`
	NPM    string                   `json:"npm"`
	Doc    string                   `json:"doc"`
	Models map[string]modelMetadata `json:"models"`
}

type modelMetadata struct {
	ID                 string          `json:"id"`
	Name               string          `json:"name"`
	Family             string          `json:"family"`
	Reasoning          bool            `json:"reasoning"`
	ReasoningLevels    []string        `json:"reasoningLevels"`
	DefaultReasonLevel string          `json:"defaultReasonLevel"`
	ToolCall           bool            `json:"tool_call"`
	CustomTools        bool            `json:"customTools"`
	Cost               modelCost       `json:"cost"`
	Limit              modelLimit      `json:"limit"`
	Modalities         modelModalities `json:"modalities"`
}

type modelCost struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}

type modelModalities struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

type modelLimit struct {
	Context int `json:"context"`
	Output  int `json:"output"`
}

type overlayEntry struct {
	// Fields applied to both new and existing models.
	ReasoningLevels    []string `json:"reasoningLevels"`
	DefaultReasonLevel string   `json:"defaultReasonLevel"`
	// Fields only meaningful when creating a new model (not present in base data).
	Name        string `json:"name"`
	Family      string `json:"family"`
	Reasoning   *bool  `json:"reasoning"`
	ToolCall    *bool  `json:"tool_call"`
	CustomTools *bool  `json:"customTools"`
}

// ── singleton ─────────────────────────────────────────────────────────────────

var (
	once    sync.Once
	data    rawData
	loadErr error
)

func load() {
	once.Do(func() {
		if err := json.Unmarshal(baseJSON, &data); err != nil {
			log.Printf("modelsdev: failed to parse models-dev-api.json: %v", err)
			loadErr = err
			return
		}
		applyOverlay()
	})
}

// applyOverlay merges model-overlay.json into the loaded base data.
//
// For existing providers and models the overlay can update ReasoningLevels and
// DefaultReasonLevel (and optionally Name/Family/Reasoning/ToolCall/CustomTools).
//
// The overlay also supports adding entirely new providers and models that are
// not present in the base models.dev snapshot:
//   - To declare a new provider, add a "$provider" key inside its block with
//     the provider-level fields (name, api, env, doc).
//   - Model entries within the block will be created if they don't already
//     exist in the base data.
func applyOverlay() {
	// Parse as raw messages so we can handle the mixed-type "$provider" key.
	var raw map[string]map[string]json.RawMessage
	if err := json.Unmarshal(overlayJSON, &raw); err != nil {
		log.Printf("modelsdev: failed to parse model-overlay.json: %v", err)
		return
	}
	for providerID, rawModels := range raw {
		provider, exists := data[providerID]

		// Apply optional provider-level metadata from the "$provider" key.
		if rawMeta, ok := rawModels["$provider"]; ok {
			var meta providerOverlayMeta
			if err := json.Unmarshal(rawMeta, &meta); err != nil {
				log.Printf("modelsdev: failed to parse $provider for %q: %v", providerID, err)
			} else {
				if !exists {
					provider = providerEntry{
						ID:     providerID,
						Models: make(map[string]modelMetadata),
					}
					exists = true
				}
				if meta.Name != "" {
					provider.Name = meta.Name
				}
				if meta.API != "" {
					provider.API = meta.API
				}
				if len(meta.Env) > 0 {
					provider.Env = append([]string(nil), meta.Env...)
				}
				if meta.Doc != "" {
					provider.Doc = meta.Doc
				}
			}
		}

		if !exists {
			continue
		}

		for modelID, rawModel := range rawModels {
			if modelID == "$provider" {
				continue
			}
			var ov overlayEntry
			if err := json.Unmarshal(rawModel, &ov); err != nil {
				log.Printf("modelsdev: failed to parse overlay for %q/%q: %v", providerID, modelID, err)
				continue
			}
			m, ok := provider.Models[modelID]
			if !ok {
				// Create a new model entry from the overlay fields.
				m = modelMetadata{ID: modelID}
			}
			if ov.Name != "" {
				m.Name = ov.Name
			}
			if ov.Family != "" {
				m.Family = ov.Family
			}
			if ov.Reasoning != nil {
				m.Reasoning = *ov.Reasoning
			}
			if ov.ToolCall != nil {
				m.ToolCall = *ov.ToolCall
			}
			if ov.CustomTools != nil {
				m.CustomTools = *ov.CustomTools
			}
			if len(ov.ReasoningLevels) > 0 {
				m.ReasoningLevels = append([]string(nil), ov.ReasoningLevels...)
			}
			if ov.DefaultReasonLevel != "" {
				m.DefaultReasonLevel = ov.DefaultReasonLevel
			}
			provider.Models[modelID] = m
		}
		data[providerID] = provider
	}
}

// ── public API ────────────────────────────────────────────────────────────────

// AllProviders returns metadata for every provider in models.dev.
func AllProviders() []ProviderInfo {
	load()
	if loadErr != nil {
		return nil
	}
	result := make([]ProviderInfo, 0, len(data))
	for _, p := range data {
		result = append(result, ProviderInfo{
			ID:      p.ID,
			Name:    p.Name,
			API:     p.API,
			NPM:     p.NPM,
			EnvVars: p.Env,
			Doc:     p.Doc,
		})
	}
	return result
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
		Doc:     p.Doc,
	}
}

// ProvidersByNPM returns all providers whose npm field matches the given package
// name. Used to bulk-register providers that share a common API implementation.
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
				Doc:     p.Doc,
			})
		}
	}
	return result
}

// Lookup returns the merged model metadata for a specific provider and model ID.
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
	return toModelInfo(m)
}

// AllForProvider returns merged metadata for every model under the given
// provider ID. Returns nil if the provider is not found.
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
		models = append(models, *toModelInfo(m))
	}
	return models
}

// SupportsInputModality reports whether the model accepts the specified input
// modality (for example "image" or "pdf").
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

func toModelInfo(m modelMetadata) *ModelInfo {
	return &ModelInfo{
		ID:                 m.ID,
		Name:               m.Name,
		Family:             m.Family,
		Reasoning:          m.Reasoning,
		ReasoningLevels:    append([]string(nil), m.ReasoningLevels...),
		DefaultReasonLevel: m.DefaultReasonLevel,
		ContextWindow:      m.Limit.Context,
		MaxOutputTokens:    m.Limit.Output,
		InputModalities:    append([]string(nil), m.Modalities.Input...),
		OutputModalities:   append([]string(nil), m.Modalities.Output...),
		ToolCall:           m.ToolCall,
		CustomTools:        m.CustomTools,
		Cost:               ModelCost{Input: m.Cost.Input, Output: m.Cost.Output},
	}
}
