// Package sandbox provides an abstraction for sandbox execution environments.
// It supports multiple backends including Docker, Kubernetes, and Cloudflare sandboxes.
package sandbox

import (
	"context"
	"io"
	"net/http"
	"time"
)

// Provider abstracts sandbox execution environments (Docker, K8s, Cloudflare, etc.)
// Each session gets one dedicated sandbox, managed through this interface.
type Provider interface {
	// ImageExists checks if the configured sandbox image is available locally.
	// Returns true if the image exists, false if it needs to be pulled.
	ImageExists(ctx context.Context) bool

	// Image returns the configured sandbox image name.
	Image() string

	// Create creates a new sandbox for the given session.
	// The sandbox is created but not started.
	// A single port (3002) is always exposed and assigned a random host port.
	// If the image doesn't exist locally, it will be pulled automatically.
	Create(ctx context.Context, sessionID string, opts CreateOptions) (*Sandbox, error)

	// Start starts a previously created sandbox.
	Start(ctx context.Context, sessionID string) error

	// Stop stops a running sandbox gracefully.
	// The timeout specifies how long to wait before force-killing.
	Stop(ctx context.Context, sessionID string, timeout time.Duration) error

	// Remove removes a sandbox and optionally its associated data volumes.
	// The sandbox must be stopped first.
	// By default, data volumes are preserved (useful for rebuilds).
	// Pass RemoveVolumes() to delete all data volumes (for session deletion).
	Remove(ctx context.Context, sessionID string, opts ...RemoveOption) error

	// Get returns the current state of a sandbox.
	Get(ctx context.Context, sessionID string) (*Sandbox, error)

	// GetSecret returns the shared secret for the sandbox.
	// This is the raw secret stored during creation, not the hashed version.
	GetSecret(ctx context.Context, sessionID string) (string, error)

	// List returns all sandboxes managed by discobot.
	// This includes sandboxes in any state (running, stopped, failed).
	List(ctx context.Context) ([]*Sandbox, error)

	// Exec runs a non-interactive command in the sandbox.
	// Returns stdout, stderr, and exit code.
	Exec(ctx context.Context, sessionID string, cmd []string, opts ExecOptions) (*ExecResult, error)

	// Attach creates an interactive PTY session to the sandbox.
	// The PTY can be used for bidirectional terminal communication.
	Attach(ctx context.Context, sessionID string, opts AttachOptions) (PTY, error)

	// ExecStream runs a command with bidirectional streaming I/O (no TTY).
	// Unlike Exec, this doesn't buffer output - it provides direct streaming access.
	// Unlike Attach, this doesn't allocate a PTY, so binary data is not corrupted.
	// This is used for SFTP and port forwarding.
	ExecStream(ctx context.Context, sessionID string, cmd []string, opts ExecStreamOptions) (Stream, error)

	// HTTPClient returns an HTTP client configured to communicate with the sandbox.
	// The client handles the transport layer (TCP for Docker, vsock for vz, etc.).
	// The returned client connects to the sandbox's HTTP server (port 3002).
	HTTPClient(ctx context.Context, sessionID string) (*http.Client, error)

	// Watch returns a channel that receives sandbox state change events.
	// On subscription, it replays the current state of all existing sandboxes,
	// then streams state changes as they occur.
	//
	// The channel is closed when the context is cancelled or when an
	// unrecoverable error occurs. Callers should watch for channel closure.
	//
	// Events include: created, running, stopped, failed, removed.
	// The "removed" status indicates a sandbox was deleted (possibly externally).
	//
	// For Docker, this watches the Docker events API for container lifecycle events.
	// For VZ, this uses the VM state change notifications.
	Watch(ctx context.Context) (<-chan StateEvent, error)

	// Reconcile performs provider-specific reconciliation on startup.
	// This handles tasks like cleaning up old images, removing outdated
	// infrastructure containers (e.g., BuildKit), and other housekeeping.
	// Called after sandbox image reconciliation completes.
	Reconcile(ctx context.Context) error

	// RemoveProject cleans up all provider-managed resources for a project.
	// This includes cache volumes, BuildKit containers, networks, etc.
	// Called when a project is deleted.
	RemoveProject(ctx context.Context, projectID string) error
}

const MetadataImageID = "image_id"

// CurrentImageIDProvider is an optional provider capability that returns the
// immutable image ID for the configured sandbox image.
type CurrentImageIDProvider interface {
	CurrentImageID(ctx context.Context) (string, error)
}

// CleanupUnusedImagesProvider is an optional provider capability for cleaning up
// sandbox images that are no longer referenced after reconciliation.
type CleanupUnusedImagesProvider interface {
	CleanupUnusedImages(ctx context.Context) error
}

// DockerProxyProvider is an optional interface that sandbox providers can implement
// to expose the Docker daemon for debugging. This is used by the debug Docker proxy
// to forward Docker API requests to the sandbox runtime (e.g., inside a VZ VM).
type DockerProxyProvider interface {
	// DockerTransport returns an http.RoundTripper that communicates with the Docker
	// daemon for the given project. Returns an error if the project VM doesn't exist.
	DockerTransport(projectID string) (http.RoundTripper, error)
}

// ProviderStatus represents the current status of a sandbox provider.
type ProviderStatus struct {
	Available bool   `json:"available"`
	State     string `json:"state"` // "ready", "downloading", "failed", "not_available"
	Message   string `json:"message,omitempty"`
	// Details contains provider-specific status information (e.g., download progress, config).
	Details any `json:"details,omitempty"`
}

// StatusProvider is an optional interface that sandbox providers can implement
// to report their status. Providers that don't implement this are assumed ready.
type StatusProvider interface {
	Status() ProviderStatus
}

// RemoveOption configures sandbox removal behavior.
type RemoveOption func(*RemoveConfig)

// RemoveConfig holds the parsed remove options.
type RemoveConfig struct {
	RemoveVolumes bool
}

// RemoveVolumes returns an option that enables volume deletion during removal.
// By default, volumes are preserved. Use this option for session deletion.
func RemoveVolumes() RemoveOption {
	return func(cfg *RemoveConfig) {
		cfg.RemoveVolumes = true
	}
}

// ParseRemoveOptions parses remove options with defaults.
// This is exported for provider implementations to use.
func ParseRemoveOptions(opts []RemoveOption) RemoveConfig {
	cfg := RemoveConfig{
		RemoveVolumes: false, // Default: preserve volumes
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// Sandbox represents a running or stopped sandbox instance.
type Sandbox struct {
	ID        string            // Runtime-specific sandbox ID
	SessionID string            // Discobot session ID (1:1 mapping)
	Status    Status            // created, running, stopped, failed
	Image     string            // Sandbox image used
	CreatedAt time.Time         // When the sandbox was created
	StartedAt *time.Time        // When the sandbox was started (nil if never started)
	StoppedAt *time.Time        // When the sandbox was stopped (nil if still running)
	Error     string            // Error message if status == failed
	Metadata  map[string]string // Runtime-specific metadata
	Ports     []AssignedPort    // Assigned port mappings after sandbox creation
	Env       map[string]string // Environment variables set on the sandbox
}

// AssignedPort represents a port mapping that was assigned after sandbox creation.
type AssignedPort struct {
	ContainerPort int    // Port inside the sandbox
	HostPort      int    // Actual port assigned on the host
	HostIP        string // Host IP address (typically "0.0.0.0" or "127.0.0.1")
	Protocol      string // Protocol: "tcp" or "udp"
}

// Status represents the current state of a sandbox.
type Status string

const (
	StatusCreated Status = "created" // Sandbox exists but not started
	StatusRunning Status = "running" // Sandbox is running
	StatusStopped Status = "stopped" // Sandbox has stopped
	StatusFailed  Status = "failed"  // Sandbox failed to start or crashed
)

// StateEvent represents a sandbox state change event.
// These events are emitted when sandboxes are created, started, stopped, or removed.
type StateEvent struct {
	SessionID string    // The session ID associated with the sandbox
	Status    Status    // The new status (or StatusRemoved for deletion)
	Timestamp time.Time // When the event occurred
	Error     string    // Error message if status is StatusFailed
}

// StateEventType indicates what kind of state change occurred.
// This is used to distinguish between a sandbox being removed vs just stopped.
const (
	// StatusRemoved is a pseudo-status indicating the sandbox was deleted.
	// This is only used in StateEvent, not in Sandbox.Status.
	StatusRemoved Status = "removed"
)

// SSHKeyProvision contains SSH key material that should be staged into a sandbox.
// The private key must only be used for runtime provisioning and must never be
// exposed via environment variables.
type SSHKeyProvision struct {
	Filename   string
	PrivateKey string
	PublicKey  string
	Algorithm  string
}

// CreateOptions configures sandbox creation.
// Note: The sandbox image is configured globally via SANDBOX_IMAGE env var,
// not per-sandbox. The provider uses its configured image for all sandboxes.
type CreateOptions struct {
	Labels map[string]string // Sandbox labels/tags for identification

	// SharedSecret is the secret used for authenticating requests to the sandbox.
	// The provider stores this secret and makes a salted+hashed version available
	// to the sandbox via the DISCOBOT_SECRET environment variable.
	SharedSecret string

	// SSHKey contains optional SSH identity material to provision into the sandbox.
	SSHKey *SSHKeyProvision

	// WorkspacePath is the local directory to mount inside the sandbox at /.workspace.
	// This is always a local directory path (either a local workspace or a cloned git repo).
	// Sets WORKSPACE_ORIGIN_PATH env var to /.workspace (the mount point).
	WorkspacePath string

	// WorkspaceSource is the original workspace source (local path or git URL).
	// Sets WORKSPACE_SOURCE env var to this value.
	// For local workspaces, this is the local directory path.
	// For git workspaces, this is the git URL (e.g., https://github.com/user/repo.git).
	WorkspaceSource string

	// WorkspaceCommit is the git commit to checkout (optional).
	// Set as WORKSPACE_COMMIT environment variable.
	WorkspaceCommit string

	// ProjectID is the project this session belongs to.
	// Set as DISCOBOT_PROJECT_ID environment variable (used by agent for MCP token persistence).
	ProjectID string

	// MCPOAuthRedirectBase is the base URL for MCP OAuth callbacks.
	// Set as DISCOBOT_MCP_OAUTH_REDIRECT_BASE environment variable.
	MCPOAuthRedirectBase string

	// AgentServerURL is the URL the agent uses to reach the Discobot server
	// (e.g. for posting MCP tokens after OAuth). Set as DISCOBOT_SERVER_URL.
	AgentServerURL string

	// Resources defines resource limits for the sandbox.
	Resources ResourceConfig
}

// ResourceConfig defines resource limits for the sandbox.
type ResourceConfig struct {
	MemoryMB int           // Memory limit in MB (0 = no limit)
	CPUCores float64       // CPU cores (0 = no limit)
	DiskMB   int           // Disk space in MB (0 = no limit)
	Timeout  time.Duration // Max sandbox lifetime (0 = no limit)
}

// ExecOptions configures non-interactive command execution.
type ExecOptions struct {
	WorkDir string            // Working directory for command
	Env     map[string]string // Additional environment variables
	User    string            // User to run as (empty = default)
	Stdin   io.Reader         // Optional stdin input
}

// ExecResult contains the result of a non-interactive command execution.
type ExecResult struct {
	ExitCode int    // Exit code of the command
	Stdout   []byte // Standard output
	Stderr   []byte // Standard error
}

// AttachOptions configures interactive PTY session creation.
type AttachOptions struct {
	Cmd  []string          // Command to run (empty = default shell)
	Rows int               // Terminal rows
	Cols int               // Terminal columns
	Env  map[string]string // Additional environment variables
	User string            // User to run as (empty = default sandbox user)
}

// PTY represents an interactive terminal session to a sandbox.
// It implements io.ReadWriteCloser for terminal I/O.
type PTY interface {
	// Read reads output from the PTY.
	// Implements io.Reader.
	Read(p []byte) (n int, err error)

	// Write sends input to the PTY.
	// Implements io.Writer.
	Write(p []byte) (n int, err error)

	// Resize changes the terminal dimensions.
	Resize(ctx context.Context, rows, cols int) error

	// Close terminates the PTY session.
	// Implements io.Closer.
	Close() error

	// Wait blocks until the PTY command exits and returns the exit code.
	// The context can be used to cancel the wait.
	Wait(ctx context.Context) (int, error)
}

// ExecStreamOptions configures streaming command execution.
type ExecStreamOptions struct {
	WorkDir string            // Working directory for command
	Env     map[string]string // Additional environment variables
	User    string            // User to run as (empty = default)
	TTY     bool              // Allocate a pseudo-terminal for the command
}

// Stream represents a bidirectional stream to a command.
// When created with TTY=true, stdout and stderr are merged and
// Resize can be used to change terminal dimensions.
// This is used for SFTP, port forwarding, and exec.
type Stream interface {
	// Read reads stdout from the command.
	// Implements io.Reader.
	Read(p []byte) (n int, err error)

	// Stderr returns a reader for the command's stderr.
	// Returns nil if stderr is not available (e.g., TTY mode merges streams).
	Stderr() io.Reader

	// Write sends input to the command's stdin.
	// Implements io.Writer.
	Write(p []byte) (n int, err error)

	// Resize changes the terminal dimensions.
	// Only effective when the stream was created with TTY=true.
	Resize(ctx context.Context, rows, cols int) error

	// CloseWrite signals EOF to the command's stdin.
	// The stream can still be read after calling this.
	CloseWrite() error

	// Close terminates the stream and the command.
	// Implements io.Closer.
	Close() error

	// Wait blocks until the command exits and returns the exit code.
	// The context can be used to cancel the wait.
	Wait(ctx context.Context) (int, error)
}
