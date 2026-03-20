package providers

import (
	"context"
	"encoding/json"
	"iter"

	"github.com/obot-platform/discobot/agent-go/message"
)

// ModelTaskType identifies the intended use of a model within a provider's
// default model map.
type ModelTaskType = string

const (
	// ModelTaskChat is the default general-purpose conversational/agent model.
	ModelTaskChat ModelTaskType = "chat"
)

// Provider is the interface that LLM provider implementations must satisfy.
// Each provider is identified by an ID matching its models.dev ID
// (e.g., "anthropic", "openai").
type Provider interface {
	// ID returns the provider identifier, matching the models.dev provider ID.
	ID() string

	// Complete sends messages to the LLM and returns a streaming iterator
	// of response chunks. The caller iterates with:
	//
	//   for chunk, err := range provider.Complete(ctx, req) {
	//       if err != nil { ... }
	//       // process chunk
	//   }
	//
	// The iterator stops when the response is complete, the context is
	// cancelled, or an error occurs. After an error is yielded, no
	// further chunks are produced.
	Complete(ctx context.Context, req CompleteRequest) iter.Seq2[message.ProviderMessageChunk, error]

	// ListModels returns the models available from this provider
	// with the current configuration/credentials.
	ListModels(ctx context.Context) ([]ModelInfo, error)

	// DefaultModels returns the provider's recommended models keyed by task type.
	// The only task type currently used is "chat".
	// Returns nil if the provider has no defaults.
	DefaultModels() map[string]ModelRef
}

// Config holds provider configuration, typically API keys and endpoint URLs.
// Keys are provider-specific (e.g., "api_key", "base_url").
type Config map[string]string

// APIKey returns the "api_key" config value.
func (c Config) APIKey() string {
	return c["api_key"]
}

func (c Config) Token() string {
	return c["auth_token"]
}

// BaseURL returns the "base_url" config value.
func (c Config) BaseURL() string {
	return c["base_url"]
}

// ToolFormat describes a custom tool input format.
type ToolFormat struct {
	Type       string `json:"type"`
	Syntax     string `json:"syntax"`
	Definition string `json:"definition"`
}

// ToolDefinition describes a tool available to the model.
type ToolDefinition struct {
	Type        string          `json:"type,omitempty"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"` // JSON Schema
	Format      *ToolFormat     `json:"format,omitempty"`
}

// CompleteRequest is the input to Provider.Complete.
type CompleteRequest struct {
	Model    ModelRef          `json:"model"`
	Messages []message.Message `json:"messages"`
	Tools    []ToolDefinition  `json:"tools,omitempty"`

	// Optional parameters. Nil means use provider default.
	MaxTokens   *int     `json:"maxTokens,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"topP,omitempty"`

	// Reasoning controls extended thinking. Empty (ReasoningEmpty) means use
	// the model's built-in default, which is the same as ReasoningDefault.
	// Providers translate this to their native API format.
	Reasoning Reasoning `json:"reasoning,omitempty"`

	// ProviderOptions is an opaque JSON blob for provider-specific parameters
	// that don't fit the common fields.
	ProviderOptions json.RawMessage `json:"providerOptions,omitempty"`
}
