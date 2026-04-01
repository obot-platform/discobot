package config

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/adrg/xdg"

	"github.com/obot-platform/discobot/server/internal/version"
)

const appName = "discobot"

// GitHubOAuthClientID is the GitHub OAuth App client ID for git operations (repo scope).
// Set at build time via -ldflags "-X github.com/obot-platform/discobot/server/internal/config.GitHubOAuthClientID=..."
// Can be overridden at runtime via the GITHUB_OAUTH_CLIENT_ID environment variable.
var GitHubOAuthClientID = ""

// DefaultSandboxImage returns the default sandbox image for sessions,
// tagged with the current build version.
func DefaultSandboxImage() string {
	return "ghcr.io/obot-platform/discobot:" + version.Get()
}

// DefaultVZImage returns the default VZ image containing kernel and rootfs for VMs,
// tagged with the current build version.
func DefaultVZImage() string {
	return "ghcr.io/obot-platform/discobot-vz:" + version.Get()
}

// Config holds all configuration for the server
type Config struct {
	// Server settings
	Port               int
	CORSOrigins        []string
	CORSDebug          bool // Enable CORS debug logging (default: false)
	SuggestionsEnabled bool // Enable filesystem suggestions API (default: false)

	// Database
	DatabaseDSN    string
	DatabaseDriver string // "postgres" or "sqlite3", auto-detected from DSN

	// Authentication
	AuthEnabled bool // If false, uses anonymous user (default: false)

	// Security
	SessionSecret []byte
	EncryptionKey []byte // 32 bytes for AES-256-GCM

	// Workspaces and Git
	WorkspaceDir string // Base directory for workspaces and git cache

	// Sandbox runtime settings
	SandboxImage       string        // Default sandbox image
	SandboxIdleTimeout time.Duration // Auto-stop sandboxes after idle period
	IdleCheckInterval  time.Duration // How often to check for idle sessions

	// Docker-specific settings
	DockerHost    string // Docker socket/host (default: unix:///var/run/docker.sock)
	DockerNetwork string // Docker network to attach containers to

	// VZ-specific settings (macOS Virtualization.framework)
	VZDataDir       string // Directory for VM data (default: ./vz)
	VZConsoleLogDir string // Directory for VM console logs (default: same as VZDataDir)
	VZKernelPath    string // Path to Linux kernel (vmlinuz)
	VZInitrdPath    string // Path to initial ramdisk (optional)
	VZBaseDiskPath  string // Path to base disk image to clone (optional)
	VZImageRef      string // Docker registry image ref for auto-downloading kernel and rootfs
	VZHomeDir       string // Host directory to share with VMs via VirtioFS (default: user home dir)
	VZCPUCount      int    // Number of CPUs per VM (0 = all host CPUs)
	VZMemoryMB      int    // Memory per VM in MB (0 = half system memory, rounded down to nearest GB)
	VZDataDiskGB    int    // Data disk size per VM in GB (0 = 100GB default)

	// Local provider settings
	LocalProviderEnabled bool   // Enable local sandbox provider (default: false)
	LocalAgentBinary     string // Path to agent API binary for local provider (default: obot-agent-api in PATH)

	// SSH server settings
	SSHEnabled     bool   // Enable SSH server (default: true)
	SSHPort        int    // SSH server port (default: 3333)
	SSHHostKeyPath string // Path to SSH host key file (default: ./ssh_host_key)

	// Job Dispatcher settings
	DispatcherEnabled               bool          // Enable job dispatcher (default: true)
	DispatcherPollInterval          time.Duration // How often to poll for jobs (default: 1s)
	DispatcherHeartbeatInterval     time.Duration // Heartbeat interval for leader (default: 10s)
	DispatcherHeartbeatTimeout      time.Duration // Timeout before leader is considered dead (default: 30s)
	DispatcherJobTimeout            time.Duration // Max time for a single job (default: 5m)
	DispatcherStaleJobTimeout       time.Duration // Time after which running jobs are considered stale (default: 10m)
	DispatcherImmediateExecution    bool          // Try to execute jobs immediately when enqueued (default: true)
	DisableSessionSandboxDeleteJobs bool          // Disable execution of session_sandbox_delete jobs (default: false)
	JobRetryBackoff                 time.Duration // Base backoff between job retries, multiplied by attempt number (default: 5s)
	JobMaxAttempts                  int           // Default max attempts for jobs (default: 3)

	// OAuth providers (for user login)
	GitHubClientID     string
	GitHubClientSecret string
	GoogleClientID     string
	GoogleClientSecret string

	// AI Provider OAuth (client IDs are public for PKCE flows)
	AnthropicClientID     string
	GitHubCopilotClientID string
	CodexClientID         string

	// GitHub OAuth for git operations (device flow, repo scope)
	// Client ID is compiled in at build time via ldflags and can be overridden at runtime.
	GitHubOAuthClientID string

	// Debug settings
	DebugDocker     bool // Expose Docker API proxy for VZ VMs (default: false)
	DebugDockerPort int  // Port for debug Docker proxy (default: 2375)

	// Process lifecycle
	LogFile        string // Redirect stdout/stderr to this file (Unix only)
	StdinKeepalive bool   // Exit when stdin is closed (for parent process death detection)

	// MCP OAuth settings (injected into agent containers)
	MCPOAuthRedirectBase string // Base URL for MCP OAuth callbacks (MCP_OAUTH_REDIRECT_BASE)
	AgentServerURL       string // URL the agent uses to reach this server (AGENT_SERVER_URL)

	// Tauri mode settings
	TauriMode   bool   // Running inside Tauri app (TAURI=true)
	TauriSecret string // Shared secret for Tauri auth (DISCOBOT_SECRET)
}

// Load reads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{}

	// Server
	cfg.Port = getEnvInt("PORT", 3001)
	cfg.CORSOrigins = getEnvList("CORS_ORIGINS", []string{"http://*.localhost:3001", "http://localhost:3000", "http://*.localhost:3000", "http://localhost:3100", "http://*.localhost:3100"})
	cfg.CORSDebug = getEnvBool("CORS_DEBUG", false)
	cfg.SuggestionsEnabled = getEnvBool("SUGGESTIONS_ENABLED", false)

	// Database - defaults to XDG_DATA_HOME/discobot/discobot.db
	cfg.DatabaseDSN = getEnv("DATABASE_DSN", "sqlite3://"+filepath.Join(xdg.DataHome, appName, "discobot.db"))
	cfg.DatabaseDriver = detectDriver(cfg.DatabaseDSN)

	// Authentication - defaults to disabled (anonymous user mode)
	cfg.AuthEnabled = getEnvBool("AUTH_ENABLED", false)

	// Security - Session secret (required only if auth is enabled)
	sessionSecret := getEnv("SESSION_SECRET", "")
	if sessionSecret == "" {
		if cfg.AuthEnabled {
			return nil, fmt.Errorf("SESSION_SECRET is required when AUTH_ENABLED=true")
		}
		// Use a default for no-auth mode (sessions still work but aren't secure)
		sessionSecret = "discobot-dev-session-secret-not-for-production"
	}
	cfg.SessionSecret = []byte(sessionSecret)

	// Security - Encryption key (32 bytes for AES-256)
	encryptionKeyStr := getEnv("ENCRYPTION_KEY", "")
	if encryptionKeyStr == "" {
		if cfg.AuthEnabled {
			return nil, fmt.Errorf("ENCRYPTION_KEY is required when AUTH_ENABLED=true (32 bytes, hex encoded)")
		}
		// Use a default for no-auth mode (credentials still encrypted but key isn't secure)
		encryptionKeyStr = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	}
	encryptionKey, err := hex.DecodeString(encryptionKeyStr)
	if err != nil {
		return nil, fmt.Errorf("ENCRYPTION_KEY must be hex encoded: %w", err)
	}
	if len(encryptionKey) != 32 {
		return nil, fmt.Errorf("ENCRYPTION_KEY must be exactly 32 bytes (64 hex chars), got %d bytes", len(encryptionKey))
	}
	cfg.EncryptionKey = encryptionKey

	// Workspaces and Git - defaults to XDG_DATA_HOME/discobot/workspaces
	cfg.WorkspaceDir = getEnv("WORKSPACE_DIR", filepath.Join(xdg.DataHome, appName, "workspaces"))

	// Sandbox runtime settings
	cfg.SandboxImage = getEnv("SANDBOX_IMAGE", DefaultSandboxImage())
	cfg.SandboxIdleTimeout = getEnvDuration("SANDBOX_IDLE_TIMEOUT", 1*time.Hour)
	cfg.IdleCheckInterval = getEnvDuration("IDLE_CHECK_INTERVAL", 5*time.Minute)

	// Docker-specific settings
	// Empty default lets the Docker SDK auto-detect (works on Linux, macOS, and Windows)
	cfg.DockerHost = getEnv("DOCKER_HOST", "")
	cfg.DockerNetwork = getEnv("DOCKER_NETWORK", "")

	// VZ-specific settings (macOS Virtualization.framework)
	// VZ state defaults to XDG_STATE_HOME/discobot/vz
	cfg.VZDataDir = getEnv("VZ_DATA_DIR", filepath.Join(xdg.StateHome, appName, "vz"))
	cfg.VZConsoleLogDir = getEnv("VZ_CONSOLE_LOG_DIR", cfg.VZDataDir) // Default to same as VZDataDir
	cfg.VZKernelPath = getEnv("VZ_KERNEL_PATH", "")
	cfg.VZInitrdPath = getEnv("VZ_INITRD_PATH", "")
	cfg.VZBaseDiskPath = getEnv("VZ_BASE_DISK_PATH", "")
	cfg.VZImageRef = getEnv("VZ_IMAGE_REF", DefaultVZImage())
	homeDir, _ := os.UserHomeDir()
	cfg.VZHomeDir = getEnv("VZ_HOME_DIR", homeDir)
	cfg.VZCPUCount = getEnvInt("VZ_CPU_COUNT", 0)
	cfg.VZMemoryMB = getEnvInt("VZ_MEMORY_MB", 0)
	cfg.VZDataDiskGB = getEnvInt("VZ_DATA_DISK_GB", 0)

	// Local provider settings
	cfg.LocalProviderEnabled = getEnvBool("LOCAL_PROVIDER_ENABLED", false)
	cfg.LocalAgentBinary = getEnv("LOCAL_AGENT_BINARY", "obot-agent-api")

	// SSH server settings
	// SSH host key defaults to XDG_STATE_HOME/discobot/ssh_host_key
	cfg.SSHEnabled = getEnvBool("SSH_ENABLED", true)
	cfg.SSHPort = getEnvInt("SSH_PORT", 3333)
	cfg.SSHHostKeyPath = getEnv("SSH_HOST_KEY_PATH", filepath.Join(xdg.StateHome, appName, "ssh_host_key"))

	// Job Dispatcher settings
	cfg.DispatcherEnabled = getEnvBool("DISPATCHER_ENABLED", true)
	cfg.DispatcherPollInterval = getEnvDuration("DISPATCHER_POLL_INTERVAL", 5*time.Second)
	cfg.DispatcherHeartbeatInterval = getEnvDuration("DISPATCHER_HEARTBEAT_INTERVAL", 10*time.Second)
	cfg.DispatcherHeartbeatTimeout = getEnvDuration("DISPATCHER_HEARTBEAT_TIMEOUT", 30*time.Second)
	cfg.DispatcherJobTimeout = getEnvDuration("DISPATCHER_JOB_TIMEOUT", 20*time.Minute)
	cfg.DispatcherStaleJobTimeout = getEnvDuration("DISPATCHER_STALE_JOB_TIMEOUT", 10*time.Minute)
	cfg.DispatcherImmediateExecution = getEnvBool("DISPATCHER_IMMEDIATE_EXECUTION", true)
	cfg.DisableSessionSandboxDeleteJobs = getEnvBool("DISABLE_SESSION_SANDBOX_DELETE_JOBS", false)
	cfg.JobRetryBackoff = getEnvDuration("JOB_RETRY_BACKOFF", 5*time.Second)
	cfg.JobMaxAttempts = getEnvInt("JOB_MAX_ATTEMPTS", 3)

	// OAuth providers for user login
	cfg.GitHubClientID = getEnv("GITHUB_CLIENT_ID", "")
	cfg.GitHubClientSecret = getEnv("GITHUB_CLIENT_SECRET", "")
	cfg.GoogleClientID = getEnv("GOOGLE_CLIENT_ID", "")
	cfg.GoogleClientSecret = getEnv("GOOGLE_CLIENT_SECRET", "")

	// AI Provider OAuth client IDs (public, used in PKCE flows)
	cfg.AnthropicClientID = getEnv("ANTHROPIC_CLIENT_ID", "9d1c250a-e61b-44d9-88ed-5944d1962f5e")
	cfg.GitHubCopilotClientID = getEnv("GITHUB_COPILOT_CLIENT_ID", "Iv1.b507a08c87ecfe98")
	cfg.CodexClientID = getEnv("GITHUB_CODEX_CLIENT_ID", "app_EMoamEEZ73f0CkXaXp7hrann")
	cfg.GitHubOAuthClientID = getEnv("GITHUB_OAUTH_CLIENT_ID", GitHubOAuthClientID)

	// Debug settings
	cfg.DebugDocker = getEnvBool("DEBUG_DOCKER", false)
	cfg.DebugDockerPort = getEnvInt("DEBUG_DOCKER_PORT", 2375)

	// Process lifecycle
	cfg.LogFile = getEnv("LOG_FILE", "")
	cfg.StdinKeepalive = getEnvBool("STDIN_KEEPALIVE", false)

	// MCP OAuth — default to http://127.0.0.1:{Port} so containers can always reach
	// the server and receive OAuth callbacks without explicit env var configuration.
	cfg.MCPOAuthRedirectBase = getEnv("MCP_OAUTH_REDIRECT_BASE", fmt.Sprintf("http://127.0.0.1:%d", cfg.Port))
	cfg.AgentServerURL = getEnv("AGENT_SERVER_URL", fmt.Sprintf("http://127.0.0.1:%d", cfg.Port))

	// Tauri mode settings
	cfg.TauriMode = getEnvBool("TAURI", false)
	cfg.TauriSecret = getEnv("DISCOBOT_SECRET", "")
	if cfg.TauriMode && cfg.TauriSecret == "" {
		return nil, fmt.Errorf("DISCOBOT_SECRET is required when TAURI=true")
	}

	return cfg, nil
}

// detectDriver determines the database driver from DSN
func detectDriver(dsn string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		return "postgres"
	}
	if strings.HasPrefix(dsn, "sqlite3://") || strings.HasPrefix(dsn, "sqlite://") {
		return "sqlite"
	}
	// Default to sqlite for file paths
	if strings.HasSuffix(dsn, ".db") || strings.HasSuffix(dsn, ".sqlite") {
		return "sqlite"
	}
	return "postgres"
}

// CleanDSN removes the driver prefix from DSN for database/sql
func (c *Config) CleanDSN() string {
	dsn := c.DatabaseDSN
	dsn = strings.TrimPrefix(dsn, "postgres://")
	dsn = strings.TrimPrefix(dsn, "postgresql://")
	dsn = strings.TrimPrefix(dsn, "sqlite3://")
	dsn = strings.TrimPrefix(dsn, "sqlite://")

	// For postgres, add the prefix back
	if c.DatabaseDriver == "postgres" {
		return "postgres://" + dsn
	}
	return dsn
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
	}
	return defaultValue
}

func getEnvList(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		return strings.Split(value, ",")
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if d, err := time.ParseDuration(value); err == nil {
			return d
		}
	}
	return defaultValue
}
