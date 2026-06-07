package config

import (
	"encoding/hex"
	"fmt"
	"net/http"
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

// DefaultWSLImage returns the default WSL image containing the managed distro
// rootfs archive, tagged with the current build version.
func DefaultWSLImage() string {
	return "ghcr.io/obot-platform/discobot-wsl:" + version.Get()
}

// Config holds all configuration for the server
type Config struct {
	// Server settings
	Port               int
	HTTPSPort          int
	HTTPSTLSMode       string
	HTTPSTLSCertFile   string
	HTTPSTLSKeyFile    string
	HTTPSTLSHosts      []string
	HTTPSACMEEmail     string
	CORSOrigins        []string
	CORSDebug          bool // Enable CORS debug logging (default: false)
	SuggestionsEnabled bool // Enable filesystem suggestions API (default: false)

	// Database
	DatabaseDSN    string
	DatabaseDriver string // "postgres" or "sqlite3", auto-detected from DSN

	// Authentication
	AuthEnabled bool // If false, uses anonymous user (default: false)

	// Security
	EncryptionKey      []byte // 32 bytes for AES-256-GCM
	AuthCookieSameSite string

	// Public server address used in external redirects
	PublicHostname string

	// Workspaces and Git
	WorkspaceDir string // Base directory for workspaces and git cache

	// Sandbox runtime settings
	SandboxImage               string        // Default sandbox image for local runtimes
	SandboxImageRemote         string        // Default remotely-pullable sandbox image for remote runtimes
	SandboxProvider            string        // Default sandbox provider override
	SandboxDNS                 []string      // Optional DNS resolvers for sandbox containers (SANDBOX_DNS, comma-separated)
	SandboxIdleTimeout         time.Duration // Auto-stop sandboxes after idle period
	IdleCheckInterval          time.Duration // How often to check for idle sessions
	ThreadStatusSyncInterval   time.Duration // How often to poll non-terminal session thread summaries
	SessionSandboxCleanupDelay time.Duration // Delay before removing a stopped sandbox after session deletion

	// Docker-specific settings
	DockerHost      string // Docker socket/host (default: unix:///var/run/docker.sock)
	DockerNetwork   string // Docker network to attach containers to
	DockerWSLDistro string // Windows WSL distro to proxy host Docker access through

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

	// WSL-specific settings (Windows Subsystem for Linux)
	WSLDistroName    string        // Managed WSL distro name
	WSLInstallDir    string        // Directory where the distro is imported and stored
	WSLStateDir      string        // Directory for WSL runtime state and metadata
	WSLVarDiskPath   string        // Path to the persistent /var VHDX mounted into WSL
	WSLVarDiskSizeGB int           // Size of the persistent /var VHDX in GB when created
	WSLRootfsPath    string        // Path to a local WSL rootfs archive (optional, preferred over WSLImageRef)
	WSLImageRef      string        // Docker registry image ref for WSL rootfs downloads
	WSLBridgePort    int           // TCP bridge port (0 = random)
	WSLIdleTimeout   time.Duration // How long to keep the distro running when idle (0 = never auto-stop)

	// Local provider settings
	LocalProviderEnabled bool   // Enable local sandbox provider (default: false)
	LocalAgentBinary     string // Path to agent API binary for local provider (default: obot-agent-api in PATH)

	// SSH server settings
	SSHEnabled     bool   // Enable SSH server (default: true)
	SSHPort        int    // SSH server port (default: 3333)
	SSHHostKeyPath string // Path to SSH host key file (default: ./ssh_host_key)

	// Job Dispatcher settings
	DispatcherEnabled            bool          // Enable job dispatcher (default: true)
	DispatcherPollInterval       time.Duration // How often to poll for jobs (default: 1s)
	DispatcherHeartbeatInterval  time.Duration // Heartbeat interval for leader (default: 10s)
	DispatcherHeartbeatTimeout   time.Duration // Timeout before leader is considered dead (default: 30s)
	DispatcherJobTimeout         time.Duration // Max time for a single job (default: 5m)
	DispatcherStaleJobTimeout    time.Duration // Time after which running jobs are considered stale (default: 10m)
	DispatcherImmediateExecution bool          // Try to execute jobs immediately when enqueued (default: true)
	JobRetryBackoff              time.Duration // Base backoff between job retries, multiplied by attempt number (default: 5s)
	JobMaxAttempts               int           // Default max attempts for jobs (default: 3)

	// OIDC provider (for Discobot login)
	OIDCIssuerURL          string
	OIDCBackchannelBaseURL string
	OIDCClientID           string
	OIDCClientSecret       string
	OIDCScopes             []string

	// AI Provider OAuth (client IDs are public for PKCE flows)
	AnthropicClientID     string
	GitHubCopilotClientID string
	CodexClientID         string

	// GitHub OAuth for git operations (device flow, repo scope)
	// Client ID is compiled in at build time via ldflags and can be overridden at runtime.
	GitHubOAuthClientID string
	// Client secret is required for the web redirect flow.
	GitHubOAuthClientSecret string

	// Debug settings
	DebugDocker     bool // Expose Docker API proxy for VZ VMs (default: false)
	DebugDockerPort int  // Port for debug Docker proxy (default: 2375)

	// Process lifecycle
	LogFile        string // Redirect stdout/stderr to this file (Unix only)
	ServerLogPath  string // Path read by support info and used by development log teeing
	StdinKeepalive bool   // Exit when stdin is closed (for parent process death detection)

	// MCP OAuth settings (injected into agent containers)
	MCPOAuthRedirectBase string // Base URL for MCP OAuth callbacks (MCP_OAUTH_REDIRECT_BASE)
	AgentServerURL       string // URL the agent uses to reach this server (AGENT_SERVER_URL)
	ValidateAPIKeys      bool   // Validate provider API keys when saving secret credentials (VALIDATE_API_KEYS)

	// Desktop shell settings
	DesktopMode     bool   // Running inside a desktop shell
	DesktopRuntime  string // Desktop shell runtime (electron)
	DesktopSecret   string // Shared secret for desktop shell auth
	DesktopIconPath string // Path to the desktop app icon, when available
}

// Load reads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{}

	// Server
	cfg.Port = getEnvInt("PORT", 3001)
	cfg.HTTPSPort = getEnvInt("HTTPS_PORT", 0)
	cfg.HTTPSTLSMode = strings.ToLower(getEnv("HTTPS_TLS_MODE", "ephemeral"))
	cfg.HTTPSTLSCertFile = getEnv("HTTPS_TLS_CERT_FILE", "")
	cfg.HTTPSTLSKeyFile = getEnv("HTTPS_TLS_KEY_FILE", "")
	cfg.HTTPSTLSHosts = getEnvList("HTTPS_TLS_HOSTS", []string{"localhost"})
	cfg.HTTPSACMEEmail = getEnv("HTTPS_ACME_EMAIL", "")
	cfg.HTTPSTLSHosts = compactStrings(cfg.HTTPSTLSHosts)
	cfg.CORSOrigins = loadCORSOrigins(cfg.Port, cfg.HTTPSPort, cfg.HTTPSTLSHosts)
	cfg.CORSDebug = getEnvBool("CORS_DEBUG", false)
	cfg.SuggestionsEnabled = getEnvBool("SUGGESTIONS_ENABLED", false)

	if cfg.HTTPSPort == cfg.Port && cfg.HTTPSPort > 0 {
		return nil, fmt.Errorf("HTTPS_PORT must be different from PORT")
	}
	switch cfg.HTTPSTLSMode {
	case "", "ephemeral":
		cfg.HTTPSTLSMode = "ephemeral"
	case "static":
		if cfg.HTTPSPort > 0 && (cfg.HTTPSTLSCertFile == "" || cfg.HTTPSTLSKeyFile == "") {
			return nil, fmt.Errorf("HTTPS_TLS_CERT_FILE and HTTPS_TLS_KEY_FILE are required when HTTPS_TLS_MODE=static")
		}
	case "acme":
		if cfg.HTTPSPort > 0 && len(cfg.HTTPSTLSHosts) == 0 {
			return nil, fmt.Errorf("HTTPS_TLS_HOSTS is required when HTTPS_TLS_MODE=acme")
		}
	default:
		return nil, fmt.Errorf("HTTPS_TLS_MODE must be one of: ephemeral, static, acme")
	}

	// Database - defaults to XDG_DATA_HOME/discobot/discobot.db
	cfg.DatabaseDSN = getEnv("DATABASE_DSN", "sqlite3://"+filepath.Join(xdg.DataHome, appName, "discobot.db"))
	cfg.DatabaseDriver = detectDriver(cfg.DatabaseDSN)

	// Authentication - defaults to disabled (anonymous user mode)
	cfg.AuthEnabled = getEnvBool("AUTH_ENABLED", false)

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
	cfg.AuthCookieSameSite = strings.ToLower(getEnv("AUTH_COOKIE_SAMESITE", "lax"))

	// Public address
	cfg.PublicHostname = getEnv("PUBLIC_HOSTNAME", "")

	// Workspaces and Git - defaults to XDG_DATA_HOME/discobot/workspaces
	cfg.WorkspaceDir = getEnv("WORKSPACE_DIR", filepath.Join(xdg.DataHome, appName, "workspaces"))

	// Sandbox runtime settings
	cfg.SandboxImage = getEnv("SANDBOX_IMAGE", DefaultSandboxImage())
	cfg.SandboxImageRemote = getEnv("SANDBOX_IMAGE_REMOTE", "")
	cfg.SandboxProvider = getEnv("SANDBOX_PROVIDER", "")
	cfg.SandboxDNS = getEnvList("SANDBOX_DNS", nil)
	cfg.SandboxIdleTimeout = getEnvDuration("SANDBOX_IDLE_TIMEOUT", 1*time.Hour)
	cfg.IdleCheckInterval = getEnvDuration("IDLE_CHECK_INTERVAL", 5*time.Minute)
	cfg.ThreadStatusSyncInterval = getEnvDuration("THREAD_STATUS_SYNC_INTERVAL", 10*time.Second)
	cfg.SessionSandboxCleanupDelay = getEnvDuration("SESSION_SANDBOX_CLEANUP_DELAY", 1*time.Minute)

	// Docker-specific settings
	// Empty default lets the Docker SDK auto-detect (works on Linux, macOS, and Windows)
	cfg.DockerHost = getEnv("DOCKER_HOST", "")
	cfg.DockerNetwork = getEnv("DOCKER_NETWORK", "")
	cfg.DockerWSLDistro = getEnv("DISCOBOT_DOCKER_WSL_DISTRO", "")

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

	// WSL-specific settings (Windows Subsystem for Linux)
	// WSL state defaults to XDG_STATE_HOME/discobot/wsl so development builds keep
	// all runtime-managed state under the same application directory structure.
	cfg.WSLDistroName = getEnv("WSL_DISTRO_NAME", "Discobot")
	cfg.WSLInstallDir = getEnv("WSL_INSTALL_DIR", filepath.Join(xdg.StateHome, appName, "wsl", "distro"))
	cfg.WSLStateDir = getEnv("WSL_STATE_DIR", filepath.Join(xdg.StateHome, appName, "wsl"))
	cfg.WSLVarDiskPath = getEnv("WSL_VAR_DISK_PATH", filepath.Join(xdg.StateHome, appName, "wsl", "var.vhdx"))
	cfg.WSLVarDiskSizeGB = getEnvInt("WSL_VAR_DISK_SIZE_GB", 100)
	cfg.WSLRootfsPath = getEnv("WSL_ROOTFS_ARCHIVE_PATH", "")
	cfg.WSLImageRef = getEnv("WSL_IMAGE_REF", DefaultWSLImage())
	cfg.WSLBridgePort = getEnvInt("WSL_BRIDGE_PORT", 0)
	cfg.WSLIdleTimeout = getEnvDuration("WSL_IDLE_TIMEOUT", 0)

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
	cfg.JobRetryBackoff = getEnvDuration("JOB_RETRY_BACKOFF", 5*time.Second)
	cfg.JobMaxAttempts = getEnvInt("JOB_MAX_ATTEMPTS", 3)

	cfg.OIDCIssuerURL = getEnv("OIDC_ISSUER_URL", "")
	cfg.OIDCBackchannelBaseURL = getEnv("OIDC_BACKCHANNEL_BASE_URL", "")
	cfg.OIDCClientID = getEnv("OIDC_CLIENT_ID", "")
	cfg.OIDCClientSecret = getEnv("OIDC_CLIENT_SECRET", "")
	cfg.OIDCScopes = getEnvList("OIDC_SCOPES", []string{"openid", "email", "profile"})

	// AI Provider OAuth client IDs (public, used in PKCE flows)
	cfg.AnthropicClientID = getEnv("ANTHROPIC_CLIENT_ID", "9d1c250a-e61b-44d9-88ed-5944d1962f5e")
	cfg.GitHubCopilotClientID = getEnv("GITHUB_COPILOT_CLIENT_ID", "Iv1.b507a08c87ecfe98")
	cfg.CodexClientID = getEnv("GITHUB_CODEX_CLIENT_ID", "app_EMoamEEZ73f0CkXaXp7hrann")
	cfg.GitHubOAuthClientID = getEnv("GITHUB_OAUTH_CLIENT_ID", GitHubOAuthClientID)
	cfg.GitHubOAuthClientSecret = getEnv("GITHUB_OAUTH_CLIENT_SECRET", "")

	// Debug settings
	cfg.DebugDocker = getEnvBool("DEBUG_DOCKER", false)
	cfg.DebugDockerPort = getEnvInt("DEBUG_DOCKER_PORT", 2375)

	// Process lifecycle
	cfg.LogFile = getEnv("LOG_FILE", "")
	cfg.ServerLogPath = getEnv("SERVER_LOG_PATH", filepath.Join(xdg.StateHome, appName, "logs", "server.log"))
	cfg.StdinKeepalive = getEnvBool("STDIN_KEEPALIVE", false)

	// MCP OAuth — default to http://127.0.0.1:{Port} so containers can always reach
	// the server and receive OAuth callbacks without explicit env var configuration.
	cfg.MCPOAuthRedirectBase = getEnv("MCP_OAUTH_REDIRECT_BASE", fmt.Sprintf("http://127.0.0.1:%d", cfg.Port))
	cfg.AgentServerURL = getEnv("AGENT_SERVER_URL", fmt.Sprintf("http://127.0.0.1:%d", cfg.Port))
	cfg.ValidateAPIKeys = getEnvBool("VALIDATE_API_KEYS", true)

	// Desktop shell settings
	cfg.DesktopRuntime = strings.ToLower(getEnv("DISCOBOT_DESKTOP_RUNTIME", ""))
	switch cfg.DesktopRuntime {
	case "", "electron":
	default:
		return nil, fmt.Errorf("DISCOBOT_DESKTOP_RUNTIME must be electron")
	}
	cfg.DesktopMode = cfg.DesktopRuntime != ""
	cfg.DesktopSecret = getEnv("DISCOBOT_DESKTOP_SECRET", getEnv("DISCOBOT_SECRET", ""))
	if cfg.DesktopMode && cfg.DesktopSecret == "" {
		return nil, fmt.Errorf("DISCOBOT_DESKTOP_SECRET (or DISCOBOT_SECRET) is required when DISCOBOT_DESKTOP_RUNTIME is set")
	}
	cfg.DesktopIconPath = getEnv("DISCOBOT_DESKTOP_ICON_PATH", "")

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

func loadCORSOrigins(httpPort, httpsPort int, httpsHosts []string) []string {
	origins := getEnvList("CORS_ORIGINS", defaultCORSOrigins(httpPort, httpsPort, httpsHosts))
	return expandCORSOriginTemplates(origins, httpPort, httpsPort)
}

func defaultCORSOrigins(httpPort, httpsPort int, httpsHosts []string) []string {
	listenerHosts := compactStrings(httpsHosts)
	if len(listenerHosts) == 0 {
		listenerHosts = []string{"localhost"}
	}

	origins := make([]string, 0, len(listenerHosts)*2+6)
	for _, host := range listenerHosts {
		origins = append(origins, fmt.Sprintf("http://%s:%d", host, httpPort))
		if host == "localhost" {
			origins = append(origins, fmt.Sprintf("http://*.localhost:%d", httpPort))
		}
	}
	origins = append(origins,
		"http://localhost:3000",
		"http://*.localhost:3000",
		"http://localhost:3100",
		"http://*.localhost:3100",
	)
	if httpsPort > 0 {
		for _, host := range listenerHosts {
			origins = append(origins, fmt.Sprintf("https://%s:%d", host, httpsPort))
			if host == "localhost" {
				origins = append(origins, fmt.Sprintf("https://*.localhost:%d", httpsPort))
			}
		}
	}
	return origins
}

func expandCORSOriginTemplates(origins []string, httpPort, httpsPort int) []string {
	result := make([]string, 0, len(origins))
	for _, origin := range compactStrings(origins) {
		expanded := strings.ReplaceAll(origin, "{HTTP_PORT}", strconv.Itoa(httpPort))
		if strings.Contains(expanded, "{HTTPS_PORT}") {
			if httpsPort <= 0 {
				continue
			}
			expanded = strings.ReplaceAll(expanded, "{HTTPS_PORT}", strconv.Itoa(httpsPort))
		}
		result = append(result, expanded)
	}
	return result
}

func compactStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		result = append(result, value)
	}
	return result
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if d, err := time.ParseDuration(value); err == nil {
			return d
		}
	}
	return defaultValue
}

// PublicBaseURL returns the externally visible base URL for this server.
func (c *Config) PublicBaseURL() string {
	host := strings.TrimSpace(c.PublicHostname)
	if host == "" {
		host = fmt.Sprintf("localhost:%d", c.Port)
	}

	host = strings.TrimRight(host, "/")
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return host
	}

	if isLoopbackHost(host) {
		return "http://" + host
	}

	return "https://" + host
}

// CookiesSecure reports whether auth cookies must be marked Secure.
func (c *Config) CookiesSecure() bool {
	return c.AuthCookieSameSite == "none" || strings.HasPrefix(c.PublicBaseURL(), "https://")
}

// OIDCBackchannelURL returns the server-to-server base URL for the configured OIDC provider.
// If unset, it defaults to the public issuer URL.
func (c *Config) OIDCBackchannelURL() string {
	if strings.TrimSpace(c.OIDCBackchannelBaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(c.OIDCBackchannelBaseURL), "/")
	}
	return strings.TrimRight(strings.TrimSpace(c.OIDCIssuerURL), "/")
}

func (c *Config) CookieSameSite() http.SameSite {
	switch strings.ToLower(strings.TrimSpace(c.AuthCookieSameSite)) {
	case "none":
		return http.SameSiteNoneMode
	case "strict":
		return http.SameSiteStrictMode
	case "lax", "":
		return http.SameSiteLaxMode
	default:
		return http.SameSiteLaxMode
	}
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(host)
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")

	switch {
	case strings.HasPrefix(host, "localhost"):
		return true
	case strings.HasPrefix(host, "127.0.0.1"):
		return true
	case strings.HasPrefix(host, "::1"):
		return true
	default:
		return false
	}
}
