// Package docker provides a Docker-based implementation of the sandbox.Provider interface.
package docker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"net"
	"net/http"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types"
	containerTypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	imageTypes "github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	volumeTypes "github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	dockercontext "github.com/docker/go-sdk/context"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/sandbox"
	"github.com/obot-platform/discobot/server/internal/sysinfo"
)

const (
	// labelSecret is the label key for storing the raw shared secret.
	labelSecret = "discobot.secret"

	// containerPort is the fixed port exposed by all sandboxes.
	containerPort = 3002

	// workspacePath is where workspaces are mounted inside the container.
	workspacePath = "/.workspace"

	// dataVolumePath is where the persistent data volume is mounted inside the container.
	dataVolumePath = "/.data"

	// dataVolumePrefix is the prefix for data volume names.
	dataVolumePrefix = "discobot-data-"

	sessionEnvWrapperCmd     = "discobot-session-env"
	hostInspectContainerName = "discobot-host-inspect"

	httpClientTargetCacheTTL = 10 * time.Second
)

// DetectDockerHost resolves the Docker host from the current Docker context.
// This handles Docker Desktop, Colima, Rancher Desktop, Podman, and custom
// contexts automatically. Returns empty string if detection fails.
func DetectDockerHost() string {
	host, err := dockercontext.CurrentDockerHost()
	if err != nil {
		return ""
	}
	if host != "" {
		log.Printf("Detected Docker host from context: %s", host)
	}
	return host
}

// SessionProjectResolver looks up the project ID for a session from the database.
type SessionProjectResolver func(ctx context.Context, sessionID string) (projectID string, err error)

// Provider implements the sandbox.Provider interface using Docker.
type Provider struct {
	client *client.Client
	cfg    *config.Config

	// containerIDs maps sessionID -> Docker container ID
	containerIDs   map[string]string
	containerIDsMu sync.RWMutex

	// lifecycleMu serializes cache clearing against sandbox lifecycle mutations.
	lifecycleMu sync.RWMutex

	httpClients         *sandbox.HTTPClientCache
	httpClientTargets   map[string]cachedHTTPClientTarget
	httpClientTargetsMu sync.Mutex

	// vsockDialer is an optional custom dialer for VSOCK connections
	vsockDialer func(ctx context.Context, network, addr string) (net.Conn, error)

	// workspaceMountSourceResolver converts host workspace paths into paths that
	// are valid on the Docker daemon host.
	workspaceMountSourceResolver func(string) (string, error)

	// sessionProjectResolver looks up session -> project mapping from the database.
	sessionProjectResolver SessionProjectResolver

	// systemManager tracks startup tasks and system status (optional)
	systemManager SystemManager

	// ensureImage synchronization: only one pull happens, all callers wait on the same result
	ensureImageOnce sync.Once
	ensureImageDone chan struct{}
	ensureImageErr  error

	stopCh   chan struct{}
	stopOnce sync.Once
}

type cachedHTTPClientTarget struct {
	BaseURL   string
	ExpiresAt time.Time
}

// SystemManager interface for tracking startup tasks
type SystemManager interface {
	RegisterTask(id, name string)
	StartTask(id string)
	UpdateTaskProgress(id string, progress int, currentOperation string)
	UpdateTaskBytes(id string, bytesDownloaded, totalBytes int64)
	CompleteTask(id string)
	FailTask(id string, err error)
}

// Option configures the Docker provider.
type Option func(*Provider)

// WithVsockDialer configures the Docker provider to use a VSOCK dialer
// instead of the standard Docker socket. This is used when Docker daemon
// runs inside a VM and is accessed via VSOCK.
func WithVsockDialer(dialer func(ctx context.Context, network, addr string) (net.Conn, error)) Option {
	return func(p *Provider) {
		p.vsockDialer = dialer
	}
}

// WithWorkspaceMountSourceResolver configures how host workspace paths are
// resolved before they are sent to the Docker daemon as bind-mount sources.
func WithWorkspaceMountSourceResolver(resolver func(string) (string, error)) Option {
	return func(p *Provider) {
		p.workspaceMountSourceResolver = resolver
	}
}

// WithSystemManager configures the Docker provider with a system manager for tracking startup tasks
func WithSystemManager(sm SystemManager) Option {
	return func(p *Provider) {
		p.systemManager = sm
	}
}

// NewProvider creates a new Docker sandbox provider.
// The sessionProjectResolver is required for mapping sessions to projects for cache volumes.
// Use WithVsockDialer option to connect to Docker daemon inside a VM via VSOCK.
func NewProvider(cfg *config.Config, sessionProjectResolver SessionProjectResolver, opts ...Option) (*Provider, error) {
	if sessionProjectResolver == nil {
		return nil, fmt.Errorf("sessionProjectResolver is required")
	}

	p := &Provider{
		cfg:                    cfg,
		containerIDs:           make(map[string]string),
		httpClients:            sandbox.NewHTTPClientCache(),
		httpClientTargets:      make(map[string]cachedHTTPClientTarget),
		sessionProjectResolver: sessionProjectResolver,
		stopCh:                 make(chan struct{}),
	}

	// Apply options
	for _, opt := range opts {
		opt(p)
	}
	if p.workspaceMountSourceResolver == nil {
		p.workspaceMountSourceResolver = resolveWorkspaceMountSource
	}

	var cli *client.Client
	var err error

	// Create Docker client with custom transport if VSOCK dialer is provided
	if p.vsockDialer != nil {
		// Use VSOCK transport
		httpClient := &http.Client{
			Transport: &http.Transport{
				DialContext: p.vsockDialer,
			},
		}

		cli, err = client.NewClientWithOpts(
			client.WithHost("http://localhost"), // must be before WithHTTPClient so it doesn't modify our VSOCK transport
			client.WithHTTPClient(httpClient),
			client.WithAPIVersionNegotiation(),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create docker client with vsock: %w", err)
		}
	} else {
		// Use standard Docker client (local socket, configured host, or WSL stdio proxy)
		cli, err = NewAPIClient(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create docker client: %w", err)
		}
	}

	p.client = cli
	p.ensureImageDone = make(chan struct{})

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := p.client.Ping(ctx); err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("failed to connect to docker daemon: %w", err)
	}

	// Kick off image pull in the background (non-blocking).
	// EnsureImage is synchronized: the first caller triggers the pull, all others wait.
	go func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go func() {
			select {
			case <-p.stopCh:
				cancel()
			case <-ctx.Done():
			}
		}()

		if err := p.EnsureImage(ctx); err != nil {
			log.Printf("Docker provider background image initialization failed: %v", err)
			return
		}
		if err := p.EnsureInspectionContainer(ctx); err != nil {
			log.Printf("Docker provider background inspection container initialization failed: %v", err)
			return
		}

		log.Printf("Docker provider background initialization complete")
	}()

	log.Printf("Docker provider initialized, image pull running in background")
	return p, nil
}

func (p *Provider) Definition() sandbox.ProviderDefinition {
	configFields := []sandbox.ProviderConfigField{
		{Key: "host", Label: "Docker host", Type: "text", Placeholder: "unix:///var/run/docker.sock", Description: "Optional Docker daemon socket or host URL.", Advanced: true},
		{Key: "network", Label: "Docker network", Type: "text", Placeholder: "bridge", Description: "Optional Docker network for sandbox containers.", Advanced: true},
	}
	if runtime.GOOS == "windows" {
		configFields = append(configFields, sandbox.ProviderConfigField{Key: "wslDistro", Label: "WSL distro", Type: "text", Placeholder: "Ubuntu", Description: "Optional Windows WSL distro used to proxy host Docker access.", Advanced: true})
	}
	return sandbox.ProviderDefinition{
		Name:         "Docker",
		Icon:         "simple:docker",
		Description:  "Docker sandbox driver",
		ConfigFields: configFields,
	}
}

// containerName generates a consistent container name from session ID.
func containerName(sessionID string) string {
	return fmt.Sprintf("discobot-session-%s", sessionID)
}

// volumeName returns the Docker volume name for a session's data volume.
func volumeName(sessionID string) string {
	return fmt.Sprintf("%s%s", dataVolumePrefix, sessionID)
}

func inspectionContainerCommand() []string {
	return []string{
		"/bin/sh",
		"-c",
		`trap 'exit 0' TERM INT QUIT; while :; do sleep 2147483647 & wait $!; done`,
	}
}

func inspectionContainerConfig(image string) (*containerTypes.Config, *containerTypes.HostConfig) {
	containerConfig := &containerTypes.Config{
		Image:  image,
		Cmd:    inspectionContainerCommand(),
		Tty:    true,
		Labels: map[string]string{"discobot.host.inspect": "true", "discobot.managed": "true"},
	}

	hostConfig := &containerTypes.HostConfig{
		Privileged:   true,
		NetworkMode:  "host",
		PidMode:      "host",
		IpcMode:      "host",
		UTSMode:      "host",
		CgroupnsMode: containerTypes.CgroupnsModeHost,
		Binds:        []string{"/var/run/docker.sock:/var/run/docker.sock"},
		RestartPolicy: containerTypes.RestartPolicy{
			Name: containerTypes.RestartPolicyAlways,
		},
	}
	hostConfig.Ulimits = []*containerTypes.Ulimit{{
		Name: "nofile",
		Soft: 1048576,
		Hard: 1048576,
	}}

	return containerConfig, hostConfig
}

func inspectionContainerNeedsRecreate(existing containerTypes.InspectResponse, image string) bool {
	return existing.Config == nil ||
		existing.HostConfig == nil ||
		existing.Config.Image != image ||
		!existing.HostConfig.Privileged ||
		existing.HostConfig.NetworkMode != "host" ||
		existing.HostConfig.PidMode != "host" ||
		existing.HostConfig.IpcMode != "host" ||
		existing.HostConfig.UTSMode != "host" ||
		existing.HostConfig.CgroupnsMode != containerTypes.CgroupnsModeHost
}

func resolveWorkspaceMountSource(sourcePath string) (string, error) {
	sourcePath = strings.TrimSpace(sourcePath)
	if sourcePath == "" {
		return "", nil
	}
	if strings.HasPrefix(sourcePath, "/") {
		return path.Clean(sourcePath), nil
	}
	if isWindowsAbsolutePath(sourcePath) || filepath.IsAbs(sourcePath) {
		return sourcePath, nil
	}
	absPath, err := filepath.Abs(sourcePath)
	if err != nil {
		return "", err
	}
	return absPath, nil
}

func (p *Provider) resolveWorkspaceMountSource(sourcePath string) (string, error) {
	resolver := p.workspaceMountSourceResolver
	if resolver == nil {
		resolver = resolveWorkspaceMountSource
	}
	return resolver(sourcePath)
}

func isWindowsAbsolutePath(path string) bool {
	if len(path) >= 3 && isASCIIAlpha(path[0]) && path[1] == ':' && (path[2] == '\\' || path[2] == '/') {
		return true
	}
	return strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, `//`)
}

func isASCIIAlpha(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

func (p *Provider) attachToContainer(ctx context.Context, containerID string, opts sandbox.AttachOptions) (sandbox.PTY, error) {
	cmd := opts.Cmd
	if len(cmd) == 0 {
		cmd = p.detectShell(ctx, containerID)
	}
	wrappedCmd := wrapCommandWithSessionEnv(cmd, opts.Env)

	env := make([]string, 0, len(opts.Env))
	for k, v := range opts.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	execConfig := containerTypes.ExecOptions{
		Cmd:          wrappedCmd,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
		Env:          env,
		User:         opts.User,
		WorkingDir:   opts.WorkDir,
	}

	execCreate, err := p.client.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", sandbox.ErrAttachFailed, err)
	}

	resp, err := p.client.ContainerExecAttach(ctx, execCreate.ID, containerTypes.ExecStartOptions{
		Tty: true,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", sandbox.ErrAttachFailed, err)
	}

	if opts.Rows > 0 && opts.Cols > 0 {
		_ = p.client.ContainerExecResize(ctx, execCreate.ID, containerTypes.ResizeOptions{
			Height: uint(opts.Rows),
			Width:  uint(opts.Cols),
		})
	}

	return &dockerPTY{
		client:   p.client,
		execID:   execCreate.ID,
		hijacked: resp,
		done:     make(chan struct{}),
	}, nil
}

// EnsureInspectionContainer makes sure the host-inspection container is running.
func (p *Provider) EnsureInspectionContainer(ctx context.Context) error {
	if err := p.EnsureImage(ctx); err != nil {
		return fmt.Errorf("failed to ensure sandbox image for inspection container: %w", err)
	}

	existing, err := p.client.ContainerInspect(ctx, hostInspectContainerName)
	if err == nil {
		if inspectionContainerNeedsRecreate(existing, p.cfg.SandboxImage) {
			log.Printf("Inspection container %s has stale config, recreating", hostInspectContainerName)
			timeout := 10
			_ = p.client.ContainerStop(ctx, existing.ID, containerTypes.StopOptions{Timeout: &timeout})
			if err := p.client.ContainerRemove(ctx, existing.ID, containerTypes.RemoveOptions{Force: true}); err != nil {
				return fmt.Errorf("failed to remove stale inspection container: %w", err)
			}
		} else if existing.State != nil && existing.State.Running {
			return nil
		} else {
			if err := p.client.ContainerStart(ctx, existing.ID, containerTypes.StartOptions{}); err != nil {
				return fmt.Errorf("failed to start inspection container: %w", err)
			}
			log.Printf("Started inspection container %s (%s)", hostInspectContainerName, existing.ID[:12])
			return nil
		}
	} else if !cerrdefs.IsNotFound(err) {
		return fmt.Errorf("failed to inspect inspection container: %w", err)
	}

	containerConfig, hostConfig := inspectionContainerConfig(p.cfg.SandboxImage)
	resp, err := p.client.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, hostInspectContainerName)
	if err != nil {
		return fmt.Errorf("failed to create inspection container: %w", err)
	}
	if err := p.client.ContainerStart(ctx, resp.ID, containerTypes.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start inspection container: %w", err)
	}

	log.Printf("Started inspection container %s (%s)", hostInspectContainerName, resp.ID[:12])
	return nil
}

// GetProjectInspectionInfo reports inspection-container availability.
func (p *Provider) GetProjectInspectionInfo(_ context.Context, _ string) (*sandbox.ProjectInspectionInfo, error) {
	return &sandbox.ProjectInspectionInfo{
		Provider:      "docker",
		Available:     true,
		ContainerName: hostInspectContainerName,
		Scope:         "host",
	}, nil
}

// AttachProjectInspection attaches to the host inspection container shell.
func (p *Provider) AttachProjectInspection(ctx context.Context, _ string, opts sandbox.AttachOptions) (sandbox.PTY, error) {
	if err := p.EnsureInspectionContainer(ctx); err != nil {
		return nil, err
	}

	info, err := p.client.ContainerInspect(ctx, hostInspectContainerName)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect inspection container: %w", err)
	}

	return p.attachToContainer(ctx, info.ID, opts)
}

// ImageExists checks if the configured sandbox image is available locally.
func (p *Provider) ImageExists(ctx context.Context) bool {
	_, err := p.client.ImageInspect(ctx, p.cfg.SandboxImage)
	return err == nil
}

// Image returns the configured sandbox image name.
func (p *Provider) Image() string {
	return p.cfg.SandboxImage
}

func (p *Provider) IsLocal() bool {
	return IsLocalHost(p.cfg.DockerHost)
}

// Create creates a new Docker container for the given session.
func (p *Provider) Create(ctx context.Context, state []byte, sessionID string, opts sandbox.CreateOptions) (*sandbox.Sandbox, []byte, error) {
	p.lifecycleMu.RLock()
	defer p.lifecycleMu.RUnlock()

	// Check if sandbox already exists in cache
	p.containerIDsMu.RLock()
	cachedID, existsInCache := p.containerIDs[sessionID]
	p.containerIDsMu.RUnlock()

	name := containerName(sessionID)

	// Check if container exists by name (from previous runs)
	if existing, err := p.client.ContainerInspect(ctx, name); err == nil && existing.ContainerJSONBase != nil {
		// If we have a cached ID and it matches the existing container, return error
		if existsInCache && cachedID == existing.ID {
			return nil, state, sandbox.ErrAlreadyExists
		}
		// Otherwise, remove the stale container (force cleanup from previous runs)
		log.Printf("Removing stale container %s (%s) before creating new sandbox", existing.ID[:12], name)
		if err := p.client.ContainerRemove(ctx, existing.ID, containerTypes.RemoveOptions{Force: true}); err != nil {
			return nil, state, fmt.Errorf("failed to remove stale container: %w", err)
		}
		// Clear any stale cache entry
		p.clearContainerID(sessionID)
	} else if existsInCache {
		// Cache has an entry but container doesn't exist - clear stale cache
		p.clearContainerID(sessionID)
	}

	// Use the globally configured sandbox image
	image := p.cfg.SandboxImage

	// Wait for image to be available (pulled on startup or by first caller)
	if err := p.EnsureImage(ctx); err != nil {
		return nil, state, fmt.Errorf("%w: %w", sandbox.ErrInvalidImage, err)
	}

	// Create data volume for persistent storage
	dataVolName := volumeName(sessionID)
	_, err := p.client.VolumeCreate(ctx, volumeTypes.CreateOptions{
		Name: dataVolName,
		Labels: map[string]string{
			"discobot.session.id": sessionID,
			"discobot.managed":    "true",
		},
	})
	if err != nil {
		return nil, state, fmt.Errorf("failed to create data volume: %w", err)
	}

	// Prepare labels - store the raw secret as a label
	labels := map[string]string{
		"discobot.session.id": sessionID,
		"discobot.managed":    "true",
	}
	if opts.SharedSecret != "" {
		labels[labelSecret] = opts.SharedSecret
	}
	maps.Copy(labels, opts.Labels)

	envMap := maps.Clone(opts.Env)
	keys := make([]string, 0, len(envMap))
	for key := range envMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+envMap[key])
	}

	// Container configuration
	containerConfig := &containerTypes.Config{
		Image:        image,
		Env:          env,
		Labels:       labels,
		Hostname:     "discobot",
		Tty:          true,
		OpenStdin:    true,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
	}

	// Host configuration with resource limits
	// Privileged mode (set below) grants all capabilities and device access,
	// so explicit CapAdd and device mappings are not needed.
	hostConfig := &containerTypes.HostConfig{
		// Mount the data volume for persistent storage
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeVolume,
				Source: dataVolName,
				Target: dataVolumePath,
			},
			// systemd needs read-write access to the host cgroup hierarchy
			{
				Type:     mount.TypeBind,
				Source:   "/sys/fs/cgroup",
				Target:   "/sys/fs/cgroup",
				ReadOnly: false,
			},
		},
		// Share host cgroup namespace (required for systemd in container)
		CgroupnsMode: containerTypes.CgroupnsModeHost,
		// systemd requires tmpfs on /run and /run/lock
		Tmpfs: map[string]string{
			"/run":      "exec,mode=755",
			"/run/lock": "exec,mode=755",
		},
	}

	// Apply resource limits
	if opts.Resources.MemoryMB > 0 {
		hostConfig.Memory = int64(opts.Resources.MemoryMB) * 1024 * 1024
	}
	if opts.Resources.CPUCores > 0 {
		hostConfig.NanoCPUs = int64(opts.Resources.CPUCores * 1e9)
	}

	// Dedicate 25% of host memory to /dev/shm (needed for Chromium, etc.)
	hostConfig.ShmSize = int64(sysinfo.TotalMemoryBytes() / 4)

	// Mount workspace directory (always a local path)
	if opts.WorkspacePath != "" {
		sourcePath, err := p.resolveWorkspaceMountSource(opts.WorkspacePath)
		if err != nil {
			return nil, state, fmt.Errorf("%w: failed to resolve workspace mount source: %w", sandbox.ErrStartFailed, err)
		}

		hostConfig.Mounts = append(hostConfig.Mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   sourcePath,
			Target:   workspacePath,
			ReadOnly: true, // Read-only for the origin
		})
	}

	// Resolve project for cache volume
	projectID, err := p.sessionProjectResolver(ctx, sessionID)
	if err != nil {
		return nil, state, fmt.Errorf("failed to resolve project for session %s: %w", sessionID, err)
	}
	if projectID == "" {
		return nil, state, fmt.Errorf("session %s has no associated project", sessionID)
	}

	// Ensure the cache volume exists
	cacheVolName, err := p.ensureCacheVolume(ctx, projectID)
	if err != nil {
		return nil, state, fmt.Errorf("failed to create cache volume for project %s: %w", projectID, err)
	}

	// Mount the entire cache volume at /.data/cache
	// The agent will bind-mount individual directories from here
	hostConfig.Mounts = append(hostConfig.Mounts, mount.Mount{
		Type:   mount.TypeVolume,
		Source: cacheVolName,
		Target: "/.data/cache",
	})
	log.Printf("Mounted cache volume %s at /.data/cache for session %s", cacheVolName, sessionID)

	// Configure network
	if p.cfg.DockerNetwork != "" {
		hostConfig.NetworkMode = containerTypes.NetworkMode(p.cfg.DockerNetwork)
	}

	// Override the container's DNS resolvers when configured (SANDBOX_DNS).
	// Needed when the VM's default resolver (the vmnet gateway) can't forward to
	// the host's resolver — e.g. a host running DNS on 127.0.0.1 (VPN/local
	// resolver), which the gateway can't reach. A public resolver routes out via
	// NAT and avoids the broken gateway DNS proxy.
	if len(p.cfg.SandboxDNS) > 0 {
		hostConfig.DNS = append([]string(nil), p.cfg.SandboxDNS...)
	}

	// Raise the open file limit so processes inside the container don't hit
	// the default 1024 soft limit (tools like Claude Code can easily exhaust it).
	hostConfig.Ulimits = []*containerTypes.Ulimit{{
		Name: "nofile",
		Soft: 1048576,
		Hard: 1048576,
	}}

	// Enable privileged mode for running Docker daemon inside container
	// The container runs its own Docker daemon (started by discobot-sandbox-init if dockerd is available)
	hostConfig.Privileged = true

	// Always expose port 3002 with a random host port
	port := nat.Port(fmt.Sprintf("%d/tcp", containerPort))
	containerConfig.ExposedPorts = nat.PortSet{port: struct{}{}}
	hostConfig.PortBindings = nat.PortMap{
		port: []nat.PortBinding{{
			HostIP:   "127.0.0.1",
			HostPort: "", // Empty = Docker assigns random available port
		}},
	}

	// Create container
	resp, err := p.client.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, name)
	if err != nil {
		return nil, state, fmt.Errorf("%w: %w", sandbox.ErrStartFailed, err)
	}

	// Store mapping
	p.containerIDsMu.Lock()
	p.containerIDs[sessionID] = resp.ID
	p.containerIDsMu.Unlock()

	now := time.Now()
	return &sandbox.Sandbox{
		ID:        resp.ID,
		SessionID: sessionID,
		Status:    sandbox.StatusCreated,
		Image:     image,
		CreatedAt: now,
		Metadata: map[string]string{
			"name": name,
		},
	}, state, nil
}

// VerifySecret checks if a plaintext secret matches a salted hash.
// The hashedSecret should be in "salt:hash" format as produced by hashSecret.
func VerifySecret(plaintext, hashedSecret string) bool {
	parts := strings.SplitN(hashedSecret, ":", 2)
	if len(parts) != 2 {
		return false
	}

	salt, err := hex.DecodeString(parts[0])
	if err != nil {
		return false
	}

	h := sha256.New()
	h.Write(salt)
	h.Write([]byte(plaintext))
	expectedHash := hex.EncodeToString(h.Sum(nil))

	return expectedHash == parts[1]
}

// EnsureImage ensures the sandbox image is available locally. If the image needs to
// be pulled, it blocks until the pull completes. Multiple callers are synchronized —
// only one pull occurs and all callers wait on the same result. Progress is reported
// via the system manager if configured.
func (p *Provider) EnsureImage(ctx context.Context) error {
	p.ensureImageOnce.Do(func() {
		go p.doEnsureImage()
	})
	select {
	case <-p.ensureImageDone:
		return p.ensureImageErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

// doEnsureImage performs the actual image check/pull with retry and progress tracking.
// It closes ensureImageDone when complete and sets ensureImageErr on failure.
func (p *Provider) doEnsureImage() {
	defer close(p.ensureImageDone)

	image := p.cfg.SandboxImage
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		select {
		case <-p.stopCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	// Local images can't be pulled from a registry. They are loaded externally
	// (e.g., via ensureImageInVM in the VZ provider which transfers from host Docker).
	// Don't set an error here — the image may be loaded after provider creation.
	if IsLocalImage(image) {
		checkCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, err := p.client.ImageInspect(checkCtx, image)
		cancel()

		if err == nil {
			log.Printf("Local sandbox image exists: %s", image)
		} else {
			log.Printf("Local sandbox image not yet available: %s (expected to be loaded externally)", image)
		}
		return
	}

	// Check if image already exists — no task registration needed
	checkCtx, checkCancel := context.WithTimeout(context.Background(), 10*time.Second)
	_, err := p.client.ImageInspect(checkCtx, image)
	checkCancel()
	if err == nil {
		log.Printf("Sandbox image already exists: %s", image)
		return
	}

	// Image needs to be pulled — register startup task for UI progress
	if p.systemManager != nil {
		p.systemManager.RegisterTask("docker-pull", fmt.Sprintf("Pulling runtime image: %s", image))
		p.systemManager.StartTask("docker-pull")
	}

	// Pull with retry and exponential backoff
	backoff := 5 * time.Second
	maxBackoff := 5 * time.Minute
	attempt := 1

	for {
		if ctx.Err() != nil {
			p.ensureImageErr = ctx.Err()
			return
		}

		pullCtx, pullCancel := context.WithTimeout(ctx, 5*time.Minute)
		err := p.pullSandboxImage(pullCtx, image)
		pullCancel()

		if err == nil {
			log.Printf("Successfully pulled sandbox image: %s", image)
			if p.systemManager != nil {
				p.systemManager.CompleteTask("docker-pull")
			}
			return
		}

		if ctx.Err() != nil {
			p.ensureImageErr = ctx.Err()
			return
		}

		log.Printf("Warning: Failed to pull sandbox image (attempt %d): %v", attempt, err)
		log.Printf("Retrying in %v...", backoff)

		if err := waitForRetry(ctx, backoff); err != nil {
			p.ensureImageErr = err
			return
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
		attempt++
	}
}

// IsLocalImage checks if an image is a local image that cannot be pulled from a registry.
// Local images include:
// - Images with discobot-local/ prefix (locally built images)
// - Bare digest references (sha256:...)
func IsLocalImage(image string) bool {
	return strings.HasPrefix(image, "discobot-local/") || strings.HasPrefix(image, "sha256:")
}

func isLocalImage(image string) bool {
	return IsLocalImage(image)
}

// pullSandboxImage pulls the sandbox image if it doesn't exist locally and can be pulled.
func (p *Provider) pullSandboxImage(ctx context.Context, image string) error {
	// Check if image already exists locally
	_, err := p.client.ImageInspect(ctx, image)
	if err == nil {
		log.Printf("Sandbox image already exists locally, skipping pull: %s", image)
		if p.systemManager != nil {
			p.systemManager.UpdateTaskProgress("docker-pull", 100, "Image already exists")
		}
		return nil
	}

	// Image doesn't exist locally. Check if it's a local-only image that can't be pulled.
	if isLocalImage(image) {
		log.Printf("Sandbox image is a local image and doesn't exist, cannot pull: %s", image)
		return fmt.Errorf("local image %s not found and cannot be pulled from registry", image)
	}

	// Image doesn't exist, pull it (works for both tags and digest references)
	log.Printf("Pulling sandbox image: %s", image)
	reader, err := p.client.ImagePull(ctx, image, imageTypes.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull sandbox image %s: %w", image, err)
	}
	defer func() { _ = reader.Close() }()

	// Process pull progress and update system manager if available
	if p.systemManager != nil {
		err = p.processPullProgress(reader, "docker-pull")
	} else {
		// No system manager - just drain the reader
		_, err = io.Copy(io.Discard, reader)
	}

	if err != nil {
		return fmt.Errorf("failed to complete sandbox image pull for %s: %w", image, err)
	}

	log.Printf("Successfully pulled sandbox image: %s", image)
	return nil
}

// processPullProgress reads Docker pull events and updates the system manager with real progress
func (p *Provider) processPullProgress(reader io.Reader, taskID string) error {
	decoder := json.NewDecoder(reader)

	// Track per-layer download progress (keep maximum to avoid going backwards)
	layerDownloadProgress := make(map[string]int64) // layerID -> max bytes downloaded

	for {
		var rawEvent map[string]any
		if err := decoder.Decode(&rawEvent); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		// Extract fields we care about
		status, _ := rawEvent["status"].(string)
		id, _ := rawEvent["id"].(string)

		var current int64
		if pd, ok := rawEvent["progressDetail"].(map[string]any); ok {
			if c, ok := pd["current"].(float64); ok {
				current = int64(c)
			}
		}

		// Only track "Downloading" events - ignore extraction
		if status == "Downloading" && id != "" && current > 0 {
			// Track download progress - keep maximum
			if existing, exists := layerDownloadProgress[id]; !exists || current > existing {
				layerDownloadProgress[id] = current

				// Calculate aggregate download progress across all layers
				var downloadedBytes int64
				for _, bytes := range layerDownloadProgress {
					downloadedBytes += bytes
				}

				// Fake total estimate: 1000MB
				totalBytes := int64(1000 * 1024 * 1024)

				// Update system manager
				if downloadedBytes > 0 {
					p.systemManager.UpdateTaskBytes(taskID, downloadedBytes, totalBytes)
				}
			}
		}
	}

	return nil
}

// CurrentImageID returns the immutable image ID for the configured sandbox image.
func (p *Provider) CurrentImageID(ctx context.Context) (string, error) {
	info, err := p.client.ImageInspect(ctx, p.cfg.SandboxImage)
	if err != nil {
		return "", fmt.Errorf("failed to inspect current sandbox image %s: %w", p.cfg.SandboxImage, err)
	}
	return info.ID, nil
}

// CleanupUnusedImages removes labeled sandbox images that are no longer referenced
// by the configured image or by any managed sandbox container.
func (p *Provider) CleanupUnusedImages(ctx context.Context) error {
	// List all images with the discobot sandbox label
	images, err := p.client.ImageList(ctx, imageTypes.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", "io.discobot.sandbox-image=true"),
		),
	})
	if err != nil {
		return fmt.Errorf("failed to list sandbox images: %w", err)
	}

	currentImageID, err := p.CurrentImageID(ctx)
	if err != nil {
		log.Printf("Skipping sandbox image cleanup: unable to resolve current sandbox image ID: %v", err)
		return nil
	}

	containers, err := p.client.ContainerList(ctx, containerTypes.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", "discobot.managed=true"),
		),
	})
	if err != nil {
		return fmt.Errorf("failed to list managed sandbox containers: %w", err)
	}

	protectedImageIDs := map[string]struct{}{
		currentImageID: {},
	}

	for _, ctr := range containers {
		if ctr.ImageID == "" {
			continue
		}
		protectedImageIDs[ctr.ImageID] = struct{}{}
	}

	deletedCount := 0
	for _, img := range images {
		if _, ok := protectedImageIDs[img.ID]; ok {
			log.Printf("Skipping sandbox image cleanup (still referenced): %s (ID: %s)", img.RepoTags, img.ID)
			continue
		}

		log.Printf("Removing unused sandbox image: %s (ID: %s)", img.RepoTags, img.ID)
		_, err := p.client.ImageRemove(ctx, img.ID, imageTypes.RemoveOptions{
			Force:         true, // Force removal even if image has tags
			PruneChildren: true,
		})
		if err != nil {
			log.Printf("Warning: Failed to remove old sandbox image %s: %v", img.ID, err)
			continue
		}
		deletedCount++
	}

	if deletedCount > 0 {
		log.Printf("Cleaned up %d old sandbox image(s)", deletedCount)
	}

	return nil
}

// Reconcile performs provider-specific reconciliation on startup.
func (p *Provider) Reconcile(_ context.Context) error {
	return nil
}

// RemoveProject cleans up all provider-managed resources for a project.
func (p *Provider) RemoveProject(ctx context.Context, projectID string) error {
	if err := p.ClearCache(ctx, projectID); err != nil {
		log.Printf("Warning: failed to remove cache volume for project %s: %v", projectID, err)
	}
	return nil
}

// Start starts a previously created sandbox.
func (p *Provider) Start(ctx context.Context, state []byte, sessionID string) ([]byte, error) {
	p.lifecycleMu.RLock()
	defer p.lifecycleMu.RUnlock()

	containerID, err := p.getContainerID(ctx, sessionID)
	if err != nil {
		return state, err
	}

	if err := p.client.ContainerStart(ctx, containerID, containerTypes.StartOptions{}); err != nil {
		return state, fmt.Errorf("%w: %w", sandbox.ErrStartFailed, err)
	}

	return state, nil
}

// Stop stops a running sandbox gracefully.
func (p *Provider) Stop(ctx context.Context, state []byte, sessionID string, timeout time.Duration) ([]byte, error) {
	p.lifecycleMu.RLock()
	defer p.lifecycleMu.RUnlock()
	p.removeHTTPClientCaches(sessionID)

	containerID, err := p.getContainerID(ctx, sessionID)
	if err != nil {
		return state, err
	}

	timeoutSeconds := int(timeout.Seconds())
	stopOptions := containerTypes.StopOptions{
		Timeout: &timeoutSeconds,
	}

	if err := p.client.ContainerStop(ctx, containerID, stopOptions); err != nil {
		return state, fmt.Errorf("failed to stop sandbox: %w", err)
	}

	return state, nil
}

// Remove removes a sandbox container and optionally its associated data volume.
// By default, data volumes are preserved (useful for rebuilds).
// Pass sandbox.RemoveVolumes() to delete volumes (for session deletion).
func (p *Provider) Remove(ctx context.Context, state []byte, sessionID string, opts ...sandbox.RemoveOption) ([]byte, error) {
	p.lifecycleMu.RLock()
	defer p.lifecycleMu.RUnlock()

	cfg := sandbox.ParseRemoveOptions(opts)

	containerID, err := p.getContainerID(ctx, sessionID)
	if err != nil {
		if !errors.Is(err, sandbox.ErrNotFound) {
			return state, err
		}
		// Container not found, but continue to clean up volume if requested
		containerID = ""
	}

	if containerID != "" {
		removeOptions := containerTypes.RemoveOptions{
			Force:         true,
			RemoveVolumes: true, // Only removes anonymous volumes, not named volumes
		}

		if err := p.client.ContainerRemove(ctx, containerID, removeOptions); err != nil {
			return state, fmt.Errorf("failed to remove sandbox container: %w", err)
		}

		// Remove from mapping
		p.containerIDsMu.Lock()
		delete(p.containerIDs, sessionID)
		p.containerIDsMu.Unlock()
	}

	p.removeHTTPClientCaches(sessionID)

	// Explicitly remove the named data volume if requested
	if cfg.RemoveVolumes {
		dataVolName := volumeName(sessionID)
		if err := p.client.VolumeRemove(ctx, dataVolName, true); err != nil {
			// Don't fail if volume doesn't exist
			if !cerrdefs.IsNotFound(err) {
				return state, fmt.Errorf("failed to remove data volume %s: %w", dataVolName, err)
			}
		}
	}

	return state, nil
}

func applyContainerState(sb *sandbox.Sandbox, state *containerTypes.State) {
	if state == nil {
		sb.Status = sandbox.StatusCreated
		return
	}

	switch {
	case state.Running:
		sb.Status = sandbox.StatusRunning
		if started, err := time.Parse(time.RFC3339Nano, state.StartedAt); err == nil {
			sb.StartedAt = &started
		}
	case state.Paused:
		sb.Status = sandbox.StatusStopped
	case state.Dead || state.OOMKilled:
		sb.Status = sandbox.StatusFailed
		sb.Error = state.Error
	case state.FinishedAt != "" && state.FinishedAt != "0001-01-01T00:00:00Z":
		sb.Status = sandbox.StatusStopped
		if stopped, err := time.Parse(time.RFC3339Nano, state.FinishedAt); err == nil {
			sb.StoppedAt = &stopped
		}
	case state.ExitCode != 0:
		sb.Status = sandbox.StatusStopped
	default:
		sb.Status = sandbox.StatusCreated
	}
}

// Get returns the current state of a sandbox.
func (p *Provider) Get(ctx context.Context, _ []byte, sessionID string) (*sandbox.Sandbox, error) {
	containerID, err := p.getContainerID(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	info, err := p.client.ContainerInspect(ctx, containerID)
	if err != nil {
		// If the container was deleted externally, clear the stale cache entry
		if cerrdefs.IsNotFound(err) {
			p.clearContainerID(sessionID)
			return nil, sandbox.ErrNotFound
		}
		return nil, fmt.Errorf("failed to inspect sandbox: %w", err)
	}

	s := &sandbox.Sandbox{
		ID:        info.ID,
		SessionID: sessionID,
		Image:     info.Config.Image,
		Metadata: map[string]string{
			"name":                  info.Name,
			sandbox.MetadataImageID: info.Image,
		},
	}

	// Parse times
	if created, err := time.Parse(time.RFC3339Nano, info.Created); err == nil {
		s.CreatedAt = created
	}

	// Determine status
	applyContainerState(s, info.State)

	// Extract assigned port mappings
	s.Ports = p.extractPorts(info.NetworkSettings)

	// Extract environment variables
	s.Env = p.extractEnv(info.Config.Env)

	return s, nil
}

// GetSecret returns the raw shared secret stored during sandbox creation.
func (p *Provider) GetSecret(ctx context.Context, _ []byte, sessionID string) (string, error) {
	containerID, err := p.getContainerID(ctx, sessionID)
	if err != nil {
		return "", err
	}

	info, err := p.client.ContainerInspect(ctx, containerID)
	if err != nil {
		// If the container was deleted externally, clear the stale cache entry
		if cerrdefs.IsNotFound(err) {
			p.clearContainerID(sessionID)
			return "", sandbox.ErrNotFound
		}
		return "", fmt.Errorf("failed to inspect sandbox: %w", err)
	}

	secret, ok := info.Config.Labels[labelSecret]
	if !ok || secret == "" {
		return "", fmt.Errorf("shared secret not found for sandbox")
	}

	return secret, nil
}

// extractEnv parses Docker's env slice (KEY=VALUE format) into a map.
func (p *Provider) extractEnv(envSlice []string) map[string]string {
	env := make(map[string]string)
	for _, e := range envSlice {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			env[parts[0]] = parts[1]
		}
	}
	return env
}

// extractPorts extracts assigned port mappings from container network settings.
func (p *Provider) extractPorts(settings *containerTypes.NetworkSettings) []sandbox.AssignedPort {
	if settings == nil {
		return nil
	}

	var ports []sandbox.AssignedPort
	for containerPort, bindings := range settings.Ports {
		for _, binding := range bindings {
			hostPort, _ := strconv.Atoi(binding.HostPort)
			ports = append(ports, sandbox.AssignedPort{
				ContainerPort: containerPort.Int(),
				HostPort:      hostPort,
				HostIP:        binding.HostIP,
				Protocol:      containerPort.Proto(),
			})
		}
	}
	return ports
}

// wrapCommandWithSessionEnv ensures docker exec-based sessions see the same
// session environment bootstrap as the agent process. docker exec does not
// inherit environment from running processes, so we route commands through a
// small runtime wrapper that loads the shared env files first.
func wrapCommandWithSessionEnv(cmd []string, injectedEnv map[string]string) []string {
	if len(cmd) == 0 {
		return nil
	}

	wrapped := make([]string, 0, len(cmd)+4)
	wrapped = append(wrapped, sessionEnvWrapperCmd)
	if len(injectedEnv) > 0 {
		keys := make([]string, 0, len(injectedEnv))
		for key := range injectedEnv {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		wrapped = append(wrapped, "--preserve", strings.Join(keys, ","))
	}
	wrapped = append(wrapped, "--")
	wrapped = append(wrapped, cmd...)
	return wrapped
}

// detectShell determines the best available shell in the container.
// It tries shells in this order: $SHELL → /bin/bash → /bin/sh
func (p *Provider) detectShell(ctx context.Context, containerID string) []string {
	// Create a quick timeout context for shell detection
	detectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// First, try to get $SHELL from the environment
	execConfig := containerTypes.ExecOptions{
		Cmd:          []string{"sh", "-c", "echo $SHELL"},
		AttachStdout: true,
		AttachStderr: true,
	}

	execCreate, err := p.client.ContainerExecCreate(detectCtx, containerID, execConfig)
	if err == nil {
		resp, err := p.client.ContainerExecAttach(detectCtx, execCreate.ID, containerTypes.ExecStartOptions{})
		if err == nil {
			var stdout, stderr bytes.Buffer
			_, _ = stdcopy.StdCopy(&stdout, &stderr, resp.Reader)
			resp.Close()

			shell := strings.TrimSpace(stdout.String())
			if shell != "" && shell != "$SHELL" {
				// Verify the shell exists
				if p.shellExists(detectCtx, containerID, shell) {
					return []string{shell}
				}
			}
		}
	}

	// Try /bin/bash
	if p.shellExists(detectCtx, containerID, "/bin/bash") {
		return []string{"/bin/bash"}
	}

	// Fall back to /bin/sh (should always exist)
	return []string{"/bin/sh"}
}

// shellExists checks if a shell binary exists and is executable in the container.
func (p *Provider) shellExists(ctx context.Context, containerID string, shell string) bool {
	execConfig := containerTypes.ExecOptions{
		Cmd:          []string{"test", "-x", shell},
		AttachStdout: true,
		AttachStderr: true,
	}

	execCreate, err := p.client.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return false
	}

	resp, err := p.client.ContainerExecAttach(ctx, execCreate.ID, containerTypes.ExecStartOptions{})
	if err != nil {
		return false
	}
	defer resp.Close()

	// Drain output
	_, _ = io.Copy(io.Discard, resp.Reader)

	// Check exit code
	inspect, err := p.client.ContainerExecInspect(ctx, execCreate.ID)
	if err != nil {
		return false
	}

	return inspect.ExitCode == 0
}

// List returns all sandboxes managed by discobot.
func (p *Provider) List(ctx context.Context) ([]*sandbox.Sandbox, error) {
	// List all containers with our label
	containers, err := p.client.ContainerList(ctx, containerTypes.ListOptions{
		All: true, // Include stopped containers
		Filters: filters.NewArgs(
			filters.Arg("label", "discobot.managed=true"),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list sandboxes: %w", err)
	}

	result := make([]*sandbox.Sandbox, 0, len(containers))
	for _, c := range containers {
		// Extract session ID from labels
		sessionID := c.Labels["discobot.session.id"]
		if sessionID == "" {
			continue
		}

		// Get full container info
		info, err := p.client.ContainerInspect(ctx, c.ID)
		if err != nil {
			continue // Skip containers we can't inspect
		}

		sb := &sandbox.Sandbox{
			ID:        info.ID,
			SessionID: sessionID,
			Image:     info.Config.Image,
			Metadata: map[string]string{
				"name":                  info.Name,
				sandbox.MetadataImageID: c.ImageID,
			},
		}

		// Parse times
		if created, err := time.Parse(time.RFC3339Nano, info.Created); err == nil {
			sb.CreatedAt = created
		}

		// Determine status
		applyContainerState(sb, info.State)

		// Extract assigned port mappings
		sb.Ports = p.extractPorts(info.NetworkSettings)

		// Extract environment variables
		sb.Env = p.extractEnv(info.Config.Env)

		// Cache the mapping
		p.containerIDsMu.Lock()
		p.containerIDs[sessionID] = info.ID
		p.containerIDsMu.Unlock()

		result = append(result, sb)
	}

	return result, nil
}

// getContainerID retrieves the Docker container ID for a session.
func (p *Provider) getContainerID(ctx context.Context, sessionID string) (string, error) {
	p.containerIDsMu.RLock()
	containerID, exists := p.containerIDs[sessionID]
	p.containerIDsMu.RUnlock()

	if exists {
		return containerID, nil
	}

	// Try to find by name (for persistence across restarts)
	name := containerName(sessionID)
	info, err := p.client.ContainerInspect(ctx, name)
	if err != nil {
		return "", sandbox.ErrNotFound
	}

	// Cache the mapping
	p.containerIDsMu.Lock()
	p.containerIDs[sessionID] = info.ID
	p.containerIDsMu.Unlock()

	return info.ID, nil
}

// clearContainerID removes a container ID from the cache.
// This is used when a container is deleted externally.
func (p *Provider) clearContainerID(sessionID string) {
	p.containerIDsMu.Lock()
	delete(p.containerIDs, sessionID)
	p.containerIDsMu.Unlock()
	p.removeHTTPClientCaches(sessionID)
}

// Client returns the underlying Docker client.
// Used by the VZ provider for direct image operations (e.g., ImageLoad).
func (p *Provider) Client() *client.Client {
	return p.client
}

// Close closes the Docker client connection.
func (p *Provider) Close() error {
	p.stopOnce.Do(func() {
		close(p.stopCh)
	})
	return p.client.Close()
}

// dockerPTY implements sandbox.PTY for Docker exec sessions.
type dockerPTY struct {
	client    *client.Client
	execID    string
	hijacked  types.HijackedResponse
	done      chan struct{}
	doneOnce  sync.Once
	closeOnce sync.Once
}

func (p *dockerPTY) Read(b []byte) (int, error) {
	n, err := p.hijacked.Reader.Read(b)
	if err != nil {
		p.doneOnce.Do(func() { close(p.done) })
	}
	return n, err
}

func (p *dockerPTY) Write(b []byte) (int, error) {
	return p.hijacked.Conn.Write(b)
}

func (p *dockerPTY) Resize(ctx context.Context, rows, cols int) error {
	log.Printf("dockerPTY.Resize: execID=%s rows=%d cols=%d", p.execID, rows, cols)
	err := p.client.ContainerExecResize(ctx, p.execID, containerTypes.ResizeOptions{
		Height: uint(rows),
		Width:  uint(cols),
	})
	if err != nil {
		log.Printf("dockerPTY.Resize: execID=%s error: %v", p.execID, err)
	} else {
		log.Printf("dockerPTY.Resize: execID=%s success", p.execID)
	}
	return err
}

func (p *dockerPTY) Close() error {
	p.closeOnce.Do(func() {
		p.hijacked.Close()
	})
	return nil
}

func (p *dockerPTY) Wait(ctx context.Context) (int, error) {
	// Block until the hijacked stream hits EOF (exec process exited),
	// then do a single ContainerExecInspect to get the exit code.
	select {
	case <-ctx.Done():
		return -1, ctx.Err()
	case <-p.done:
	}

	inspect, err := p.client.ContainerExecInspect(ctx, p.execID)
	if err != nil {
		return -1, err
	}
	return inspect.ExitCode, nil
}

// AcquireHTTPClient returns a leased HTTP client configured to communicate with the sandbox.
func (p *Provider) AcquireHTTPClient(ctx context.Context, state []byte, sessionID string) (*sandbox.HTTPClientLease, error) {
	if baseURL, ok := p.getCachedHTTPClientTarget(sessionID); ok {
		return p.acquireHTTPClientForTarget(sessionID, baseURL)
	}

	sb, err := p.Get(ctx, state, sessionID)
	if err != nil {
		p.removeHTTPClientCaches(sessionID)
		return nil, err
	}

	if sb.Status != sandbox.StatusRunning {
		p.removeHTTPClientCaches(sessionID)
		return nil, fmt.Errorf("sandbox is not running: %s", sb.Status)
	}

	// Find the HTTP port (3002)
	var httpPort *sandbox.AssignedPort
	for i := range sb.Ports {
		if sb.Ports[i].ContainerPort == containerPort {
			httpPort = &sb.Ports[i]
			break
		}
	}
	if httpPort == nil {
		p.removeHTTPClientCaches(sessionID)
		return nil, fmt.Errorf("sandbox does not expose port %d", containerPort)
	}

	hostIP := httpPort.HostIP
	if hostIP == "" || hostIP == "0.0.0.0" {
		hostIP = "127.0.0.1"
	}

	// Create a custom transport that always dials to the sandbox's mapped port
	baseURL := fmt.Sprintf("%s:%d", hostIP, httpPort.HostPort)
	p.setCachedHTTPClientTarget(sessionID, baseURL)
	return p.acquireHTTPClientForTarget(sessionID, baseURL)
}

func (p *Provider) acquireHTTPClientForTarget(sessionID, baseURL string) (*sandbox.HTTPClientLease, error) {
	return p.httpClients.Acquire(sessionID, baseURL, func() (*http.Client, error) {
		return &http.Client{
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   100,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					// Always connect to the sandbox's mapped port, ignoring the addr from the URL.
					var d net.Dialer
					return d.DialContext(ctx, "tcp", baseURL)
				},
			},
			Timeout: 60 * time.Second,
		}, nil
	})
}

func (p *Provider) getCachedHTTPClientTarget(sessionID string) (string, bool) {
	p.httpClientTargetsMu.Lock()
	defer p.httpClientTargetsMu.Unlock()

	target, ok := p.httpClientTargets[sessionID]
	if !ok {
		return "", false
	}
	if time.Now().After(target.ExpiresAt) {
		delete(p.httpClientTargets, sessionID)
		return "", false
	}
	return target.BaseURL, true
}

func (p *Provider) setCachedHTTPClientTarget(sessionID, baseURL string) {
	p.httpClientTargetsMu.Lock()
	p.httpClientTargets[sessionID] = cachedHTTPClientTarget{
		BaseURL:   baseURL,
		ExpiresAt: time.Now().Add(httpClientTargetCacheTTL),
	}
	p.httpClientTargetsMu.Unlock()
}

func (p *Provider) removeHTTPClientCaches(sessionID string) {
	p.httpClients.Remove(sessionID)
	p.httpClientTargetsMu.Lock()
	delete(p.httpClientTargets, sessionID)
	p.httpClientTargetsMu.Unlock()
}

// Watch returns a channel that receives sandbox state change events.
// It first replays the current state of all existing sandboxes, then streams
// state changes as they occur by watching Docker events.
func (p *Provider) Watch(ctx context.Context) (<-chan sandbox.StateEvent, error) {
	eventCh := make(chan sandbox.StateEvent, 100)

	// Start a goroutine to handle the watch
	go func() {
		defer close(eventCh)

		// First, replay current state of all managed sandboxes
		sandboxes, err := p.List(ctx)
		if err != nil {
			log.Printf("Watch: failed to list sandboxes for replay: %v", err)
			// Continue anyway - we can still watch for new events
		} else {
			for _, sb := range sandboxes {
				select {
				case <-ctx.Done():
					return
				case eventCh <- sandbox.StateEvent{
					SessionID: sb.SessionID,
					Status:    sb.Status,
					Timestamp: time.Now(),
					Error:     sb.Error,
				}:
				}
			}
		}

		// Set up Docker events filter for our managed containers
		filterArgs := filters.NewArgs(
			filters.Arg("type", string(events.ContainerEventType)),
			filters.Arg("label", "discobot.managed=true"),
		)

		// Watch Docker events
		p.watchDockerEvents(ctx, eventCh, filterArgs)
	}()

	return eventCh, nil
}

// watchDockerEvents watches Docker container events and translates them to sandbox events.
// It automatically reconnects if the connection is lost.
func (p *Provider) watchDockerEvents(ctx context.Context, eventCh chan<- sandbox.StateEvent, filterArgs filters.Args) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Start watching Docker events
		msgCh, errCh := p.client.Events(ctx, events.ListOptions{
			Filters: filterArgs,
		})

		// Process events until error or context cancellation
		if !p.processDockerEvents(ctx, eventCh, msgCh, errCh) {
			return // Context cancelled or unrecoverable error
		}

		// If we get here, there was a recoverable error - wait before reconnecting
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
			log.Printf("Watch: reconnecting to Docker events...")
		}
	}
}

// processDockerEvents processes Docker events from the channels.
// Returns false if the context was cancelled (caller should exit),
// returns true if reconnection should be attempted.
func (p *Provider) processDockerEvents(ctx context.Context, eventCh chan<- sandbox.StateEvent, msgCh <-chan events.Message, errCh <-chan error) bool {
	for {
		select {
		case <-ctx.Done():
			return false

		case err := <-errCh:
			if err == nil {
				// Channel closed, reconnect
				return true
			}
			if ctx.Err() != nil {
				return false
			}
			log.Printf("Watch: Docker events error: %v, reconnecting...", err)
			return true

		case msg := <-msgCh:
			event := p.translateDockerEvent(msg)
			if event != nil {
				select {
				case <-ctx.Done():
					return false
				case eventCh <- *event:
				}
			}
		}
	}
}

// translateDockerEvent converts a Docker event to a sandbox StateEvent.
// Returns nil if the event should be ignored.
func (p *Provider) translateDockerEvent(msg events.Message) *sandbox.StateEvent {
	// Extract session ID from container labels
	sessionID := msg.Actor.Attributes["discobot.session.id"]
	if sessionID == "" {
		// Not one of our containers or missing session ID
		return nil
	}

	var status sandbox.Status
	var errMsg string

	switch msg.Action {
	case "create":
		status = sandbox.StatusCreated
	case "start":
		status = sandbox.StatusRunning
	case "stop", "kill":
		status = sandbox.StatusStopped
	case "die":
		status = sandbox.StatusStopped
	case "destroy":
		status = sandbox.StatusRemoved
		// Clear container ID from cache since it's been deleted
		p.clearContainerID(sessionID)
	case "oom":
		status = sandbox.StatusFailed
		errMsg = "out of memory"
	default:
		// Ignore other events (pause, unpause, attach, etc.)
		return nil
	}

	return &sandbox.StateEvent{
		SessionID: sessionID,
		Status:    status,
		Timestamp: time.Unix(msg.Time, msg.TimeNano),
		Error:     errMsg,
	}
}
