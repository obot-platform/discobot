package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/encryption"
	"github.com/obot-platform/discobot/server/internal/model"
	"github.com/obot-platform/discobot/server/internal/oauth"
	"github.com/obot-platform/discobot/server/internal/providers"
	"github.com/obot-platform/discobot/server/internal/store"
)

// Supported providers
const (
	ProviderAnthropic     = "anthropic"
	ProviderGitHubCopilot = "github-copilot"
	ProviderGitHub        = "github-git"
	ProviderCodex         = "codex"
	ProviderOpenAI        = "openai"
	ProviderTavily        = "tavily"
	ProviderDiscobot      = "discobot"
)

// mcpProviderPrefix is the credential provider prefix for MCP OAuth tokens.
// The full provider key is "mcp:{resourceUrl}".
const mcpProviderPrefix = "mcp:"

// Auth types
const (
	AuthTypeAPIKey = "api_key"
	AuthTypeOAuth  = "oauth"
	AuthTypeID     = "id"
)

// oauthEnvVars maps provider IDs to their OAuth-specific environment variable names.
// If a provider has an OAuth-specific env var, it will be used instead of the provider's
// default env var when the credential type is OAuth.
var oauthEnvVars = map[string]string{
	ProviderAnthropic: "CLAUDE_CODE_OAUTH_TOKEN",
	// Add more OAuth-specific env vars here as needed
	// e.g., ProviderOpenAI: "OPENAI_OAUTH_TOKEN",
}

var (
	ErrCredentialNotFound = errors.New("credential not found")
	ErrInvalidProvider    = errors.New("invalid provider")
	ErrEncryptionFailed   = errors.New("encryption failed")
	ErrDecryptionFailed   = errors.New("decryption failed")
)

// APIKeyCredential represents an encrypted secret-style credential value.
// It is used for both api_key and id auth types.
type APIKeyCredential struct {
	APIKey string `json:"api_key"`
}

// OAuthCredential represents OAuth tokens
type OAuthCredential struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	Scope        string    `json:"scope,omitempty"`
}

// CredentialInfo represents safe credential info for API responses (no secrets)
type CredentialInfo struct {
	ID           string     `json:"id"`
	Provider     string     `json:"provider"`
	Name         string     `json:"name"`
	AuthType     string     `json:"authType"`
	IsConfigured bool       `json:"isConfigured"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"` // For OAuth credentials
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

// CredentialService handles credential operations with encryption
type CredentialService struct {
	store            *store.Store
	cfg              *config.Config
	encryptor        *encryption.Encryptor
	lastRefreshFail  map[string]time.Time // Track last refresh failure per provider
	refreshFailMutex sync.RWMutex         // Protect the map
}

// NewCredentialService creates a new credential service
func NewCredentialService(s *store.Store, cfg *config.Config) (*CredentialService, error) {
	enc, err := encryption.NewEncryptor(cfg.EncryptionKey)
	if err != nil {
		return nil, err
	}

	return &CredentialService{
		store:           s,
		cfg:             cfg,
		encryptor:       enc,
		lastRefreshFail: make(map[string]time.Time),
	}, nil
}

// List returns all credentials for a project (safe info only, no secrets)
func (s *CredentialService) List(ctx context.Context, projectID string) ([]CredentialInfo, error) {
	creds, err := s.store.ListCredentialsByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}

	result := make([]CredentialInfo, len(creds))
	for i, c := range creds {
		result[i] = s.toCredentialInfo(c)
	}
	return result, nil
}

// Get returns credential info for a specific provider (safe info only, no secrets)
func (s *CredentialService) Get(ctx context.Context, projectID, provider string) (*CredentialInfo, error) {
	cred, err := s.store.GetCredentialByProvider(ctx, projectID, provider)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrCredentialNotFound
		}
		return nil, err
	}

	info := s.toCredentialInfo(cred)
	return &info, nil
}

// SetAPIKey creates or updates an API key credential
func (s *CredentialService) SetAPIKey(ctx context.Context, projectID, provider, name, apiKey string) (*CredentialInfo, error) {
	return s.setSecretCredential(ctx, projectID, provider, name, AuthTypeAPIKey, apiKey)
}

// SetID creates or updates an ID credential.
func (s *CredentialService) SetID(ctx context.Context, projectID, provider, name, value string) (*CredentialInfo, error) {
	return s.setSecretCredential(ctx, projectID, provider, name, AuthTypeID, value)
}

func (s *CredentialService) setSecretCredential(ctx context.Context, projectID, provider, name, authType, value string) (*CredentialInfo, error) {
	if !isValidProvider(provider) {
		return nil, ErrInvalidProvider
	}

	// Encrypt the API key
	data := APIKeyCredential{APIKey: value}
	encrypted, err := s.encryptor.EncryptJSON(data)
	if err != nil {
		return nil, ErrEncryptionFailed
	}

	// Check if credential already exists
	existing, err := s.store.GetCredentialByProvider(ctx, projectID, provider)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	if existing != nil {
		// Update existing
		existing.Name = name
		existing.AuthType = authType
		existing.EncryptedData = encrypted
		existing.IsConfigured = true
		if err := s.store.UpdateCredential(ctx, existing); err != nil {
			return nil, err
		}
		info := s.toCredentialInfo(existing)
		return &info, nil
	}

	// Create new
	cred := &model.Credential{
		ProjectID:     projectID,
		Provider:      provider,
		Name:          name,
		AuthType:      authType,
		EncryptedData: encrypted,
		IsConfigured:  true,
	}
	if err := s.store.CreateCredential(ctx, cred); err != nil {
		return nil, err
	}

	info := s.toCredentialInfo(cred)
	return &info, nil
}

// SetOAuthTokens creates or updates OAuth tokens for a credential
func (s *CredentialService) SetOAuthTokens(ctx context.Context, projectID, provider, name string, tokens *OAuthCredential) (*CredentialInfo, error) {
	if !isValidProvider(provider) {
		return nil, ErrInvalidProvider
	}

	// Encrypt the tokens
	encrypted, err := s.encryptor.EncryptJSON(tokens)
	if err != nil {
		return nil, ErrEncryptionFailed
	}

	// Check if credential already exists
	existing, err := s.store.GetCredentialByProvider(ctx, projectID, provider)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	if existing != nil {
		// Update existing
		existing.Name = name
		existing.AuthType = AuthTypeOAuth
		existing.EncryptedData = encrypted
		existing.IsConfigured = true
		if err := s.store.UpdateCredential(ctx, existing); err != nil {
			return nil, err
		}
		info := s.toCredentialInfo(existing)
		return &info, nil
	}

	// Create new
	cred := &model.Credential{
		ProjectID:     projectID,
		Provider:      provider,
		Name:          name,
		AuthType:      AuthTypeOAuth,
		EncryptedData: encrypted,
		IsConfigured:  true,
	}
	if err := s.store.CreateCredential(ctx, cred); err != nil {
		return nil, err
	}

	info := s.toCredentialInfo(cred)
	return &info, nil
}

// GetAPIKey retrieves and decrypts an API key credential
func (s *CredentialService) GetAPIKey(ctx context.Context, projectID, provider string) (*APIKeyCredential, error) {
	cred, err := s.store.GetCredentialByProvider(ctx, projectID, provider)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrCredentialNotFound
		}
		return nil, err
	}

	if cred.AuthType != AuthTypeAPIKey {
		return nil, errors.New("credential is not an API key type")
	}

	var data APIKeyCredential
	if err := s.encryptor.DecryptJSON(cred.EncryptedData, &data); err != nil {
		return nil, ErrDecryptionFailed
	}

	return &data, nil
}

// GetOAuthTokens retrieves and decrypts OAuth tokens.
// If the token is expired and a refresh token is available, it will automatically refresh.
func (s *CredentialService) GetOAuthTokens(ctx context.Context, projectID, provider string) (*OAuthCredential, error) {
	cred, err := s.store.GetCredentialByProvider(ctx, projectID, provider)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrCredentialNotFound
		}
		return nil, err
	}

	if cred.AuthType != AuthTypeOAuth {
		return nil, errors.New("credential is not an OAuth type")
	}

	var tokens OAuthCredential
	if err := s.encryptor.DecryptJSON(cred.EncryptedData, &tokens); err != nil {
		return nil, ErrDecryptionFailed
	}

	// Check if token is expired (with 5 minute buffer for safety)
	if !tokens.ExpiresAt.IsZero() && time.Now().Add(5*time.Minute).After(tokens.ExpiresAt) {
		// Token is expired or about to expire
		if tokens.RefreshToken != "" {
			// Check if we recently failed to refresh this token (backoff mechanism)
			s.refreshFailMutex.RLock()
			lastFail, hasFailed := s.lastRefreshFail[provider]
			s.refreshFailMutex.RUnlock()

			// If we failed within the last 5 minutes, don't try again
			if hasFailed && time.Since(lastFail) < 5*time.Minute {
				log.Printf("Token for provider %s is expired, but skipping refresh (last attempt failed %v ago)",
					provider, time.Since(lastFail).Round(time.Second))
				return &tokens, nil
			}

			log.Printf("Token for provider %s is expired, attempting refresh", provider)
			refreshed, err := s.RefreshOAuthTokens(ctx, projectID, provider)
			if err != nil {
				log.Printf("Failed to refresh token for provider %s: %v", provider, err)
				// Record the failure time
				s.refreshFailMutex.Lock()
				s.lastRefreshFail[provider] = time.Now()
				s.refreshFailMutex.Unlock()
				// Return the expired token anyway, let the API call fail with proper auth error
				return &tokens, nil
			}
			// Clear the failure record on success
			s.refreshFailMutex.Lock()
			delete(s.lastRefreshFail, provider)
			s.refreshFailMutex.Unlock()
			log.Printf("Successfully refreshed token for provider %s", provider)
			return refreshed, nil
		}
		log.Printf("Token for provider %s is expired but no refresh token available", provider)
	}

	return &tokens, nil
}

// RefreshOAuthTokens refreshes OAuth tokens for a provider
func (s *CredentialService) RefreshOAuthTokens(ctx context.Context, projectID, provider string) (*OAuthCredential, error) {
	// Get existing credential directly from store to avoid recursion
	cred, err := s.store.GetCredentialByProvider(ctx, projectID, provider)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrCredentialNotFound
		}
		return nil, err
	}

	if cred.AuthType != AuthTypeOAuth {
		return nil, errors.New("credential is not an OAuth type")
	}

	// Decrypt existing tokens
	var tokens OAuthCredential
	if err := s.encryptor.DecryptJSON(cred.EncryptedData, &tokens); err != nil {
		return nil, ErrDecryptionFailed
	}

	if tokens.RefreshToken == "" {
		return nil, errors.New("no refresh token available")
	}

	// Refresh based on provider
	var newTokenResp *oauth.TokenResponse
	switch provider {
	case ProviderAnthropic:
		p := oauth.NewAnthropicProvider(s.cfg.AnthropicClientID)
		newTokenResp, err = p.Refresh(ctx, tokens.RefreshToken)
		if err != nil {
			return nil, fmt.Errorf("failed to refresh %s token: %w", provider, err)
		}
	default:
		return nil, fmt.Errorf("token refresh not implemented for provider: %s", provider)
	}

	// Update stored tokens
	updatedTokens := &OAuthCredential{
		AccessToken:  newTokenResp.AccessToken,
		RefreshToken: newTokenResp.RefreshToken,
		TokenType:    newTokenResp.TokenType,
		ExpiresAt:    newTokenResp.ExpiresAt,
		Scope:        newTokenResp.Scope,
	}

	_, err = s.SetOAuthTokens(ctx, projectID, provider, provider+" OAuth", updatedTokens)
	if err != nil {
		return nil, err
	}

	return updatedTokens, nil
}

// Delete removes a credential
func (s *CredentialService) Delete(ctx context.Context, projectID, provider string) error {
	return s.store.DeleteCredential(ctx, projectID, provider)
}

// CredentialEnvVar represents a credential value with its target environment variable.
// Used for passing credentials to agent containers.
type CredentialEnvVar struct {
	EnvVar    string `json:"envVar"`
	Value     string `json:"value"`
	Provider  string `json:"provider"`
	AuthType  string `json:"authType"`            // "api_key", "id", or "oauth"
	ExpiresAt int64  `json:"expiresAt,omitempty"` // OAuth only (unix timestamp)
}

// GetAllDecrypted returns all credentials for a project as environment variable mappings.
// This is used to pass credentials to agent containers.
// Each credential is mapped to its provider's configured environment variable name.
func (s *CredentialService) GetAllDecrypted(ctx context.Context, projectID string) ([]CredentialEnvVar, error) {
	creds, err := s.store.ListCredentialsByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}

	result := make([]CredentialEnvVar, 0, len(creds))
	var mcpTokens []MCPTokenData

	for _, c := range creds {
		if !c.IsConfigured {
			continue
		}

		// Handle MCP OAuth tokens separately: collect into a JSON array.
		if strings.HasPrefix(c.Provider, mcpProviderPrefix) {
			var token MCPTokenData
			if err := s.encryptor.DecryptJSON(c.EncryptedData, &token); err != nil {
				log.Printf("Warning: Failed to decrypt MCP token for provider %s: %v", c.Provider, err)
				continue
			}
			mcpTokens = append(mcpTokens, token)
			continue
		}

		// Get env var names for this provider
		envVars := providers.GetEnvVars(c.Provider)
		if len(envVars) == 0 {
			log.Printf("Warning: No env var configured for provider %s, skipping", c.Provider)
			continue
		}

		switch c.AuthType {
		case AuthTypeAPIKey, AuthTypeID:
			var data APIKeyCredential
			if err := s.encryptor.DecryptJSON(c.EncryptedData, &data); err != nil {
				// Skip credentials that fail to decrypt
				continue
			}
			// Use the first env var for API keys
			result = append(result, CredentialEnvVar{
				EnvVar:   envVars[0],
				Value:    data.APIKey,
				Provider: c.Provider,
				AuthType: c.AuthType,
			})
		case AuthTypeOAuth:
			// Use GetOAuthTokens to get auto-refresh behavior for expired tokens
			tokens, err := s.GetOAuthTokens(ctx, projectID, c.Provider)
			if err != nil {
				// Skip credentials that fail to decrypt or refresh
				log.Printf("Warning: Failed to get OAuth tokens for provider %s: %v", c.Provider, err)
				continue
			}
			// Use OAuth-specific env var if defined, otherwise fall back to provider's first env var
			if tokens.AccessToken != "" {
				envVar := envVars[0] // Default to provider's first env var
				if oauthEnvVar, exists := oauthEnvVars[c.Provider]; exists {
					envVar = oauthEnvVar // Use OAuth-specific env var if defined
				}
				result = append(result, CredentialEnvVar{
					EnvVar:    envVar,
					Value:     tokens.AccessToken,
					Provider:  c.Provider,
					AuthType:  AuthTypeOAuth,
					ExpiresAt: tokens.ExpiresAt.Unix(),
				})
			}
		}
	}

	// Deliver all MCP OAuth tokens as a single JSON array env var.
	if len(mcpTokens) > 0 {
		tokensJSON, err := json.Marshal(mcpTokens)
		if err != nil {
			log.Printf("Warning: Failed to marshal MCP tokens: %v", err)
		} else {
			result = append(result, CredentialEnvVar{
				EnvVar:   "MCP_OAUTH_TOKENS",
				Value:    string(tokensJSON),
				Provider: "mcp-oauth-tokens",
				AuthType: AuthTypeOAuth,
			})
		}
	}

	return result, nil
}

// MCPTokenData represents an OAuth token for an MCP server.
// This is the data stored per-resource-URL and also the element type in
// the MCP_OAUTH_TOKENS JSON array delivered to the agent container.
type MCPTokenData struct {
	URL          string `json:"url"`
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken,omitempty"`
	ExpiresAt    int64  `json:"expiresAt,omitempty"` // unix timestamp
}

// StoreMCPToken upserts an OAuth token for an MCP server, keyed by resource URL.
// The credential provider key is "mcp:{resourceUrl}".
func (s *CredentialService) StoreMCPToken(ctx context.Context, projectID string, data MCPTokenData) error {
	if data.URL == "" {
		return fmt.Errorf("url is required")
	}

	providerKey := mcpProviderPrefix + data.URL

	encrypted, err := s.encryptor.EncryptJSON(data)
	if err != nil {
		return ErrEncryptionFailed
	}

	existing, err := s.store.GetCredentialByProvider(ctx, projectID, providerKey)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}

	if existing != nil {
		existing.AuthType = AuthTypeOAuth
		existing.EncryptedData = encrypted
		existing.IsConfigured = true
		return s.store.UpdateCredential(ctx, existing)
	}

	cred := &model.Credential{
		ProjectID:     projectID,
		Provider:      providerKey,
		Name:          "MCP OAuth: " + data.URL,
		AuthType:      AuthTypeOAuth,
		EncryptedData: encrypted,
		IsConfigured:  true,
	}
	return s.store.CreateCredential(ctx, cred)
}

// isValidProvider checks if a provider is supported
func isValidProvider(provider string) bool {
	switch provider {
	case ProviderAnthropic, ProviderGitHubCopilot, ProviderGitHub, ProviderCodex, ProviderOpenAI, ProviderTavily, ProviderDiscobot:
		return true
	default:
		return false
	}
}

// toCredentialInfo converts a model.Credential to CredentialInfo (safe for API)
// toCredentialInfo converts a model.Credential to CredentialInfo (safe for API)
// For OAuth credentials, it decrypts the data to extract the expiration time
func (s *CredentialService) toCredentialInfo(c *model.Credential) CredentialInfo {
	info := CredentialInfo{
		ID:           c.ID,
		Provider:     c.Provider,
		Name:         c.Name,
		AuthType:     c.AuthType,
		IsConfigured: c.IsConfigured,
		CreatedAt:    c.CreatedAt,
		UpdatedAt:    c.UpdatedAt,
	}

	// For OAuth credentials, include expiration time
	if c.AuthType == AuthTypeOAuth && c.IsConfigured {
		var tokens OAuthCredential
		if err := s.encryptor.DecryptJSON(c.EncryptedData, &tokens); err == nil {
			if !tokens.ExpiresAt.IsZero() {
				info.ExpiresAt = &tokens.ExpiresAt
			}
		}
	}

	return info
}
