package providers

// ModelInfo describes a model available from a provider.
type ModelInfo struct {
	// ID is the model identifier as used in API calls (e.g., "claude-sonnet-4-20250514").
	// When returned by ProviderRegistry.ListModels, this is prefixed: "providerId/modelId".
	ID string `json:"id"`

	// ProviderID is the ID of the provider that offers this model.
	// Set by ProviderRegistry.ListModels; individual providers do not set this.
	ProviderID string `json:"providerId,omitempty"`

	// DisplayName is a human-readable name (e.g., "Claude Sonnet 4").
	DisplayName string `json:"displayName"`

	// Reasoning indicates whether the model supports extended thinking.
	Reasoning bool `json:"reasoning"`

	// ReasoningLevels lists the Reasoning values that this model accepts.
	// Empty means reasoning is not supported or the valid levels are not known.
	ReasoningLevels []Reasoning `json:"reasoningLevels,omitempty"`

	// DefaultReasoning is the reasoning level applied when the request does not
	// specify one (i.e. Reasoning is ReasoningEmpty or ReasoningDefault).
	// Empty means the provider chooses its own default.
	DefaultReasoning Reasoning `json:"defaultReasoning,omitempty"`

	// ContextWindow is the maximum context length in tokens. Zero means unknown.
	ContextWindow int `json:"contextWindow,omitempty"`

	// MaxOutputTokens is the maximum output tokens. Zero means unknown.
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
}
