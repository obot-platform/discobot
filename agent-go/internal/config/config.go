package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config holds all configuration for the agent-go process.
type Config struct {
	// Server settings (HTTP API mode)
	Port       int    // HTTP server port (DISCOBOT_PORT, default: 3002)
	SecretHash string // Auth secret for API access (DISCOBOT_SECRET)

	// Agent settings
	AgentCwd         string   // Working directory for the agent (DISCOBOT_AGENT_CWD, default: cwd)
	Model            string   // Default model in "providerId/modelId" format (DISCOBOT_MODEL)
	BashEnvAllowlist []string // Optional CSV allowlist of env var names passed to Bash (DISCOBOT_BASH_ENV_ALLOWLIST, default: pass all env)

	// Storage
	DataDir    string // Root data directory (DISCOBOT_DATA_DIR, default: ~/.discobot)
	ThreadsDir string // Thread persistence directory (DISCOBOT_THREADS_DIR)

	// Hooks
	HooksEnabled bool   // Enable file hooks (DISCOBOT_HOOKS_ENABLED)
	SessionID    string // Session ID for hooks (DISCOBOT_SESSION_ID, default: "default")

	// Idle timeout
	IdleTimeout time.Duration // Exit after idle period with no active completions (DISCOBOT_IDLE_TIMEOUT, 0 = disabled)

	// MCP OAuth settings
	MCPOAuthRedirectBase string // Base URL for OAuth callbacks (DISCOBOT_MCP_OAUTH_REDIRECT_BASE)
	DiscobotServerURL    string // Discobot server URL for posting tokens (DISCOBOT_SERVER_URL)
	DiscobotProjectID    string // Project ID for the token POST path (DISCOBOT_PROJECT_ID)
}

// Load reads configuration from environment variables.
// Call godotenv.Load() before this to support .env files.
func Load() *Config {
	cwd, _ := os.Getwd()

	cfg := &Config{}

	// Server
	cfg.Port = getEnvInt("DISCOBOT_PORT", 3002)
	cfg.SecretHash = getEnv("DISCOBOT_SECRET", "")

	// Agent
	cfg.AgentCwd = getEnv("DISCOBOT_AGENT_CWD", cwd)
	cfg.Model = getEnv("DISCOBOT_MODEL", "")
	cfg.BashEnvAllowlist = getEnvCSV("DISCOBOT_BASH_ENV_ALLOWLIST")

	// Storage — default to ~/.discobot
	home, _ := os.UserHomeDir()
	if home == "" {
		home = cwd
	}
	cfg.DataDir = getEnv("DISCOBOT_DATA_DIR", filepath.Join(home, ".discobot"))
	cfg.ThreadsDir = getEnv("DISCOBOT_THREADS_DIR", filepath.Join(cfg.DataDir, "threads"))

	// Hooks
	cfg.HooksEnabled = getEnvBool("DISCOBOT_HOOKS_ENABLED", false)
	cfg.SessionID = getEnv("DISCOBOT_SESSION_ID", "default")

	// Idle timeout
	cfg.IdleTimeout = getEnvDuration("DISCOBOT_IDLE_TIMEOUT", 0)

	// MCP OAuth
	cfg.MCPOAuthRedirectBase = getEnv("DISCOBOT_MCP_OAUTH_REDIRECT_BASE", "")
	cfg.DiscobotServerURL = getEnv("DISCOBOT_SERVER_URL", "")
	cfg.DiscobotProjectID = getEnv("DISCOBOT_PROJECT_ID", "")

	return cfg
}

// ProviderConfig holds configuration for a single LLM provider.
type ProviderConfig struct {
	APIKey  string
	BaseURL string
}

// ProviderConfigs reads provider configuration from environment variables.
// For each provider ID, it checks {UPPER_ID}_API_KEY and optionally {UPPER_ID}_BASE_URL.
// Returns a map of provider ID → ProviderConfig for providers that have an API key set.
func ProviderConfigs(providerIDs []string) map[string]ProviderConfig {
	configs := make(map[string]ProviderConfig)
	for _, id := range providerIDs {
		prefix := strings.ToUpper(id) + "_"
		apiKey := os.Getenv(prefix + "API_KEY")
		if apiKey == "" {
			continue
		}
		pc := ProviderConfig{APIKey: apiKey}
		if baseURL := os.Getenv(prefix + "BASE_URL"); baseURL != "" {
			pc.BaseURL = baseURL
		}
		configs[id] = pc
	}
	return configs
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

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if d, err := time.ParseDuration(value); err == nil {
			return d
		}
	}
	return defaultValue
}

func getEnvCSV(key string) []string {
	value := os.Getenv(key)
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}
