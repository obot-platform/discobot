package credentials

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os/exec"
	"sort"
	"sync"
)

// EnvVar represents a credential mapped to an environment variable.
type EnvVar struct {
	EnvVar       string `json:"envVar"`
	Value        string `json:"value"`
	Provider     string `json:"provider"`
	AuthType     string `json:"authType"` // "api_key" or "oauth"
	AgentVisible bool   `json:"agentVisible"`
	ExpiresAt    *int64 `json:"expiresAt,omitempty"` // OAuth only (unix timestamp)
}

// Manager holds the current set of credentials received via the request header.
// Credentials are stored in memory and queried by the provider registry;
// the manager never writes to OS environment variables.
type Manager struct {
	mu    sync.RWMutex
	hash  string
	creds []EnvVar
}

// NewManager creates a new credential Manager.
func NewManager() *Manager {
	return &Manager{}
}

// Snapshot returns a copy of the currently applied env vars keyed by name.
func (m *Manager) Snapshot() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make(map[string]string, len(m.creds))
	for _, cred := range m.creds {
		if !cred.AgentVisible {
			continue
		}
		out[cred.EnvVar] = cred.Value
	}
	return out
}

// VisibleGet returns an agent-visible credential for the given environment variable name, or nil if not found.
func (m *Manager) VisibleGet(envVarName string) *EnvVar {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for i := range m.creds {
		if m.creds[i].EnvVar == envVarName && m.creds[i].AgentVisible {
			return &m.creds[i]
		}
	}
	return nil
}

// Apply parses the credentials header, stores them in memory if changed,
// and configures git user if provided.
func (m *Manager) Apply(credentialsHeader, gitUserName, gitUserEmail string) {
	if credentialsHeader != "" {
		creds := parseHeader(credentialsHeader)
		m.update(creds)
	}

	configureGitUser(gitUserName, gitUserEmail)
}

// ForProvider returns all credentials for the given provider ID.
func (m *Manager) ForProvider(providerID string) []EnvVar {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []EnvVar
	for _, c := range m.creds {
		if c.Provider == providerID {
			result = append(result, c)
		}
	}
	return result
}

// Get returns the credential for the given environment variable name, or nil if not found.
func (m *Manager) Get(envVarName string) *EnvVar {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for i := range m.creds {
		if m.creds[i].EnvVar == envVarName {
			return &m.creds[i]
		}
	}
	return nil
}

// parseHeader parses the X-Discobot-Credentials JSON array header.
func parseHeader(headerValue string) []EnvVar {
	if headerValue == "" {
		return nil
	}

	var raw []struct {
		EnvVar       string `json:"envVar"`
		Value        string `json:"value"`
		Provider     string `json:"provider"`
		AuthType     string `json:"authType"`
		AgentVisible *bool  `json:"agentVisible"`
		ExpiresAt    *int64 `json:"expiresAt,omitempty"`
	}
	if err := json.Unmarshal([]byte(headerValue), &raw); err != nil {
		log.Printf("credentials: failed to parse header: %v", err)
		return nil
	}
	creds := make([]EnvVar, 0, len(raw))
	for _, entry := range raw {
		agentVisible := true
		if entry.AgentVisible != nil {
			agentVisible = *entry.AgentVisible
		}
		creds = append(creds, EnvVar{
			EnvVar:       entry.EnvVar,
			Value:        entry.Value,
			Provider:     entry.Provider,
			AuthType:     entry.AuthType,
			AgentVisible: agentVisible,
			ExpiresAt:    entry.ExpiresAt,
		})
	}
	return creds
}

// update checks if credentials changed and stores the new set if so.
func (m *Manager) update(creds []EnvVar) {
	newHash := computeHash(creds)

	m.mu.Lock()
	defer m.mu.Unlock()

	if newHash == m.hash {
		return
	}

	m.hash = newHash
	m.creds = creds
}

// computeHash computes a SHA-256 hash of credentials for change detection.
func computeHash(creds []EnvVar) string {
	// Sort by envVar for consistent ordering.
	sorted := make([]EnvVar, len(creds))
	copy(sorted, creds)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].EnvVar < sorted[j].EnvVar
	})

	data, err := json.Marshal(sorted)
	if err != nil {
		return ""
	}

	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// configureGitUser sets git global user.name and user.email if provided.
func configureGitUser(name, email string) {
	if name != "" {
		if err := exec.Command("git", "config", "--global", "user.name", name).Run(); err != nil {
			log.Printf("credentials: failed to set git user.name: %v", err)
		}
	}
	if email != "" {
		if err := exec.Command("git", "config", "--global", "user.email", email).Run(); err != nil {
			log.Printf("credentials: failed to set git user.email: %v", err)
		}
	}
}
