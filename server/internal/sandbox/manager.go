package sandbox

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"time"
)

// PlatformDefaultProvider returns the default sandbox provider for the current OS.
// On macOS (darwin), the default is "vz" (Virtualization.framework).
// On all other platforms, the default is "docker".
func PlatformDefaultProvider() string {
	if runtime.GOOS == "darwin" {
		return "vz"
	}
	return "docker"
}

// Manager manages multiple sandbox providers and routes requests to the appropriate one.
type Manager struct {
	providers       map[string]Provider
	defaultProvider string // Default provider name
}

// NewManager creates a new sandbox provider manager.
// The default provider is determined by the platform: "vz" on macOS, "docker" elsewhere.
func NewManager() *Manager {
	return &Manager{
		providers:       make(map[string]Provider),
		defaultProvider: PlatformDefaultProvider(),
	}
}

// RegisterProvider registers a provider with the given name.
func (m *Manager) RegisterProvider(name string, provider Provider) {
	m.providers[name] = provider
}

// SetDefault sets the default provider name.
func (m *Manager) SetDefault(name string) {
	m.defaultProvider = name
}

// DefaultProviderName returns the name of the current default provider.
func (m *Manager) DefaultProviderName() string {
	return m.defaultProvider
}

// EnsureDefaultAvailable checks that the default provider is registered.
// If not, it falls back to any registered provider. Returns false if no providers are registered.
func (m *Manager) EnsureDefaultAvailable() bool {
	if _, ok := m.providers[m.defaultProvider]; ok {
		return true
	}
	// Fall back to any registered provider
	for name := range m.providers {
		m.defaultProvider = name
		return true
	}
	return false
}

// GetProvider returns the provider with the given name.
func (m *Manager) GetProvider(name string) (Provider, error) {
	if name == "" {
		name = m.defaultProvider
	}

	provider, ok := m.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %q not found", name)
	}

	return provider, nil
}

// GetDefault returns the default provider.
func (m *Manager) GetDefault() Provider {
	provider, _ := m.GetProvider(m.defaultProvider)
	return provider
}

// ListProviders returns the names of all registered providers.
func (m *Manager) ListProviders() []string {
	var names []string
	for name := range m.providers {
		names = append(names, name)
	}
	return names
}

// GetProviderStatus returns the status of a specific provider.
// If the provider implements StatusProvider, its Status() is called.
// Otherwise, a default "ready" status is returned.
func (m *Manager) GetProviderStatus(name string) (ProviderStatus, bool) {
	provider, ok := m.providers[name]
	if !ok {
		return ProviderStatus{}, false
	}

	if sp, ok := provider.(StatusProvider); ok {
		return sp.Status(), true
	}

	return ProviderStatus{
		Available: true,
		State:     "ready",
	}, true
}

// ListProviderStatuses returns the status of all registered providers.
func (m *Manager) ListProviderStatuses() map[string]ProviderStatus {
	statuses := make(map[string]ProviderStatus, len(m.providers))
	for name := range m.providers {
		status, _ := m.GetProviderStatus(name)
		statuses[name] = status
	}
	return statuses
}

// Shutdown gracefully shuts down all providers that support cleanup.
// Providers implementing a Close() method will have it called.
func (m *Manager) Shutdown() {
	for name, provider := range m.providers {
		// Check if provider implements Close() method using type assertion
		if closer, ok := provider.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				// Log error but continue shutting down other providers
				_ = err // Logging happens inside provider's Close()
			}
		}
		_ = name // Keep for potential logging
	}
}

// ProviderProxy implements the Provider interface and routes to the appropriate provider.
// This is used when we need a single Provider interface but want to support multiple backends.
type ProviderProxy struct {
	manager        *Manager
	providerGetter func(ctx context.Context, sessionID string) (string, error)
}

// NewProviderProxy creates a new provider proxy that uses providerGetter to determine
// which provider to use for each session.
func NewProviderProxy(manager *Manager, providerGetter func(ctx context.Context, sessionID string) (string, error)) *ProviderProxy {
	return &ProviderProxy{
		manager:        manager,
		providerGetter: providerGetter,
	}
}

// ListProviders returns the names of all available providers.
func (p *ProviderProxy) ListProviders() []string {
	return p.manager.ListProviders()
}

// ImageExists checks if the image exists in the default provider.
func (p *ProviderProxy) ImageExists(ctx context.Context) bool {
	provider := p.manager.GetDefault()
	if provider == nil {
		return false
	}
	return provider.ImageExists(ctx)
}

// Image returns the image name from the default provider.
func (p *ProviderProxy) Image() string {
	provider := p.manager.GetDefault()
	if provider == nil {
		return ""
	}
	return provider.Image()
}

// DefaultProvider returns the current default provider.
func (p *ProviderProxy) DefaultProvider() Provider {
	return p.manager.GetDefault()
}

// Create creates a sandbox using the provider determined by providerGetter.
func (p *ProviderProxy) Create(ctx context.Context, sessionID string, opts CreateOptions) (*Sandbox, error) {
	providerName, err := p.providerGetter(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider for session: %w", err)
	}

	provider, err := p.manager.GetProvider(providerName)
	if err != nil {
		return nil, err
	}

	return provider.Create(ctx, sessionID, opts)
}

// Start starts a sandbox using the provider determined by providerGetter.
func (p *ProviderProxy) Start(ctx context.Context, sessionID string) error {
	providerName, err := p.providerGetter(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get provider for session: %w", err)
	}

	provider, err := p.manager.GetProvider(providerName)
	if err != nil {
		return err
	}

	return provider.Start(ctx, sessionID)
}

// Stop stops a sandbox using the provider determined by providerGetter.
func (p *ProviderProxy) Stop(ctx context.Context, sessionID string, timeout time.Duration) error {
	providerName, err := p.providerGetter(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get provider for session: %w", err)
	}

	provider, err := p.manager.GetProvider(providerName)
	if err != nil {
		return err
	}

	return provider.Stop(ctx, sessionID, timeout)
}

// Remove removes a sandbox using the provider determined by providerGetter.
func (p *ProviderProxy) Remove(ctx context.Context, sessionID string, opts ...RemoveOption) error {
	providerName, err := p.providerGetter(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to get provider for session: %w", err)
	}

	provider, err := p.manager.GetProvider(providerName)
	if err != nil {
		return err
	}

	return provider.Remove(ctx, sessionID, opts...)
}

// Get gets a sandbox using the provider determined by providerGetter.
func (p *ProviderProxy) Get(ctx context.Context, sessionID string) (*Sandbox, error) {
	providerName, err := p.providerGetter(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider for session: %w", err)
	}

	provider, err := p.manager.GetProvider(providerName)
	if err != nil {
		return nil, err
	}

	return provider.Get(ctx, sessionID)
}

// GetSecret gets the secret using the provider determined by providerGetter.
func (p *ProviderProxy) GetSecret(ctx context.Context, sessionID string) (string, error) {
	providerName, err := p.providerGetter(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("failed to get provider for session: %w", err)
	}

	provider, err := p.manager.GetProvider(providerName)
	if err != nil {
		return "", err
	}

	return provider.GetSecret(ctx, sessionID)
}

// List lists all sandboxes across all providers.
func (p *ProviderProxy) List(ctx context.Context) ([]*Sandbox, error) {
	var allSandboxes []*Sandbox

	for _, provider := range p.manager.providers {
		sandboxes, err := provider.List(ctx)
		if err != nil {
			continue // Skip providers that error
		}
		allSandboxes = append(allSandboxes, sandboxes...)
	}

	return allSandboxes, nil
}

// Exec executes a command using the provider determined by providerGetter.
func (p *ProviderProxy) Exec(ctx context.Context, sessionID string, cmd []string, opts ExecOptions) (*ExecResult, error) {
	providerName, err := p.providerGetter(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider for session: %w", err)
	}

	provider, err := p.manager.GetProvider(providerName)
	if err != nil {
		return nil, err
	}

	return provider.Exec(ctx, sessionID, cmd, opts)
}

// Attach attaches to a sandbox using the provider determined by providerGetter.
func (p *ProviderProxy) Attach(ctx context.Context, sessionID string, opts AttachOptions) (PTY, error) {
	providerName, err := p.providerGetter(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider for session: %w", err)
	}

	provider, err := p.manager.GetProvider(providerName)
	if err != nil {
		return nil, err
	}

	return provider.Attach(ctx, sessionID, opts)
}

// ExecStream executes a streaming command using the provider determined by providerGetter.
func (p *ProviderProxy) ExecStream(ctx context.Context, sessionID string, cmd []string, opts ExecStreamOptions) (Stream, error) {
	providerName, err := p.providerGetter(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider for session: %w", err)
	}

	provider, err := p.manager.GetProvider(providerName)
	if err != nil {
		return nil, err
	}

	return provider.ExecStream(ctx, sessionID, cmd, opts)
}

// HTTPClient returns an HTTP client using the provider determined by providerGetter.
func (p *ProviderProxy) HTTPClient(ctx context.Context, sessionID string) (*http.Client, error) {
	providerName, err := p.providerGetter(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider for session: %w", err)
	}

	provider, err := p.manager.GetProvider(providerName)
	if err != nil {
		return nil, err
	}

	return provider.HTTPClient(ctx, sessionID)
}

// Watch watches all providers and merges events.
func (p *ProviderProxy) Watch(ctx context.Context) (<-chan StateEvent, error) {
	merged := make(chan StateEvent, 100)

	// Start watching all providers
	var channels []<-chan StateEvent
	for _, provider := range p.manager.providers {
		ch, err := provider.Watch(ctx)
		if err != nil {
			continue // Skip providers that can't be watched
		}
		channels = append(channels, ch)
	}

	// Merge all channels
	go func() {
		defer close(merged)

		// Use a WaitGroup to wait for all goroutines
		cases := make([]<-chan StateEvent, len(channels))
		copy(cases, channels)

		for _, ch := range cases {
			go func(c <-chan StateEvent) {
				for event := range c {
					select {
					case merged <- event:
					case <-ctx.Done():
						return
					}
				}
			}(ch)
		}

		// Wait for context cancellation
		<-ctx.Done()
	}()

	return merged, nil
}

// Reconcile delegates to all providers.
func (p *ProviderProxy) Reconcile(ctx context.Context) error {
	for name, provider := range p.manager.providers {
		if err := provider.Reconcile(ctx); err != nil {
			log.Printf("Warning: Failed to reconcile provider %s: %v", name, err)
		}
	}
	return nil
}

// RemoveProject delegates to all providers.
func (p *ProviderProxy) RemoveProject(ctx context.Context, projectID string) error {
	for name, provider := range p.manager.providers {
		if err := provider.RemoveProject(ctx, projectID); err != nil {
			log.Printf("Warning: Failed to remove project resources for provider %s: %v", name, err)
		}
	}
	return nil
}
