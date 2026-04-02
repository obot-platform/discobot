package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestLoadUsesDiscobotEnvVars(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("DISCOBOT_PORT", "4123")
	t.Setenv("DISCOBOT_SECRET", "secret-hash")
	t.Setenv("DISCOBOT_AGENT_CWD", "/tmp/workspace")
	t.Setenv("DISCOBOT_MODEL", "openai/gpt-5.4")
	t.Setenv("DISCOBOT_BASH_ENV_ALLOWLIST", "FOO, BAR,FOO")
	t.Setenv("DISCOBOT_DATA_DIR", "/tmp/discobot-data")
	t.Setenv("DISCOBOT_THREADS_DIR", "/tmp/discobot-threads")
	t.Setenv("DISCOBOT_HOOKS_ENABLED", "true")
	t.Setenv("DISCOBOT_SESSION_ID", "session-123")
	t.Setenv("DISCOBOT_IDLE_TIMEOUT", "45s")
	t.Setenv("DISCOBOT_MCP_OAUTH_REDIRECT_BASE", "http://127.0.0.1:9999")
	t.Setenv("DISCOBOT_SERVER_URL", "http://127.0.0.1:3001")
	t.Setenv("DISCOBOT_PROJECT_ID", "project-123")

	cfg := Load()

	if cfg.Port != 4123 {
		t.Fatalf("expected port 4123, got %d", cfg.Port)
	}
	if cfg.SecretHash != "secret-hash" {
		t.Fatalf("expected secret hash to load from DISCOBOT_SECRET, got %q", cfg.SecretHash)
	}
	if cfg.AgentCwd != "/tmp/workspace" {
		t.Fatalf("expected agent cwd to load from DISCOBOT_AGENT_CWD, got %q", cfg.AgentCwd)
	}
	if cfg.Model != "openai/gpt-5.4" {
		t.Fatalf("expected model to load from DISCOBOT_MODEL, got %q", cfg.Model)
	}
	if want := []string{"FOO", "BAR"}; !reflect.DeepEqual(cfg.BashEnvAllowlist, want) {
		t.Fatalf("expected bash env allowlist %v, got %v", want, cfg.BashEnvAllowlist)
	}
	if cfg.DataDir != "/tmp/discobot-data" {
		t.Fatalf("expected data dir to load from DISCOBOT_DATA_DIR, got %q", cfg.DataDir)
	}
	if cfg.ThreadsDir != "/tmp/discobot-threads" {
		t.Fatalf("expected threads dir to load from DISCOBOT_THREADS_DIR, got %q", cfg.ThreadsDir)
	}
	if !cfg.HooksEnabled {
		t.Fatal("expected hooks to be enabled from DISCOBOT_HOOKS_ENABLED")
	}
	if cfg.SessionID != "session-123" {
		t.Fatalf("expected session ID to load from DISCOBOT_SESSION_ID, got %q", cfg.SessionID)
	}
	if cfg.IdleTimeout != 45*time.Second {
		t.Fatalf("expected idle timeout 45s, got %s", cfg.IdleTimeout)
	}
	if cfg.MCPOAuthRedirectBase != "http://127.0.0.1:9999" {
		t.Fatalf("expected MCP OAuth redirect base to load from DISCOBOT_MCP_OAUTH_REDIRECT_BASE, got %q", cfg.MCPOAuthRedirectBase)
	}
	if cfg.DiscobotServerURL != "http://127.0.0.1:3001" {
		t.Fatalf("expected server URL to load from DISCOBOT_SERVER_URL, got %q", cfg.DiscobotServerURL)
	}
	if cfg.DiscobotProjectID != "project-123" {
		t.Fatalf("expected project ID to load from DISCOBOT_PROJECT_ID, got %q", cfg.DiscobotProjectID)
	}
}

func TestLoadIgnoresLegacyUnprefixedConfigEnvVars(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("PORT", "4123")
	t.Setenv("AGENT_CWD", "/tmp/workspace")
	t.Setenv("MODEL", "openai/gpt-5.4")
	t.Setenv("BASH_ENV_ALLOWLIST", "FOO,BAR")
	t.Setenv("DATA_DIR", "/tmp/discobot-data")
	t.Setenv("THREADS_DIR", "/tmp/discobot-threads")
	t.Setenv("SESSION_ID", "session-123")
	t.Setenv("IDLE_TIMEOUT", "45s")
	t.Setenv("MCP_OAUTH_REDIRECT_BASE", "http://127.0.0.1:9999")

	t.Setenv("DISCOBOT_PORT", "")
	t.Setenv("DISCOBOT_AGENT_CWD", "")
	t.Setenv("DISCOBOT_MODEL", "")
	t.Setenv("DISCOBOT_BASH_ENV_ALLOWLIST", "")
	t.Setenv("DISCOBOT_DATA_DIR", "")
	t.Setenv("DISCOBOT_THREADS_DIR", "")
	t.Setenv("DISCOBOT_SESSION_ID", "")
	t.Setenv("DISCOBOT_IDLE_TIMEOUT", "")
	t.Setenv("DISCOBOT_MCP_OAUTH_REDIRECT_BASE", "")

	cfg := Load()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Port != 3002 {
		t.Fatalf("expected default port 3002 when DISCOBOT_PORT is unset, got %d", cfg.Port)
	}
	if cfg.AgentCwd != cwd {
		t.Fatalf("expected default cwd %q when DISCOBOT_AGENT_CWD is unset, got %q", cwd, cfg.AgentCwd)
	}
	if cfg.Model != "" {
		t.Fatalf("expected empty model when DISCOBOT_MODEL is unset, got %q", cfg.Model)
	}
	if len(cfg.BashEnvAllowlist) != 0 {
		t.Fatalf("expected empty bash env allowlist when DISCOBOT_BASH_ENV_ALLOWLIST is unset, got %v", cfg.BashEnvAllowlist)
	}
	if want := filepath.Join(homeDir, ".discobot"); cfg.DataDir != want {
		t.Fatalf("expected default data dir %q, got %q", want, cfg.DataDir)
	}
	if want := filepath.Join(homeDir, ".discobot", "threads"); cfg.ThreadsDir != want {
		t.Fatalf("expected default threads dir %q, got %q", want, cfg.ThreadsDir)
	}
	if cfg.SessionID != "default" {
		t.Fatalf("expected default session ID when DISCOBOT_SESSION_ID is unset, got %q", cfg.SessionID)
	}
	if cfg.IdleTimeout != 0 {
		t.Fatalf("expected idle timeout 0 when DISCOBOT_IDLE_TIMEOUT is unset, got %s", cfg.IdleTimeout)
	}
	if cfg.MCPOAuthRedirectBase != "" {
		t.Fatalf("expected empty MCP OAuth redirect base when DISCOBOT_MCP_OAUTH_REDIRECT_BASE is unset, got %q", cfg.MCPOAuthRedirectBase)
	}
}
