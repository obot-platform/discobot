package vm

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"

	dockerclient "github.com/docker/docker/client"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/sandbox"
	"github.com/obot-platform/discobot/server/internal/sandbox/docker"
)

// SessionProjectResolver looks up the project ID for a session from the database.
// Returns the project ID or an error if the session doesn't exist.
type SessionProjectResolver func(ctx context.Context, sessionID string) (projectID string, err error)

// SystemManager interface for tracking startup tasks.
type SystemManager interface {
	RegisterTask(id, name string)
	StartTask(id string)
	UpdateTaskProgress(id string, progress int, currentOperation string)
	UpdateTaskBytes(id string, bytesDownloaded, totalBytes int64)
	CompleteTask(id string)
	FailTask(id string, err error)
}

// Provider is a generic VM+Docker hybrid provider that:
//   - Uses a ProjectVMManager to create project-level VMs (one VM per project)
//   - Uses Docker provider to create containers inside those VMs (one container per session)
//   - Communicates with Docker daemon inside VM via the dialer provided by ProjectVM
//
// This provides the isolation benefits of VMs at the project level while allowing
// multiple sessions to share a VM, with session-level isolation via containers.
type Provider struct {
	cfg *config.Config

	// vmManager manages project-level VMs (abstraction).
	vmManager ProjectVMManager

	// providerName is reported through optional provider capabilities.
	providerName string

	// dockerProviders maps projectID -> Docker provider (with VM transport).
	dockerProviders   map[string]*docker.Provider
	dockerProvidersMu sync.RWMutex

	// sessionProjectResolver looks up session -> project mapping from the database.
	sessionProjectResolver SessionProjectResolver

	// providerResourceResolver returns the effective VM resources for a project.
	providerResourceResolver ProviderResourceResolver

	// hostDockerClient connects to the configured host Docker daemon for image
	// transfer to VMs. On Windows this can proxy through a user-managed WSL
	// distro when DISCOBOT_DOCKER_WSL_DISTRO is set.
	hostDockerClient     *dockerclient.Client
	hostDockerClientOnce sync.Once
	hostDockerClientErr  error

	// systemManager tracks startup tasks and system status (optional).
	systemManager SystemManager

	// postVMSetup is called after a VM's Docker provider is created and images are loaded.
	// Used by VZ to start the proxy container.
	postVMSetup func(ctx context.Context, projectID string, dockerProv *docker.Provider) error

	// idleTimeout is how long a project VM with no running sandboxes can remain
	// idle before the shared idle-runtime monitor shuts it down.
	idleTimeout time.Duration

	httpClients *sandbox.HTTPClientCache

	// idleMonitor shuts down project VMs once they have had no running
	// sandboxes for the configured idle period.
	idleMonitor *sandbox.IdleRuntimeMonitor
}

func (p *Provider) IsLocal() bool {
	return false
}

// Option configures a Provider.
type Option func(*Provider)

// WithPostVMSetup sets a callback called after a VM's Docker provider is created
// and images are loaded. VZ uses this to start the VSOCK proxy container.
func WithPostVMSetup(fn func(ctx context.Context, projectID string, dockerProv *docker.Provider) error) Option {
	return func(p *Provider) {
		p.postVMSetup = fn
	}
}

// WithIdleTimeout sets how long a VM with no running sandboxes can be idle
// before being automatically shut down. Zero (default) means never shut down.
func WithIdleTimeout(d time.Duration) Option {
	return func(p *Provider) {
		p.idleTimeout = d
	}
}

// WithProviderResourceResolver sets the resolver used for effective provider VM resources.
func WithProviderResourceResolver(resolver ProviderResourceResolver) Option {
	return func(p *Provider) {
		p.providerResourceResolver = resolver
	}
}

// WithProviderName sets the provider name reported by optional capabilities.
func WithProviderName(name string) Option {
	return func(p *Provider) {
		p.providerName = name
	}
}

// NewProvider creates a new VM+Docker hybrid provider.
// The vmManager provides VMs with Docker daemons; the provider creates Docker
// containers inside those VMs for session isolation.
func NewProvider(cfg *config.Config, vmManager ProjectVMManager, resolver SessionProjectResolver, systemManager SystemManager, opts ...Option) *Provider {
	p := &Provider{
		cfg:                    cfg,
		vmManager:              vmManager,
		providerName:           "vm",
		dockerProviders:        make(map[string]*docker.Provider),
		httpClients:            sandbox.NewHTTPClientCache(),
		sessionProjectResolver: resolver,
		systemManager:          systemManager,
	}

	for _, opt := range opts {
		opt(p)
	}

	// Pre-warm the "local" project VM and start idle cleanup after the manager is ready
	go func() {
		<-vmManager.Ready()
		if vmManager.Err() != nil {
			return
		}
		if _, err := p.getOrCreateDockerProvider(context.Background(), "local"); err != nil {
			log.Printf("failed to warm VM for local project: %v", err)
		}

		// Start idle VM cleanup after ready
		if p.idleTimeout > 0 {
			p.idleMonitor = sandbox.NewIdleRuntimeMonitor(p, p.providerName, p.idleTimeout, time.Minute)
			p.idleMonitor.Start(context.Background())
		}
	}()

	return p
}

func (p *Provider) Definition() sandbox.ProviderDefinition {
	name := "VM"
	icon := ""
	switch p.providerName {
	case "vz":
		name = "Apple VZ"
		icon = "simple:apple"
	case "hcs":
		name = "Windows HCS"
		icon = "simple:windows"
	}
	return sandbox.ProviderDefinition{
		Name:        name,
		Icon:        icon,
		Description: name + " sandbox driver",
		ConfigFields: []sandbox.ProviderConfigField{
			{Key: "dataDir", Label: "Data directory", Type: "text", Placeholder: "~/.local/state/discobot/vz", Description: "Directory for Apple VZ VM state.", Advanced: true},
			{Key: "imageRef", Label: "Image reference", Type: "text", Placeholder: "ghcr.io/...", Description: "Optional registry image for VM guest assets.", Advanced: true},
			{Key: "homeDir", Label: "Shared home directory", Type: "text", Placeholder: "/Users/me", Description: "Host directory shared into VMs.", Advanced: true},
			{Key: "cpuCount", Label: "CPU count", Type: "number", Placeholder: "0", Description: "CPUs per VM. Leave empty or 0 for the platform default.", Advanced: true},
			{Key: "memoryMB", Label: "Memory", Type: "number", Placeholder: "8192", Description: "Memory per VM in MB.", Advanced: true},
			{Key: "dataDiskGB", Label: "Data disk", Type: "number", Placeholder: "100", Description: "Data disk size per VM in GB.", Advanced: true},
		},
	}
}

// ImageExists checks if the Docker image exists.
// Checks VM Docker daemons first (if any VMs are running), then falls back to host Docker.
func (p *Provider) ImageExists(ctx context.Context) bool {
	image := p.cfg.SandboxImage

	// First, check if image exists in any running VM's Docker daemon
	p.dockerProvidersMu.RLock()
	for _, dp := range p.dockerProviders {
		if dp.ImageExists(ctx) {
			p.dockerProvidersMu.RUnlock()
			return true
		}
	}
	p.dockerProvidersMu.RUnlock()

	// Fall back to host Docker daemon for verification
	client, err := p.getHostDockerClient()
	if err != nil {
		return false
	}

	_, err = client.ImageInspect(ctx, image)
	return err == nil
}

// Image returns the sandbox image name.
func (p *Provider) Image() string {
	return p.cfg.SandboxImage
}

// CurrentImageID returns the immutable image ID for the configured sandbox image.
func (p *Provider) CurrentImageID(ctx context.Context) (string, error) {
	providers, err := p.ensureDockerProviders(ctx)
	if err != nil {
		return "", err
	}
	for _, dockerProv := range providers {
		imageID, err := dockerProv.CurrentImageID(ctx)
		if err == nil {
			return imageID, nil
		}
		log.Printf("Warning: Failed to resolve current sandbox image ID from project VM: %v", err)
	}

	hostClient, err := p.getHostDockerClient()
	if err != nil {
		return "", fmt.Errorf("failed to get host docker client: %w", err)
	}
	inspect, err := hostClient.ImageInspect(ctx, p.cfg.SandboxImage)
	if err != nil {
		return "", fmt.Errorf("failed to inspect current sandbox image %s on host docker: %w", p.cfg.SandboxImage, err)
	}
	return inspect.ID, nil
}

// CleanupUnusedImages delegates labeled sandbox image cleanup to each project VM Docker daemon.
func (p *Provider) CleanupUnusedImages(ctx context.Context) error {
	providers, err := p.ensureDockerProviders(ctx)
	if err != nil {
		return err
	}

	var firstErr error
	for _, dockerProv := range providers {
		if err := dockerProv.CleanupUnusedImages(ctx); err != nil {
			log.Printf("Warning: Failed to clean up sandbox images in project VM: %v", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// GetProviderResourceInfo reports the effective VM resources for a project.
func (p *Provider) GetProviderResourceInfo(ctx context.Context, projectID string) (*sandbox.ProviderResourceInfo, error) {
	var (
		resources ProviderResourceConfig
		err       error
	)

	if resourceManager, ok := p.vmManager.(ProviderResourceManager); ok {
		resources, err = resourceManager.ProviderResources(ctx, projectID)
	} else {
		if p.providerResourceResolver == nil {
			return nil, fmt.Errorf("provider resources not supported")
		}
		resources, err = p.providerResourceResolver(ctx, projectID)
	}
	if err != nil {
		return nil, err
	}

	return &sandbox.ProviderResourceInfo{
		Provider:   p.providerName,
		CPUCount:   resources.CPUCount,
		MemoryMB:   resources.MemoryMB,
		DataDiskGB: resources.DataDiskGB,
	}, nil
}

// ApplyProviderResourceUpdate applies provider VM resource changes.
func (p *Provider) ApplyProviderResourceUpdate(ctx context.Context, projectID string, req sandbox.UpdateProviderResourcesRequest) error {
	if err := p.vmManager.RemoveVM(projectID); err != nil {
		return err
	}

	p.dockerProvidersMu.Lock()
	delete(p.dockerProviders, projectID)
	p.dockerProvidersMu.Unlock()

	if req.DataDiskGB != nil {
		diskResizer, ok := p.vmManager.(DiskResizer)
		if !ok {
			return fmt.Errorf("data disk resize not supported")
		}
		if err := diskResizer.ResizeDataDisk(ctx, projectID, *req.DataDiskGB); err != nil {
			return err
		}
	}

	return nil
}

// GetProjectInspectionInfo reports inspection-container availability for a project VM.
func (p *Provider) GetProjectInspectionInfo(_ context.Context, _ string) (*sandbox.ProjectInspectionInfo, error) {
	return &sandbox.ProjectInspectionInfo{
		Provider:      p.providerName,
		Available:     true,
		ContainerName: "discobot-host-inspect",
		Scope:         "project_vm",
	}, nil
}

// AttachProjectInspection attaches to the inspection container shell in a project VM.
func (p *Provider) AttachProjectInspection(ctx context.Context, projectID string, opts sandbox.AttachOptions) (sandbox.PTY, error) {
	dockerProv, err := p.getOrCreateDockerProvider(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to get docker provider: %w", err)
	}
	return dockerProv.AttachProjectInspection(ctx, projectID, opts)
}

// Create creates a sandbox in the project's VM.
func (p *Provider) Create(ctx context.Context, state []byte, sessionID string, opts sandbox.CreateOptions) (*sandbox.Sandbox, []byte, error) {
	projectID, err := p.sessionProjectResolver(ctx, sessionID)
	if err != nil {
		return nil, state, fmt.Errorf("failed to resolve project for session %s: %w", sessionID, err)
	}

	dockerProv, err := p.getOrCreateDockerProvider(ctx, projectID)
	if err != nil {
		return nil, state, fmt.Errorf("failed to get docker provider: %w", err)
	}

	return dockerProv.Create(ctx, state, sessionID, opts)
}

// Start starts a sandbox.
func (p *Provider) Start(ctx context.Context, state []byte, sessionID string) ([]byte, error) {
	_, dockerProv, err := p.getDockerProviderForSession(ctx, sessionID)
	if err != nil {
		return state, err
	}
	return dockerProv.Start(ctx, state, sessionID)
}

// Stop stops a sandbox.
func (p *Provider) Stop(ctx context.Context, state []byte, sessionID string, timeout time.Duration) ([]byte, error) {
	p.httpClients.Remove(sessionID)
	_, dockerProv, err := p.getDockerProviderForSession(ctx, sessionID)
	if err != nil {
		return state, err
	}
	return dockerProv.Stop(ctx, state, sessionID, timeout)
}

// Remove removes a sandbox.
func (p *Provider) Remove(ctx context.Context, state []byte, sessionID string, opts ...sandbox.RemoveOption) ([]byte, error) {
	p.httpClients.Remove(sessionID)
	_, dockerProv, err := p.getDockerProviderForSession(ctx, sessionID)
	if err != nil {
		return state, err
	}
	newState, err := dockerProv.Remove(ctx, state, sessionID, opts...)
	if err != nil {
		return state, err
	}

	return newState, nil
}

// Get returns sandbox info.
func (p *Provider) Get(ctx context.Context, state []byte, sessionID string) (*sandbox.Sandbox, error) {
	_, dockerProv, err := p.getDockerProviderForSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return dockerProv.Get(ctx, state, sessionID)
}

// GetSecret returns the shared secret for a sandbox.
func (p *Provider) GetSecret(ctx context.Context, state []byte, sessionID string) (string, error) {
	_, dockerProv, err := p.getDockerProviderForSession(ctx, sessionID)
	if err != nil {
		return "", err
	}
	return dockerProv.GetSecret(ctx, state, sessionID)
}

func (p *Provider) ensureDockerProviders(ctx context.Context) ([]*docker.Provider, error) {
	if err := p.WaitForReady(ctx); err != nil {
		return nil, err
	}

	projectIDSet := make(map[string]struct{})

	p.dockerProvidersMu.RLock()
	for projectID := range p.dockerProviders {
		projectIDSet[projectID] = struct{}{}
	}
	p.dockerProvidersMu.RUnlock()

	for _, projectID := range p.vmManager.ListProjectIDs() {
		projectIDSet[projectID] = struct{}{}
	}

	projectIDs := make([]string, 0, len(projectIDSet))
	for projectID := range projectIDSet {
		projectIDs = append(projectIDs, projectID)
	}
	sort.Strings(projectIDs)

	providers := make([]*docker.Provider, 0, len(projectIDs))
	for _, projectID := range projectIDs {
		dockerProv, err := p.getOrCreateDockerProvider(ctx, projectID)
		if err != nil {
			log.Printf("Warning: Failed to prepare Docker provider for project %s: %v", projectID, err)
			continue
		}
		providers = append(providers, dockerProv)
	}
	return providers, nil
}

// List returns all sandboxes across all project VMs.
func (p *Provider) List(ctx context.Context) ([]*sandbox.Sandbox, error) {
	providers, err := p.ensureDockerProviders(ctx)
	if err != nil {
		return nil, err
	}

	var allSandboxes []*sandbox.Sandbox
	for _, dockerProv := range providers {
		sandboxes, err := dockerProv.List(ctx)
		if err != nil {
			log.Printf("Warning: Failed to list sandboxes from a VM Docker provider: %v", err)
			continue
		}
		allSandboxes = append(allSandboxes, sandboxes...)
	}
	return allSandboxes, nil
}

// AcquireHTTPClient returns a leased HTTP client that connects to the sandbox's published port
// via the VM's port dialer.
func (p *Provider) AcquireHTTPClient(ctx context.Context, state []byte, sessionID string) (*sandbox.HTTPClientLease, error) {
	projectID, dockerProv, err := p.getDockerProviderForSession(ctx, sessionID)
	if err != nil {
		p.httpClients.Remove(sessionID)
		return nil, err
	}

	pvm, ok := p.GetVMForProject(projectID)
	if !ok {
		p.httpClients.Remove(sessionID)
		return nil, fmt.Errorf("no VM found for project %q", projectID)
	}

	// Get the sandbox to find its published port
	sb, err := dockerProv.Get(ctx, state, sessionID)
	if err != nil {
		p.httpClients.Remove(sessionID)
		return nil, fmt.Errorf("failed to get sandbox info: %w", err)
	}

	// Find the host port for the container port
	var hostPort uint32
	for _, port := range sb.Ports {
		if port.ContainerPort == 3002 {
			hostPort = uint32(port.HostPort)
			break
		}
	}
	if hostPort == 0 {
		p.httpClients.Remove(sessionID)
		return nil, fmt.Errorf("no published port found for sandbox %s", sessionID)
	}

	target := fmt.Sprintf("%p:%d", pvm, hostPort)
	return p.httpClients.Acquire(sessionID, target, func() (*http.Client, error) {
		return &http.Client{
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   100,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
				DialContext:           pvm.PortDialer(hostPort),
			},
			Timeout: 60 * time.Second,
		}, nil
	})
}

// Watch merges state events from all Docker providers.
func (p *Provider) Watch(ctx context.Context) (<-chan sandbox.StateEvent, error) {
	p.dockerProvidersMu.RLock()
	providers := make([]*docker.Provider, 0, len(p.dockerProviders))
	for _, prov := range p.dockerProviders {
		providers = append(providers, prov)
	}
	p.dockerProvidersMu.RUnlock()

	merged := make(chan sandbox.StateEvent, 32)

	var wg sync.WaitGroup
	for _, prov := range providers {
		ch, err := prov.Watch(ctx)
		if err != nil {
			continue
		}
		wg.Add(1)
		go func(ch <-chan sandbox.StateEvent) {
			defer wg.Done()
			for event := range ch {
				select {
				case merged <- event:
				case <-ctx.Done():
					return
				}
			}
		}(ch)
	}

	// Close merged channel when all watchers are done
	go func() {
		wg.Wait()
		close(merged)
	}()

	return merged, nil
}

// Close shuts down the provider and all project VMs.
func (p *Provider) Close() error {
	log.Printf("Shutting down VM+Docker provider")

	if p.idleMonitor != nil {
		_ = p.idleMonitor.Stop(context.Background())
	}

	// Close all Docker providers
	p.dockerProvidersMu.Lock()
	for projectID, dockerProv := range p.dockerProviders {
		if err := dockerProv.Close(); err != nil {
			log.Printf("Warning: failed to close Docker provider for project %s: %v", projectID, err)
		}
		delete(p.dockerProviders, projectID)
	}
	p.dockerProvidersMu.Unlock()

	p.vmManager.Shutdown()

	// Close host Docker client if initialized
	if p.hostDockerClient != nil {
		_ = p.hostDockerClient.Close()
	}

	return nil
}

// Reconcile delegates to all per-project Docker providers to reconcile.
func (p *Provider) Reconcile(ctx context.Context) error {
	providers, err := p.ensureDockerProviders(ctx)
	if err != nil {
		return err
	}

	for _, dockerProv := range providers {
		if err := dockerProv.Reconcile(ctx); err != nil {
			log.Printf("Warning: Failed to reconcile VM Docker provider: %v", err)
		}
	}
	return nil
}

// RemoveProject delegates to the project's Docker provider to clean up resources.
func (p *Provider) RemoveProject(ctx context.Context, projectID string) error {
	p.dockerProvidersMu.RLock()
	dockerProv, ok := p.dockerProviders[projectID]
	p.dockerProvidersMu.RUnlock()

	if ok {
		return dockerProv.RemoveProject(ctx, projectID)
	}
	return nil
}

// ClearCache delegates to the project's Docker provider.
func (p *Provider) ClearCache(ctx context.Context, projectID string) error {
	p.dockerProvidersMu.RLock()
	dockerProv, ok := p.dockerProviders[projectID]
	p.dockerProvidersMu.RUnlock()

	if ok {
		return dockerProv.ClearCache(ctx, projectID)
	}
	return nil
}

// Status returns the current status of the VM provider.
// Implements sandbox.StatusProvider.
func (p *Provider) Status() sandbox.ProviderStatus {
	// Delegate to the VM manager if it implements StatusReporter
	if reporter, ok := p.vmManager.(StatusReporter); ok {
		return reporter.Status()
	}

	// Basic status based on Ready/Err
	select {
	case <-p.vmManager.Ready():
		if err := p.vmManager.Err(); err != nil {
			return sandbox.ProviderStatus{
				Available: false,
				State:     "failed",
				Message:   err.Error(),
			}
		}
		return sandbox.ProviderStatus{
			Available: true,
			State:     "ready",
		}
	default:
		return sandbox.ProviderStatus{
			Available: true,
			State:     "initializing",
			Message:   "VM manager initializing",
		}
	}
}

// GetVMForProject returns the project VM if it exists.
// This is used by the debug Docker proxy to get the VM dialer.
func (p *Provider) GetVMForProject(projectID string) (ProjectVM, bool) {
	select {
	case <-p.vmManager.Ready():
		if p.vmManager.Err() != nil {
			return nil, false
		}
	default:
		return nil, false
	}
	return p.vmManager.GetVM(projectID)
}

// DockerTransport returns an http.RoundTripper that communicates with the Docker
// daemon inside the VM for the given project. Implements sandbox.DockerProxyProvider.
func (p *Provider) DockerTransport(projectID string) (http.RoundTripper, error) {
	projectVM, ok := p.GetVMForProject(projectID)
	if !ok {
		return nil, fmt.Errorf("no VM found for project %q", projectID)
	}

	return &http.Transport{
		DialContext: projectVM.DockerDialer(),
	}, nil
}

// IsReady returns true if the provider is ready to create VMs.
func (p *Provider) IsReady() bool {
	select {
	case <-p.vmManager.Ready():
		return p.vmManager.Err() == nil
	default:
		return false
	}
}

// WaitForReady blocks until the VM provider is ready.
// Returns an error if initialization fails or the context is cancelled.
func (p *Provider) WaitForReady(ctx context.Context) error {
	select {
	case <-p.vmManager.Ready():
		return p.vmManager.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// getHostDockerClient returns a Docker client connected to the configured host
// Docker daemon. Used to export locally-built images for transfer into VMs.
func (p *Provider) getHostDockerClient() (*dockerclient.Client, error) {
	p.hostDockerClientOnce.Do(func() {
		cli, err := docker.NewAPIClient(p.cfg)
		if err != nil {
			p.hostDockerClientErr = fmt.Errorf("failed to create host docker client: %w", err)
			return
		}
		p.hostDockerClient = cli
	})
	return p.hostDockerClient, p.hostDockerClientErr
}

// ensureImageInVM loads the sandbox image from the host's Docker into the VM's Docker
// when the image is local (discobot-local/ tag) and cannot be pulled from a registry.
func (p *Provider) ensureImageInVM(ctx context.Context, dockerProv *docker.Provider) error {
	if !docker.IsLocalImage(p.cfg.SandboxImage) {
		return nil
	}

	// Get host Docker client
	hostClient, err := p.getHostDockerClient()
	if err != nil {
		return fmt.Errorf("failed to get host docker client: %w", err)
	}

	if err := docker.EnsureLocalImageLoaded(ctx, hostClient, dockerProv.Client(), p.cfg.SandboxImage, p.systemManager); err != nil {
		return fmt.Errorf("failed to load image into VM docker: %w", err)
	}
	return nil
}

type workspaceMountSourceResolver interface {
	WorkspaceMountSource(string) (string, error)
}

// getOrCreateDockerProvider gets or creates a Docker provider for the given project.
// It ensures the project VM exists (creating one if needed) and sets up a Docker
// provider connected to the VM's Docker daemon via the VM's dialer.
func (p *Provider) getOrCreateDockerProvider(ctx context.Context, projectID string) (*docker.Provider, error) {
	// Non-blocking check: fail immediately if not ready
	select {
	case <-p.vmManager.Ready():
		if err := p.vmManager.Err(); err != nil {
			return nil, fmt.Errorf("VM provider not ready: %w", err)
		}
	default:
		return nil, fmt.Errorf("VM provider not ready, still initializing")
	}

	// Get or create the project VM
	pvm, err := p.vmManager.GetOrCreateVM(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("failed to get/create project VM: %w", err)
	}

	p.dockerProvidersMu.RLock()
	if prov, exists := p.dockerProviders[projectID]; exists {
		p.dockerProvidersMu.RUnlock()
		return prov, nil
	}
	p.dockerProvidersMu.RUnlock()

	p.dockerProvidersMu.Lock()
	defer p.dockerProvidersMu.Unlock()

	// Double-check after acquiring write lock
	if prov, exists := p.dockerProviders[projectID]; exists {
		return prov, nil
	}

	log.Printf("Creating Docker provider for project VM: %s", projectID)

	// Create Docker provider with VM transport.
	// The provider kicks off image pull in the background on creation.
	opts := []docker.Option{
		docker.WithVsockDialer(pvm.DockerDialer()),
	}
	if resolver, ok := pvm.(workspaceMountSourceResolver); ok {
		opts = append(opts, docker.WithWorkspaceMountSourceResolver(resolver.WorkspaceMountSource))
	}
	if p.systemManager != nil {
		opts = append(opts, docker.WithSystemManager(p.systemManager))
	}
	dockerProv, err := docker.NewProvider(
		p.cfg,
		docker.SessionProjectResolver(p.sessionProjectResolver),
		opts...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker provider: %w", err)
	}

	// Load sandbox image into VM if it's a local image
	if err := p.ensureImageInVM(ctx, dockerProv); err != nil {
		return nil, fmt.Errorf("failed to load sandbox image into VM: %w", err)
	}

	if err := dockerProv.EnsureInspectionContainer(ctx); err != nil {
		return nil, fmt.Errorf("failed to start inspection container in project %s: %w", projectID, err)
	}

	// Run post-VM setup hook (e.g., VZ starts VSOCK proxy container here)
	if p.postVMSetup != nil {
		if err := p.postVMSetup(ctx, projectID, dockerProv); err != nil {
			return nil, fmt.Errorf("post-VM setup failed for project %s: %w", projectID, err)
		}
	}

	p.dockerProviders[projectID] = dockerProv
	log.Printf("Docker provider created for project %s", projectID)
	return dockerProv, nil
}

// getDockerProviderForSession resolves the session's project ID from the database
// and returns the corresponding Docker provider. Returns sandbox.ErrNotFound if
// the session doesn't exist or has no running VM.
func (p *Provider) getDockerProviderForSession(ctx context.Context, sessionID string) (string, *docker.Provider, error) {
	projectID, err := p.sessionProjectResolver(ctx, sessionID)
	if err != nil {
		return "", nil, fmt.Errorf("%w: failed to resolve project for session %s: %w", sandbox.ErrNotFound, sessionID, err)
	}

	p.dockerProvidersMu.RLock()
	dockerProv, exists := p.dockerProviders[projectID]
	p.dockerProvidersMu.RUnlock()

	if !exists {
		return "", nil, fmt.Errorf("%w: no running VM for project %s (session %s)", sandbox.ErrNotFound, projectID, sessionID)
	}

	return projectID, dockerProv, nil
}

// RunningSandboxCount implements sandbox.IdleRuntimeController for project VMs.
func (p *Provider) RunningSandboxCount(_ context.Context, projectID string) (int, error) {
	p.dockerProvidersMu.RLock()
	dockerProv, exists := p.dockerProviders[projectID]
	p.dockerProvidersMu.RUnlock()

	if !exists {
		return 0, nil
	}

	sandboxes, err := dockerProv.List(context.Background())
	if err != nil {
		return 0, err
	}

	count := 0
	for _, sb := range sandboxes {
		if sb.Status == sandbox.StatusRunning {
			count++
		}
	}
	return count, nil
}

// ListRuntimeIDs implements sandbox.IdleRuntimeController for project VMs.
func (p *Provider) ListRuntimeIDs(_ context.Context) ([]string, error) {
	return p.vmManager.ListProjectIDs(), nil
}

// StopRuntime implements sandbox.IdleRuntimeController for project VMs.
func (p *Provider) StopRuntime(_ context.Context, projectID string) error {
	if err := p.vmManager.RemoveVM(projectID); err != nil {
		return err
	}

	p.dockerProvidersMu.Lock()
	if dockerProv, ok := p.dockerProviders[projectID]; ok {
		_ = dockerProv.Close()
		delete(p.dockerProviders, projectID)
	}
	p.dockerProvidersMu.Unlock()

	return nil
}
