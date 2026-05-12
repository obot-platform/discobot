//go:build windows

package wsl

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	dockerclient "github.com/docker/docker/client"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/sandbox"
	"github.com/obot-platform/discobot/server/internal/sandbox/docker"
	"github.com/obot-platform/discobot/server/internal/startup"
)

const (
	startupTaskWSLInstallID = "wsl-install"
	startupTaskWSLStartID   = "wsl-start"
)

var dockerClientClose = func(cli *dockerclient.Client) error {
	return cli.Close()
}

func (p *Provider) Definition() sandbox.ProviderDefinition {
	return sandbox.ProviderDefinition{
		Name:        "WSL2",
		Icon:        "simple:linux",
		Description: "WSL2 sandbox driver",
		ConfigFields: []sandbox.ProviderConfigField{
			{Key: "distro", Label: "Distro", Type: "text", Placeholder: "Ubuntu", Description: "WSL distro used for sandbox execution."},
			{Key: "installRoot", Label: "Install root", Type: "text", Placeholder: "C:\\Users\\me\\AppData\\Local\\Discobot\\wsl", Description: "Optional location for Discobot-managed WSL data.", Advanced: true},
		},
	}
}

// SessionProjectResolver maps session IDs to project IDs.
type SessionProjectResolver func(ctx context.Context, sessionID string) (projectID string, err error)

// Provider is the Windows WSL sandbox provider. It translates Windows paths,
// resolves bridge configuration, and delegates sandbox operations to an inner
// Docker provider once the managed WSL runtime exposes a reachable bridge.
type Provider struct {
	cfg                     *config.Config
	manager                 *Manager
	sessionProjectResolver  SessionProjectResolver
	systemManager           *startup.SystemManager
	ensureInstalled         func(context.Context, progressReporter) error
	ensureRunning           func(context.Context, progressReporter) (*RuntimeInfo, error)
	status                  func() sandbox.ProviderStatus
	newDockerProvider       func(*config.Config) (*docker.Provider, error)
	ensureLocalImageLoad    func(context.Context, *docker.Provider) error
	startDockerWatch        func(context.Context, *RuntimeInfo) (<-chan sandbox.StateEvent, error)
	probeBridgeReady        func(context.Context, *RuntimeInfo) (bool, error)
	watchBridgePollInterval time.Duration
	watchRetryDelay         time.Duration

	bootstrapMu          sync.RWMutex
	bootstrapInstallDone chan struct{}
	bootstrapCancel      context.CancelFunc
	bootstrapDone        chan struct{}

	mu             sync.RWMutex
	dockerProvider *docker.Provider
	dockerHost     string
	runtimeInfo    *RuntimeInfo
	runtimeWait    chan struct{}
	runtimeErr     error
	activeWatches  int

	hostDockerClientMu   sync.Mutex
	hostDockerClient     *dockerclient.Client
	hostDockerClientOnce sync.Once
	hostDockerClientErr  error

	// idleMonitor shuts down the managed distro after it has had no running
	// Discobot sandboxes for the configured idle period.
	idleMonitor *sandbox.IdleRuntimeMonitor
}

// NewProvider creates a new WSL-backed sandbox provider.
func NewProvider(cfg *config.Config, resolver SessionProjectResolver, systemManager *startup.SystemManager) (*Provider, error) {
	if resolver == nil {
		return nil, fmt.Errorf("sessionProjectResolver is required")
	}

	provider := &Provider{
		cfg:                    cfg,
		manager:                NewManager(cfg),
		sessionProjectResolver: resolver,
		systemManager:          systemManager,
	}
	provider.ensureInstalled = provider.manager.ensureInstalledWithProgress
	provider.ensureRunning = provider.manager.ensureRunningWithProgress
	provider.status = provider.manager.Status
	provider.newDockerProvider = provider.buildDockerProvider
	provider.ensureLocalImageLoad = provider.ensureLocalImageLoaded
	provider.startDockerWatch = provider.watchDockerEvents
	provider.probeBridgeReady = provider.probeRuntimeBridge
	if cfg.WSLIdleTimeout > 0 {
		provider.idleMonitor = sandbox.NewIdleRuntimeMonitor(provider, "wsl", cfg.WSLIdleTimeout, cfg.IdleCheckInterval)
		provider.idleMonitor.Start(context.Background())
	}
	provider.startBackgroundBootstrap()
	return provider, nil
}

// Status reports the WSL provider status.
func (p *Provider) Status() sandbox.ProviderStatus {
	statusFn := p.status
	if statusFn == nil {
		statusFn = p.manager.Status
	}
	status := statusFn()
	if status.Available && status.State == "ready" {
		p.reconcileReadyStartupTask(&RuntimeInfo{BridgeReady: true})
	}
	return status
}

func (p *Provider) reconcileReadyStartupTask(runtimeInfo *RuntimeInfo) {
	if p.systemManager == nil || runtimeInfo == nil || !runtimeInfo.BridgeReady {
		return
	}
	if task, ok := p.systemManager.GetTask(startupTaskWSLStartID); ok && task.State == startup.TaskStateCompleted {
		return
	}
	p.systemManager.RegisterTask(startupTaskWSLStartID, "Starting managed WSL distro")
	p.systemManager.StartTask(startupTaskWSLStartID)
	p.systemManager.UpdateTaskProgress(startupTaskWSLStartID, 100, "Managed WSL distro and Docker bridge are ready")
	p.systemManager.CompleteTask(startupTaskWSLStartID)
}

func (p *Provider) ensureRuntimeInfo(ctx context.Context, progress progressReporter) (*RuntimeInfo, error) {
	if err := p.waitForBackgroundInstall(ctx); err != nil {
		return nil, err
	}
	if runtimeInfo := p.cachedRuntimeInfoIfBridgeReady(ctx); runtimeInfo != nil {
		p.reconcileReadyStartupTask(runtimeInfo)
		return runtimeInfo, nil
	}
	waitCh, leader := p.beginRuntimeEnsureWait()
	if !leader {
		if err := p.waitForRuntimeEnsure(ctx, waitCh); err != nil {
			return nil, err
		}
		if runtimeInfo := p.loadRuntimeInfo(); runtimeInfo != nil {
			p.reconcileReadyStartupTask(runtimeInfo)
			return runtimeInfo, nil
		}
		if err := p.loadRuntimeErr(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("managed WSL runtime startup completed without runtime info")
	}
	runtimeInfo, err := p.ensureRunning(ctx, progress)
	if err != nil {
		p.clearRuntimeInfo()
		p.finishRuntimeEnsureWait(waitCh, err)
		return nil, err
	}
	p.storeRuntimeInfo(runtimeInfo)
	p.finishRuntimeEnsureWait(waitCh, nil)
	p.reconcileReadyStartupTask(runtimeInfo)
	return cloneRuntimeInfo(runtimeInfo), nil
}

func (p *Provider) cachedRuntimeInfoIfBridgeReady(ctx context.Context) *RuntimeInfo {
	cached := p.loadRuntimeInfo()
	if cached == nil || !cached.BridgeReady {
		return nil
	}
	ready, err := p.probeBridge(ctx, cached)
	if err == nil && ready {
		return cached
	}
	p.clearRuntimeInfo()
	return nil
}

func (p *Provider) loadRuntimeInfo() *RuntimeInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return cloneRuntimeInfo(p.runtimeInfo)
}

func (p *Provider) storeRuntimeInfo(runtimeInfo *RuntimeInfo) {
	p.mu.Lock()
	p.runtimeInfo = cloneRuntimeInfo(runtimeInfo)
	p.mu.Unlock()
}

func (p *Provider) clearRuntimeInfo() {
	p.mu.Lock()
	p.runtimeInfo = nil
	p.mu.Unlock()
}

func (p *Provider) beginRuntimeEnsureWait() (chan struct{}, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.runtimeWait != nil {
		return p.runtimeWait, false
	}
	waitCh := make(chan struct{})
	p.runtimeWait = waitCh
	p.runtimeErr = nil
	return waitCh, true
}

func (p *Provider) finishRuntimeEnsureWait(waitCh chan struct{}, err error) {
	p.mu.Lock()
	if p.runtimeWait == waitCh {
		p.runtimeWait = nil
		p.runtimeErr = err
	}
	p.mu.Unlock()
	close(waitCh)
}

func (p *Provider) waitForRuntimeEnsure(ctx context.Context, waitCh chan struct{}) error {
	select {
	case <-waitCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Provider) loadRuntimeErr() error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.runtimeErr
}

func cloneRuntimeInfo(runtimeInfo *RuntimeInfo) *RuntimeInfo {
	if runtimeInfo == nil {
		return nil
	}
	cloned := *runtimeInfo
	return &cloned
}

func (p *Provider) waitForBackgroundInstall(ctx context.Context) error {
	p.bootstrapMu.RLock()
	installDone := p.bootstrapInstallDone
	p.bootstrapMu.RUnlock()
	if installDone == nil {
		return nil
	}
	select {
	case <-installDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Provider) beginBackgroundInstallWait() chan struct{} {
	p.bootstrapMu.Lock()
	defer p.bootstrapMu.Unlock()
	if p.bootstrapInstallDone == nil {
		p.bootstrapInstallDone = make(chan struct{})
	}
	return p.bootstrapInstallDone
}

func (p *Provider) beginBackgroundBootstrap() (context.Context, chan struct{}, bool) {
	p.bootstrapMu.Lock()
	defer p.bootstrapMu.Unlock()
	if p.bootstrapDone != nil {
		return nil, nil, false
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	p.bootstrapCancel = cancel
	p.bootstrapDone = done
	return ctx, done, true
}

func (p *Provider) finishBackgroundBootstrap(done chan struct{}) {
	p.bootstrapMu.Lock()
	if p.bootstrapDone == done {
		p.bootstrapDone = nil
		p.bootstrapCancel = nil
	}
	p.bootstrapMu.Unlock()
	close(done)
}

func (p *Provider) cancelBackgroundBootstrap(ctx context.Context) error {
	p.bootstrapMu.RLock()
	cancel := p.bootstrapCancel
	done := p.bootstrapDone
	p.bootstrapMu.RUnlock()
	if cancel != nil {
		cancel()
	}
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Provider) finishBackgroundInstallWait(installDone chan struct{}) {
	p.bootstrapMu.Lock()
	shouldClose := p.bootstrapInstallDone == installDone
	if shouldClose {
		p.bootstrapInstallDone = nil
	}
	p.bootstrapMu.Unlock()
	if shouldClose {
		close(installDone)
	}
}

// ImageExists reports whether the configured sandbox image is locally available
// inside the managed WSL Docker daemon.
func (p *Provider) ImageExists(ctx context.Context) bool {
	runtimeInfo, err := p.ensureRuntimeInfo(ctx, progressReporter{})
	if err != nil || !runtimeInfo.BridgeReady {
		return false
	}

	dockerProvider, err := p.dockerProviderForRuntime(ctx, runtimeInfo)
	if err != nil {
		return false
	}
	return dockerProvider.ImageExists(ctx)
}

// Image returns the configured session sandbox image.
func (p *Provider) Image() string {
	return p.cfg.SandboxImage
}

// Create creates a new sandbox through the inner Docker provider once the WSL
// bridge is available.
func (p *Provider) Create(ctx context.Context, state []byte, sessionID string, opts sandbox.CreateOptions) (*sandbox.Sandbox, []byte, error) {
	runtimeInfo, err := p.ensureRuntimeInfo(ctx, progressReporter{})
	if err != nil {
		return nil, state, err
	}

	opts, err = p.translateCreateOptions(opts)
	if err != nil {
		return nil, state, err
	}

	dockerProvider, err := p.requireDockerProvider(ctx, runtimeInfo)
	if err != nil {
		return nil, state, err
	}
	return dockerProvider.Create(ctx, state, sessionID, opts)
}

// Start starts a sandbox through the inner Docker provider.
func (p *Provider) Start(ctx context.Context, state []byte, sessionID string) ([]byte, error) {
	runtimeInfo, err := p.ensureRuntimeInfo(ctx, progressReporter{})
	if err != nil {
		return state, err
	}

	dockerProvider, err := p.requireDockerProvider(ctx, runtimeInfo)
	if err != nil {
		return state, err
	}
	return dockerProvider.Start(ctx, state, sessionID)
}

// Stop stops a sandbox through the inner Docker provider.
func (p *Provider) Stop(ctx context.Context, state []byte, sessionID string, timeout time.Duration) ([]byte, error) {
	runtimeInfo, err := p.ensureRuntimeInfo(ctx, progressReporter{})
	if err != nil {
		return state, err
	}

	dockerProvider, err := p.requireDockerProvider(ctx, runtimeInfo)
	if err != nil {
		return state, err
	}
	return dockerProvider.Stop(ctx, state, sessionID, timeout)
}

// Remove removes a sandbox through the inner Docker provider.
func (p *Provider) Remove(ctx context.Context, state []byte, sessionID string, opts ...sandbox.RemoveOption) ([]byte, error) {
	runtimeInfo, err := p.ensureRuntimeInfo(ctx, progressReporter{})
	if err != nil {
		return state, err
	}

	dockerProvider, err := p.requireDockerProvider(ctx, runtimeInfo)
	if err != nil {
		return state, err
	}
	return dockerProvider.Remove(ctx, state, sessionID, opts...)
}

// Get returns sandbox state through the inner Docker provider.
func (p *Provider) Get(ctx context.Context, state []byte, sessionID string) (*sandbox.Sandbox, error) {
	runtimeInfo, err := p.ensureRuntimeInfo(ctx, progressReporter{})
	if err != nil {
		return nil, err
	}

	dockerProvider, err := p.requireDockerProvider(ctx, runtimeInfo)
	if err != nil {
		return nil, err
	}
	return dockerProvider.Get(ctx, state, sessionID)
}

// GetSecret returns sandbox secrets through the inner Docker provider.
func (p *Provider) GetSecret(ctx context.Context, state []byte, sessionID string) (string, error) {
	runtimeInfo, err := p.ensureRuntimeInfo(ctx, progressReporter{})
	if err != nil {
		return "", err
	}

	dockerProvider, err := p.requireDockerProvider(ctx, runtimeInfo)
	if err != nil {
		return "", err
	}
	return dockerProvider.GetSecret(ctx, state, sessionID)
}

// List lists sandboxes through the inner Docker provider.
func (p *Provider) List(ctx context.Context) ([]*sandbox.Sandbox, error) {
	runtimeInfo, err := p.ensureRuntimeInfo(ctx, progressReporter{})
	if err != nil {
		return nil, err
	}

	dockerProvider, err := p.requireDockerProvider(ctx, runtimeInfo)
	if err != nil {
		return nil, err
	}
	return dockerProvider.List(ctx)
}

// AcquireHTTPClient returns a leased sandbox HTTP client through the inner Docker provider.
func (p *Provider) AcquireHTTPClient(ctx context.Context, state []byte, sessionID string) (*sandbox.HTTPClientLease, error) {
	runtimeInfo, err := p.ensureRuntimeInfo(ctx, progressReporter{})
	if err != nil {
		return nil, err
	}

	dockerProvider, err := p.requireDockerProvider(ctx, runtimeInfo)
	if err != nil {
		return nil, err
	}
	return dockerProvider.AcquireHTTPClient(ctx, state, sessionID)
}

// Watch streams Docker sandbox events and recreates the inner subscription if
// the managed WSL runtime bridge disappears and later returns.
func (p *Provider) Watch(ctx context.Context) (<-chan sandbox.StateEvent, error) {
	releaseWatch := p.retainActiveWatch()

	runtimeInfo, watchCtx, cancel, innerCh, err := p.startManagedWatch(ctx)
	if err != nil {
		releaseWatch()
		return nil, err
	}

	eventCh := make(chan sandbox.StateEvent, 100)
	go func() {
		defer close(eventCh)
		defer releaseWatch()

		currentRuntimeInfo := runtimeInfo
		currentWatchCtx := watchCtx
		currentCancel := cancel
		currentInnerCh := innerCh

		for {
			reconnect := p.forwardManagedWatch(ctx, currentWatchCtx, eventCh, currentRuntimeInfo, currentCancel, currentInnerCh)
			currentCancel()
			if !reconnect {
				return
			}

			runtimeInfo, watchCtx, cancel, innerCh, err := p.restartManagedWatch(ctx)
			if err != nil {
				return
			}
			currentRuntimeInfo = runtimeInfo
			currentWatchCtx = watchCtx
			currentCancel = cancel
			currentInnerCh = innerCh
		}
	}()

	return eventCh, nil
}

// Reconcile delegates provider reconciliation to the inner Docker provider.
func (p *Provider) Reconcile(ctx context.Context) error {
	runtimeInfo, err := p.ensureRuntimeInfo(ctx, progressReporter{})
	if err != nil {
		return err
	}

	dockerProvider, err := p.requireDockerProvider(ctx, runtimeInfo)
	if err != nil {
		return err
	}
	return dockerProvider.Reconcile(ctx)
}

// RemoveProject delegates project cleanup to the inner Docker provider.
func (p *Provider) RemoveProject(ctx context.Context, projectID string) error {
	runtimeInfo, err := p.ensureRuntimeInfo(ctx, progressReporter{})
	if err != nil {
		return err
	}

	dockerProvider, err := p.requireDockerProvider(ctx, runtimeInfo)
	if err != nil {
		return err
	}
	return dockerProvider.RemoveProject(ctx, projectID)
}

// Close closes the inner Docker provider and stops the manager-owned runtime.
func (p *Provider) Close() error {
	if p.idleMonitor != nil {
		_ = p.idleMonitor.Stop(context.Background())
	}

	result := p.cancelBackgroundBootstrap(context.Background())

	p.mu.Lock()
	dockerProvider := p.dockerProvider
	p.dockerProvider = nil
	p.dockerHost = ""
	p.runtimeInfo = nil
	p.mu.Unlock()

	if dockerProvider != nil {
		result = errors.Join(result, dockerProvider.Close())
	}
	result = errors.Join(result, p.closeHostDockerClient())
	if p.manager != nil {
		result = errors.Join(result, p.manager.Stop(context.Background()))
	}
	return result
}

// ListRuntimeIDs implements sandbox.IdleRuntimeController for the single managed
// WSL distro runtime.
func (p *Provider) ListRuntimeIDs(ctx context.Context) ([]string, error) {
	distro, found, err := p.manager.probeDistro(ctx)
	if err != nil {
		return nil, err
	}
	if !found || !strings.EqualFold(distro.State, "Running") {
		return nil, nil
	}
	return []string{p.cfg.WSLDistroName}, nil
}

// RunningSandboxCount implements sandbox.IdleRuntimeController for the managed
// WSL distro runtime.
func (p *Provider) RunningSandboxCount(ctx context.Context, runtimeID string) (int, error) {
	if runtimeID != p.cfg.WSLDistroName {
		return 0, nil
	}

	distro, found, err := p.manager.probeDistro(ctx)
	if err != nil {
		return 0, err
	}
	if !found || !strings.EqualFold(distro.State, "Running") {
		return 0, nil
	}
	if p.activeWatchCount() > 0 {
		return 1, nil
	}

	runtimeInfo, err := p.ensureRuntimeInfo(ctx, progressReporter{})
	if err != nil {
		return 0, err
	}
	dockerProvider, err := p.requireDockerProvider(ctx, runtimeInfo)
	if err != nil {
		return 0, err
	}

	sandboxes, err := dockerProvider.List(ctx)
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

// StopRuntime implements sandbox.IdleRuntimeController for the managed WSL distro.
func (p *Provider) StopRuntime(ctx context.Context, runtimeID string) error {
	if runtimeID != p.cfg.WSLDistroName {
		return nil
	}

	result := p.cancelBackgroundBootstrap(ctx)

	p.mu.Lock()
	dockerProvider := p.dockerProvider
	p.dockerProvider = nil
	p.dockerHost = ""
	p.runtimeInfo = nil
	p.mu.Unlock()

	if dockerProvider != nil {
		result = errors.Join(result, dockerProvider.Close())
	}
	result = errors.Join(result, p.closeHostDockerClient())
	if p.manager != nil {
		result = errors.Join(result, p.manager.Stop(ctx))
	}
	return result
}

func (p *Provider) translateCreateOptions(opts sandbox.CreateOptions) (sandbox.CreateOptions, error) {
	if opts.WorkspacePath == "" {
		return opts, nil
	}

	translatedPath, err := TranslatePath(opts.WorkspacePath)
	if err != nil {
		return sandbox.CreateOptions{}, fmt.Errorf("translate workspace path %q: %w", opts.WorkspacePath, err)
	}
	opts.WorkspacePath = translatedPath
	return opts, nil
}

func (p *Provider) retainActiveWatch() func() {
	p.mu.Lock()
	p.activeWatches++
	p.mu.Unlock()

	return func() {
		p.mu.Lock()
		if p.activeWatches > 0 {
			p.activeWatches--
		}
		p.mu.Unlock()
	}
}

func (p *Provider) activeWatchCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.activeWatches
}

func (p *Provider) startManagedWatch(ctx context.Context) (*RuntimeInfo, context.Context, context.CancelFunc, <-chan sandbox.StateEvent, error) {
	runtimeInfo, err := p.ensureRuntimeInfo(ctx, progressReporter{})
	if err != nil {
		return nil, nil, nil, nil, err
	}

	watchCtx, cancel := context.WithCancel(ctx)
	eventCh, err := p.startDockerWatch(watchCtx, runtimeInfo)
	if err != nil {
		cancel()
		return nil, nil, nil, nil, err
	}

	return runtimeInfo, watchCtx, cancel, eventCh, nil
}

func (p *Provider) restartManagedWatch(ctx context.Context) (*RuntimeInfo, context.Context, context.CancelFunc, <-chan sandbox.StateEvent, error) {
	for {
		runtimeInfo, err := p.ensureRuntimeInfo(ctx, progressReporter{})
		if err != nil {
			if !p.waitForWatchRetry(ctx, "Watch: failed to ensure managed WSL runtime for Docker events", err) {
				return nil, nil, nil, nil, ctx.Err()
			}
			continue
		}

		watchCtx, cancel := context.WithCancel(ctx)
		eventCh, err := p.startDockerWatch(watchCtx, runtimeInfo)
		if err == nil {
			return runtimeInfo, watchCtx, cancel, eventCh, nil
		}

		cancel()
		p.clearRuntimeInfo()
		if !p.waitForWatchRetry(ctx, "Watch: failed to start Docker events for managed WSL runtime", err) {
			return nil, nil, nil, nil, ctx.Err()
		}
	}
}

func (p *Provider) forwardManagedWatch(
	ctx context.Context,
	watchCtx context.Context,
	eventCh chan<- sandbox.StateEvent,
	runtimeInfo *RuntimeInfo,
	cancel context.CancelFunc,
	innerCh <-chan sandbox.StateEvent,
) bool {
	ticker := time.NewTicker(p.watchBridgePollIntervalValue())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-watchCtx.Done():
			return ctx.Err() == nil
		case event, ok := <-innerCh:
			if !ok {
				if ctx.Err() != nil {
					return false
				}
				p.clearRuntimeInfo()
				log.Printf("Watch: Docker event stream ended for managed WSL runtime, reconnecting...")
				return true
			}
			select {
			case <-ctx.Done():
				return false
			case eventCh <- event:
			}
		case <-ticker.C:
			ready, err := p.probeBridge(ctx, runtimeInfo)
			if err == nil && ready {
				continue
			}
			if ctx.Err() != nil {
				return false
			}

			if err != nil {
				log.Printf("Watch: failed to probe managed WSL bridge %q: %v", runtimeInfo.BridgeDockerHost, err)
			} else {
				log.Printf("Watch: managed WSL bridge %q is unavailable, restarting Docker event watch", runtimeInfo.BridgeDockerHost)
			}

			p.clearRuntimeInfo()
			cancel()
			return true
		}
	}
}

func (p *Provider) waitForWatchRetry(ctx context.Context, message string, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	if err != nil {
		log.Printf("%s: %v", message, err)
	} else {
		log.Print(message)
	}

	select {
	case <-ctx.Done():
		return false
	case <-time.After(p.watchRetryDelayValue()):
		return true
	}
}

func (p *Provider) watchBridgePollIntervalValue() time.Duration {
	if p.watchBridgePollInterval > 0 {
		return p.watchBridgePollInterval
	}
	return 5 * time.Second
}

func (p *Provider) watchRetryDelayValue() time.Duration {
	if p.watchRetryDelay > 0 {
		return p.watchRetryDelay
	}
	return 5 * time.Second
}

func (p *Provider) probeBridge(ctx context.Context, runtimeInfo *RuntimeInfo) (bool, error) {
	if runtimeInfo == nil || !runtimeInfo.BridgeReady {
		return false, nil
	}
	if p.probeBridgeReady != nil {
		return p.probeBridgeReady(ctx, runtimeInfo)
	}
	if p.manager == nil {
		return true, nil
	}
	return p.manager.probeBridgeReady(ctx, BridgeInfo{
		Type:       runtimeInfo.BridgeType,
		Port:       runtimeInfo.BridgePort,
		PipeName:   runtimeInfo.BridgePipeName,
		DockerHost: runtimeInfo.BridgeDockerHost,
	})
}

func (p *Provider) watchDockerEvents(ctx context.Context, runtimeInfo *RuntimeInfo) (<-chan sandbox.StateEvent, error) {
	dockerProvider, err := p.requireDockerProvider(ctx, runtimeInfo)
	if err != nil {
		return nil, err
	}
	return dockerProvider.Watch(ctx)
}

func (p *Provider) probeRuntimeBridge(ctx context.Context, runtimeInfo *RuntimeInfo) (bool, error) {
	if p.manager == nil {
		return true, nil
	}
	return p.manager.probeBridgeReady(ctx, BridgeInfo{
		Type:       runtimeInfo.BridgeType,
		Port:       runtimeInfo.BridgePort,
		PipeName:   runtimeInfo.BridgePipeName,
		DockerHost: runtimeInfo.BridgeDockerHost,
	})
}

func (p *Provider) requireDockerProvider(ctx context.Context, runtimeInfo *RuntimeInfo) (*docker.Provider, error) {
	if runtimeInfo == nil {
		return nil, fmt.Errorf("missing WSL runtime info")
	}
	if !runtimeInfo.BridgeReady {
		return nil, bridgeNotReadyError(runtimeInfo)
	}
	dockerProvider, err := p.dockerProviderForRuntime(ctx, runtimeInfo)
	if err == nil || !shouldRetryDockerProviderForStaleBridge(runtimeInfo, err) {
		return dockerProvider, err
	}

	p.clearRuntimeInfo()
	if p.manager != nil {
		if clearErr := p.manager.clearBridgeRuntimeState(); clearErr != nil {
			log.Printf("Failed to clear persisted WSL bridge runtime state after Docker connection error on %q: %v", runtimeInfo.BridgeDockerHost, clearErr)
		}
	}

	refreshedRuntimeInfo, refreshErr := p.ensureRuntimeInfo(ctx, progressReporter{})
	if refreshErr != nil {
		return nil, fmt.Errorf("%w; retrying after clearing cached WSL bridge runtime failed: %v", err, refreshErr)
	}
	return p.dockerProviderForRuntime(ctx, refreshedRuntimeInfo)
}

func (p *Provider) dockerProviderForRuntime(ctx context.Context, runtimeInfo *RuntimeInfo) (*docker.Provider, error) {
	if runtimeInfo == nil {
		return nil, fmt.Errorf("missing WSL runtime info")
	}
	if runtimeInfo.BridgeDockerHost == "" {
		return nil, fmt.Errorf("WSL bridge did not provide a Docker host")
	}

	p.mu.RLock()
	if p.dockerProvider != nil && p.dockerHost == runtimeInfo.BridgeDockerHost {
		dockerProvider := p.dockerProvider
		p.mu.RUnlock()
		return dockerProvider, nil
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.dockerProvider != nil && p.dockerHost == runtimeInfo.BridgeDockerHost {
		return p.dockerProvider, nil
	}
	if p.dockerProvider != nil {
		_ = p.dockerProvider.Close()
		p.dockerProvider = nil
		p.dockerHost = ""
	}

	cfg := *p.cfg
	cfg.DockerHost = runtimeInfo.BridgeDockerHost

	dockerProvider, err := p.newDockerProvider(&cfg)
	if err != nil {
		return nil, fmt.Errorf("create inner Docker provider for WSL bridge %q: %w", runtimeInfo.BridgeDockerHost, err)
	}
	if err := p.ensureLocalImageLoad(ctx, dockerProvider); err != nil {
		if dockerProvider.Client() != nil {
			_ = dockerProvider.Close()
		}
		return nil, fmt.Errorf("ensure local sandbox image for WSL bridge %q: %w", runtimeInfo.BridgeDockerHost, err)
	}

	p.dockerProvider = dockerProvider
	p.dockerHost = runtimeInfo.BridgeDockerHost
	return dockerProvider, nil
}

func (p *Provider) buildDockerProvider(cfg *config.Config) (*docker.Provider, error) {
	return docker.NewProvider(cfg, docker.SessionProjectResolver(p.sessionProjectResolver), docker.WithSystemManager(p.systemManager))
}

func shouldRetryDockerProviderForStaleBridge(runtimeInfo *RuntimeInfo, err error) bool {
	if runtimeInfo == nil || err == nil || !strings.EqualFold(runtimeInfo.BridgeType, BridgeTypeTCP) {
		return false
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "failed to connect to docker daemon") ||
		strings.Contains(message, "failed to load image into wsl docker") ||
		strings.Contains(message, "failed to load image into target docker") ||
		strings.Contains(message, "connection refused") ||
		strings.Contains(message, "connectex:")
}

func (p *Provider) getHostDockerClient() (*dockerclient.Client, error) {
	p.hostDockerClientMu.Lock()
	defer p.hostDockerClientMu.Unlock()
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

func (p *Provider) closeHostDockerClient() error {
	p.hostDockerClientMu.Lock()
	cli := p.hostDockerClient
	p.hostDockerClient = nil
	p.hostDockerClientErr = nil
	p.hostDockerClientOnce = sync.Once{}
	p.hostDockerClientMu.Unlock()
	if cli == nil {
		return nil
	}
	return dockerClientClose(cli)
}

func (p *Provider) ensureLocalImageLoaded(ctx context.Context, dockerProvider *docker.Provider) error {
	if !docker.IsLocalImage(p.cfg.SandboxImage) {
		return nil
	}

	hostClient, err := p.getHostDockerClient()
	if err != nil {
		return fmt.Errorf("failed to get host docker client: %w", err)
	}

	if err := docker.EnsureLocalImageLoaded(ctx, hostClient, dockerProvider.Client(), p.cfg.SandboxImage, p.systemManager); err != nil {
		return fmt.Errorf("failed to load image into WSL docker: %w", err)
	}
	return nil
}

func bridgeNotReadyError(runtimeInfo *RuntimeInfo) error {
	if runtimeInfo == nil {
		return fmt.Errorf("wsl docker bridge is not ready yet")
	}
	if runtimeInfo.BridgeDockerHost != "" {
		return fmt.Errorf("wsl docker bridge is not ready yet (configured host: %s)", runtimeInfo.BridgeDockerHost)
	}
	if runtimeInfo.BridgeType == BridgeTypeTCP && runtimeInfo.BridgePort == 0 {
		return fmt.Errorf("wsl docker bridge is not ready yet (tcp bridge will be assigned a loopback port on startup)")
	}
	return fmt.Errorf("wsl docker bridge is not ready yet")
}

func (p *Provider) startBackgroundBootstrap() {
	if p.ensureInstalled == nil || p.ensureRunning == nil {
		return
	}
	installDone := p.beginBackgroundInstallWait()
	bootstrapCtx, done, started := p.beginBackgroundBootstrap()
	if !started {
		return
	}

	if p.systemManager != nil {
		p.systemManager.RegisterTask(startupTaskWSLInstallID, "Preparing managed WSL distro")
		p.systemManager.RegisterTask(startupTaskWSLStartID, "Starting managed WSL distro")
	}

	go func() {
		defer p.finishBackgroundBootstrap(done)
		log.Printf("Starting background WSL distro bootstrap")

		if p.systemManager != nil {
			p.systemManager.StartTask(startupTaskWSLInstallID)
			p.systemManager.UpdateTaskProgress(startupTaskWSLInstallID, 0, "Ensuring managed WSL distro is installed")
		}
		installProgress := progressReporter{
			update: func(progress int, currentOperation string) {
				if p.systemManager == nil {
					return
				}
				p.systemManager.UpdateTaskProgress(startupTaskWSLInstallID, progress, currentOperation)
			},
		}
		if err := p.ensureInstalled(bootstrapCtx, installProgress); err != nil {
			p.finishBackgroundInstallWait(installDone)
			log.Printf("Background WSL distro setup failed: %v", err)
			if p.systemManager != nil {
				p.systemManager.FailTask(startupTaskWSLInstallID, err)
				p.systemManager.FailTask(startupTaskWSLStartID, fmt.Errorf("skipped because WSL distro setup failed: %w", err))
			}
			return
		}
		p.finishBackgroundInstallWait(installDone)
		log.Printf("Background WSL distro install completed; starting runtime bootstrap")
		if p.systemManager != nil {
			p.systemManager.CompleteTask(startupTaskWSLInstallID)
			p.systemManager.StartTask(startupTaskWSLStartID)
			p.systemManager.UpdateTaskProgress(startupTaskWSLStartID, 0, "Ensuring managed WSL distro is running")
		}

		startProgress := progressReporter{
			update: func(progress int, currentOperation string) {
				if p.systemManager == nil {
					return
				}
				p.systemManager.UpdateTaskProgress(startupTaskWSLStartID, progress, currentOperation)
			},
		}
		runtimeInfo, err := p.ensureRuntimeInfo(bootstrapCtx, startProgress)
		if err != nil {
			log.Printf("Background WSL distro startup failed: %v", err)
			if p.systemManager != nil {
				p.systemManager.FailTask(startupTaskWSLStartID, err)
			}
			return
		}

		log.Printf(
			"Background WSL distro startup completed (state: %s, bridge_ready: %t, docker_host: %s)",
			runtimeInfo.DistroState,
			runtimeInfo.BridgeReady,
			runtimeInfo.BridgeDockerHost,
		)
		if p.systemManager != nil {
			p.systemManager.CompleteTask(startupTaskWSLStartID)
		}
	}()
}
