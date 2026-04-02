package providers

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/obot-platform/discobot/agent-go/internal/credentials"
	"github.com/obot-platform/discobot/modelsdev"
)

// ProviderRegistry builds and caches Provider instances on demand.
//
// Rather than requiring callers to pre-register providers at startup,
// Get builds a provider from the current credentials the first time
// it is requested and caches it. If the credentials change (e.g. via
// X-Discobot-Credentials on a subsequent request), the cached instance
// is discarded and a new one is built with the updated config.
//
// Add exists for tests and special cases where a pre-built provider instance
// must be injected directly.
type ProviderRegistry struct {
	mu      sync.RWMutex
	pinned  map[string]Provider       // injected via Add; never evicted
	cache   map[string]cachedProvider // built on demand from credentials
	credMgr *credentials.Manager
}

type cachedProvider struct {
	provider Provider
	cfg      Config // snapshot of config used to build this instance
}

// NewProviderRegistry creates an empty registry.
func NewProviderRegistry(credMgr *credentials.Manager) *ProviderRegistry {
	return &ProviderRegistry{
		pinned:  make(map[string]Provider),
		cache:   make(map[string]cachedProvider),
		credMgr: credMgr,
	}
}

// Add pins a pre-built provider instance. Pinned providers are always returned
// by Get without consulting the environment. Panics on duplicate ID.
func (r *ProviderRegistry) Add(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := p.ID()
	if _, exists := r.pinned[id]; exists {
		panic(fmt.Sprintf("provider already registered: %q", id))
	}
	r.pinned[id] = p
}

// Get returns a configured provider for id.
//
// Resolution order:
//  1. Pinned instance (from Add) — returned as-is.
//  2. Cached instance whose config still matches current credentials — returned as-is.
//  3. Build a new instance from current credentials and cache it.
//
// Returns an error if no factory is registered for id, or if no credentials
// are available for that provider.
func (r *ProviderRegistry) Get(id string) (Provider, error) {
	// Fast path: pinned instance (never stale).
	r.mu.RLock()
	if p, ok := r.pinned[id]; ok {
		r.mu.RUnlock()
		return p, nil
	}
	r.mu.RUnlock()

	if !Has(id) {
		return nil, fmt.Errorf("no provider factory registered for %q", id)
	}

	cfg := r.configForProvider(id)
	if len(cfg) == 0 {
		return nil, fmt.Errorf("no credentials configured for provider %q", id)
	}

	// Fast path: cached instance with unchanged config.
	r.mu.RLock()
	if cached, ok := r.cache[id]; ok && configEqual(cached.cfg, cfg) {
		r.mu.RUnlock()
		return cached.provider, nil
	}
	r.mu.RUnlock()

	// Slow path: build a new provider instance.
	p, err := New(id, cfg)
	if err != nil {
		return nil, fmt.Errorf("build provider %q: %w", id, err)
	}

	r.mu.Lock()
	r.cache[id] = cachedProvider{provider: p, cfg: cfg}
	r.mu.Unlock()

	return p, nil
}

// Resolve parses a "providerId/modelId" string, looks up the provider,
// and returns the provider and bare model ID.
func (r *ProviderRegistry) Resolve(modelRef string) (Provider, string, error) {
	ref, err := ParseModelRef(modelRef)
	if err != nil {
		return nil, "", err
	}
	p, err := r.Get(ref.ProviderID)
	if err != nil {
		return nil, "", err
	}
	return p, ref.ModelID, nil
}

// ResolveModel resolves a model reference string to a concrete ModelRef using
// provider default models when needed. taskType should be one of the
// ModelTask* constants (e.g. ModelTaskChat).
//
//   - ref == "":              find the first available provider that has a
//     default for taskType and return it.
//   - ref == "providerID":    use that provider's default for taskType.
//   - ref == "provider/model": parse and return as-is.
func (r *ProviderRegistry) ResolveModel(ref string, taskType string) (ModelRef, error) {
	return r.ResolveModelInProvider("", ref, taskType)
}

// ResolveModelInProvider resolves a model reference string relative to the
// given current provider when needed. taskType should be one of the ModelTask*
// constants (e.g. ModelTaskChat).
//
// Resolution order:
//   - ref == "":                    resolve the default for taskType
//   - ref == "providerID":          resolve that provider's default for taskType
//   - ref == "provider/model":      parse and return as-is
//   - ref == "model":               resolve as currentProviderID/model when currentProviderID is set
//   - ref == "supporting_model":    resolve the current provider's default for that supporting model type
func (r *ProviderRegistry) ResolveModelInProvider(currentProviderID, ref string, taskType string) (ModelRef, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		ids := r.IDs()
		if len(ids) == 0 {
			return ModelRef{}, fmt.Errorf("no model providers are available; configure a provider, set DISCOBOT_MODEL, or pass --model")
		}

		// Find the first available provider with a default for this task type.
		for _, id := range ids {
			p, err := r.Get(id)
			if err != nil {
				continue
			}
			if ref := p.DefaultModels()[taskType]; ref.ModelID != "" {
				return ref, nil
			}
		}
		return ModelRef{}, fmt.Errorf(
			"no provider available with a default %q model; available providers: %s; set DISCOBOT_MODEL or pass --model",
			taskType,
			strings.Join(ids, ", "),
		)
	}

	if strings.Contains(ref, "/") {
		// Fully-qualified "providerId/modelId".
		return ParseModelRef(ref)
	}

	if resolvedTaskType, ok := supportingModelTaskType(SupportingModelType(ref)); ok {
		if currentProviderID == "" {
			return ModelRef{}, fmt.Errorf("supporting model type %q requires a current provider", ref)
		}
		return r.ResolveModelInProvider("", currentProviderID, resolvedTaskType)
	}

	// Provider-only ref: look up that provider's default.
	if p, err := r.Get(ref); err == nil {
		modelRef := p.DefaultModels()[taskType]
		if modelRef.ModelID == "" {
			return ModelRef{}, fmt.Errorf("provider %q has no default %q model", ref, taskType)
		}
		return modelRef, nil
	}

	if currentProviderID != "" {
		return ModelRef{ProviderID: currentProviderID, ModelID: ref}, nil
	}

	// Fall back to the provider-only path so the caller gets the provider-focused
	// error when no current provider was available for a bare model ID.
	p, err := r.Get(ref)
	if err != nil {
		return ModelRef{}, fmt.Errorf("provider %q: %w", ref, err)
	}
	modelRef := p.DefaultModels()[taskType]
	if modelRef.ModelID == "" {
		return ModelRef{}, fmt.Errorf("provider %q has no default %q model", ref, taskType)
	}
	return modelRef, nil
}

func supportingModelTaskType(modelType SupportingModelType) (ModelTaskType, bool) {
	switch modelType {
	case SupportingModelThreadSummarization:
		return ModelTaskThreadSummarization, true
	default:
		return "", false
	}
}

func CurrentProviderFromRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if strings.Contains(ref, "/") {
		parsed, err := ParseModelRef(ref)
		if err != nil {
			return ""
		}
		return parsed.ProviderID
	}
	return ref
}

// ResolveSupportingModel resolves a task-specific supporting model using the
// already-resolved main model as the fallback anchor.
//
// Resolution order:
//   - explicit override in overrides[taskType], if provided
//   - the main model's provider default for taskType
//   - the resolved main model itself
func (r *ProviderRegistry) ResolveSupportingModel(main ModelRef, overrides SupportingModels, modelType SupportingModelType) (ModelRef, error) {
	taskType := string(modelType)
	if override := strings.TrimSpace(overrides[modelType]); override != "" {
		return r.ResolveModelInProvider(main.ProviderID, override, taskType)
	}

	p, err := r.Get(main.ProviderID)
	if err != nil {
		return ModelRef{}, fmt.Errorf("provider %q: %w", main.ProviderID, err)
	}
	if ref := p.DefaultModels()[taskType]; ref.ModelID != "" {
		return ref, nil
	}

	return main, nil
}

// ListModels queries all providers that are currently configured (have
// credentials available or were pinned via Add) and returns their
// models with IDs prefixed as "providerId/modelId".
func (r *ProviderRegistry) ListModels(ctx context.Context) ([]ModelInfo, error) {
	// Collect the union of pinned IDs and registered factory IDs.
	seen := make(map[string]struct{})

	r.mu.RLock()
	for id := range r.pinned {
		seen[id] = struct{}{}
	}
	r.mu.RUnlock()

	for _, id := range RegisteredIDs() {
		seen[id] = struct{}{}
	}

	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var all []ModelInfo
	for _, id := range ids {
		p, err := r.Get(id)
		if err != nil {
			// Provider not configured — skip silently.
			continue
		}
		models, err := p.ListModels(ctx)
		if err != nil {
			return nil, fmt.Errorf("list models from %s: %w", id, err)
		}
		for _, m := range models {
			m.ID = id + "/" + m.ID
			m.ProviderID = id
			all = append(all, m)
		}
	}
	return all, nil
}

// IDs returns the sorted list of provider IDs that are currently available
// (either pinned via Add, or have credentials available).
func (r *ProviderRegistry) IDs() []string {
	seen := make(map[string]struct{})

	r.mu.RLock()
	for id := range r.pinned {
		seen[id] = struct{}{}
	}
	r.mu.RUnlock()

	for _, id := range RegisteredIDs() {
		if len(r.configForProvider(id)) > 0 {
			seen[id] = struct{}{}
		}
	}

	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// configForProvider builds a Config for the given provider ID.
//
// Credential resolution order:
//  1. OAuth credentials in the Manager for this provider ID (sets "auth_token", returns early).
//  2. Special alias: "codex" OAuth credentials are mapped to the "openai" provider
//     with the ChatGPT Codex base URL (sets "api_key", returns early).
//  3. Env var credentials from models.dev (Manager first, then OS env).
//  4. CODEX_TOKEN OS env var overrides OpenAI credentials with the Codex backend.
//
// Returns an empty Config if no credentials are found.
func (r *ProviderRegistry) configForProvider(id string) Config {
	cfg := Config{}

	// Check for OAuth credentials from the Manager first — they take priority.
	if r.credMgr != nil {
		for _, c := range r.credMgr.ForProvider(id) {
			if c.AuthType == "oauth" && c.Value != "" {
				cfg["auth_token"] = c.Value
				return cfg
			}
		}
	}

	info := modelsdev.LookupProvider(id)
	if info == nil || len(info.EnvVars) == 0 {
		return cfg // no models.dev entry — provider unknown or has no env vars
	}

	// For each declared env var, check Manager first then OS env.
	// The first non-empty value becomes "api_key" for factories that call cfg.APIKey().
	apiKeySet := false
	for _, envName := range info.EnvVars {
		var val string
		if r.credMgr != nil {
			if c := r.credMgr.Get(envName); c != nil && c.Value != "" {
				val = c.Value
			}
		}
		if val == "" {
			val = os.Getenv(envName)
		}
		if val == "" {
			continue
		}
		cfg[envName] = val
		if !apiKeySet {
			cfg["api_key"] = val
			apiKeySet = true
		}
	}

	return cfg
}

// configEqual reports whether two Configs have identical key-value pairs.
func configEqual(a, b Config) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
