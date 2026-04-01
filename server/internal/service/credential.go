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

	"github.com/google/uuid"

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

// customProviderPrefix identifies ad-hoc user-defined env bundle credentials.
const customProviderPrefix = "custom:"

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

// APIKeyCredential is a compatibility view over the first secret env var.
type APIKeyCredential struct {
	APIKey string `json:"api_key"`
}

// SecretEnvVar is a single environment variable stored in a credential.
type SecretEnvVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// SecretCredentialData represents one or more encrypted env vars for secret-style credentials.
// It is used for api_key, id, and custom env credentials.
type SecretCredentialData struct {
	EnvVars []SecretEnvVar `json:"envVars"`
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
	ID           string         `json:"id"`
	Provider     string         `json:"provider"`
	Name         string         `json:"name"`
	Description  string         `json:"description,omitempty"`
	AuthType     string         `json:"authType"`
	IsConfigured bool           `json:"isConfigured"`
	AgentVisible bool           `json:"agentVisible"`
	EnvKeys      []string       `json:"envKeys,omitempty"`
	EnvVars      []SecretEnvVar `json:"envVars,omitempty"`
	ExpiresAt    *time.Time     `json:"expiresAt,omitempty"` // For OAuth credentials
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`
}

// SessionCredentialAssignmentInfo is the client-safe representation of a credential assigned to a session.
type SessionCredentialAssignmentInfo struct {
	CredentialID string         `json:"credentialId"`
	AgentVisible bool           `json:"agentVisible"`
	Credential   CredentialInfo `json:"credential"`
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

// GetByID returns credential info for a specific credential ID without secrets.
func (s *CredentialService) GetByID(ctx context.Context, projectID, credentialID string) (*CredentialInfo, error) {
	cred, err := s.store.GetCredentialByIDForProject(ctx, projectID, credentialID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrCredentialNotFound
		}
		return nil, err
	}

	info := s.toCredentialInfo(cred)
	return &info, nil
}

// UpdateMetadata updates non-secret credential fields without changing stored secret values.
func (s *CredentialService) UpdateMetadata(ctx context.Context, projectID, credentialID, name, description string, agentVisible bool) (*CredentialInfo, error) {
	cred, err := s.store.GetCredentialByIDForProject(ctx, projectID, credentialID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrCredentialNotFound
		}
		return nil, err
	}

	if strings.TrimSpace(name) != "" {
		cred.Name = name
	}
	description = strings.TrimSpace(description)
	if description == "" {
		cred.Description = nil
	} else {
		cred.Description = &description
	}
	cred.AgentVisible = agentVisible
	if err := s.store.UpdateCredential(ctx, cred); err != nil {
		return nil, err
	}
	info := s.toCredentialInfo(cred)
	if cred.AuthType != AuthTypeOAuth {
		if data, err := s.getSecretData(cred); err == nil {
			info.EnvKeys = secretEnvKeys(data.EnvVars)
		}
	}
	return &info, nil
}

// ListSessionAssignments returns all credentials for a session with effective visibility.
func (s *CredentialService) ListSessionAssignments(ctx context.Context, projectID, sessionID string) ([]SessionCredentialAssignmentInfo, error) {
	sess, err := s.store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if sess.ProjectID != projectID {
		return nil, ErrCredentialNotFound
	}
	creds, err := s.store.ListCredentialsByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	assignments, err := s.store.ListSessionCredentialAssignments(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	assignmentByCredentialID := make(map[string]*model.SessionCredentialAssignment, len(assignments))
	for _, assignment := range assignments {
		assignmentByCredentialID[assignment.CredentialID] = assignment
	}
	result := make([]SessionCredentialAssignmentInfo, 0, len(creds))
	for _, cred := range creds {
		agentVisible := cred.AgentVisible
		if assignment, ok := assignmentByCredentialID[cred.ID]; ok {
			agentVisible = assignment.AgentVisible
		}
		info := s.toCredentialInfo(cred)
		info.AgentVisible = agentVisible
		result = append(result, SessionCredentialAssignmentInfo{
			CredentialID: cred.ID,
			AgentVisible: agentVisible,
			Credential:   info,
		})
	}
	return result, nil
}

// SetSessionAssignments replaces session-specific visibility overrides for credentials.
func (s *CredentialService) SetSessionAssignments(ctx context.Context, projectID, sessionID string, assignments []SessionCredentialAssignmentInfo) ([]SessionCredentialAssignmentInfo, error) {
	sess, err := s.store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if sess.ProjectID != projectID {
		return nil, ErrCredentialNotFound
	}
	if err := s.store.DeleteSessionCredentialAssignments(ctx, sessionID); err != nil {
		return nil, err
	}
	for _, assignment := range assignments {
		cred, err := s.store.GetCredentialByIDForProject(ctx, projectID, assignment.CredentialID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, ErrCredentialNotFound
			}
			return nil, err
		}
		if assignment.AgentVisible == cred.AgentVisible {
			continue
		}
		if err := s.store.UpsertSessionCredentialAssignment(ctx, &model.SessionCredentialAssignment{
			SessionID:    sessionID,
			CredentialID: cred.ID,
			AgentVisible: assignment.AgentVisible,
		}); err != nil {
			return nil, err
		}
	}
	return s.ListSessionAssignments(ctx, projectID, sessionID)
}

// SetAPIKey creates or updates an API key credential.
func (s *CredentialService) SetAPIKey(ctx context.Context, projectID, provider, name, apiKey string) (*CredentialInfo, error) {
	return s.SetAPIKeyWithMetadata(ctx, projectID, provider, name, "", apiKey, defaultCredentialAgentVisible(provider))
}

// SetAPIKeyWithMetadata creates or updates an API key credential with explicit metadata.
func (s *CredentialService) SetAPIKeyWithMetadata(ctx context.Context, projectID, provider, name, description, apiKey string, agentVisible bool) (*CredentialInfo, error) {
	return s.setSecretCredential(ctx, projectID, provider, name, description, AuthTypeAPIKey, []SecretEnvVar{{
		Key:   firstProviderEnvVar(provider),
		Value: apiKey,
	}}, agentVisible)
}

// SetID creates or updates an ID credential.
func (s *CredentialService) SetID(ctx context.Context, projectID, provider, name, value string) (*CredentialInfo, error) {
	return s.SetIDWithMetadata(ctx, projectID, provider, name, "", value, defaultCredentialAgentVisible(provider))
}

// SetIDWithMetadata creates or updates an ID credential with explicit metadata.
func (s *CredentialService) SetIDWithMetadata(ctx context.Context, projectID, provider, name, description, value string, agentVisible bool) (*CredentialInfo, error) {
	return s.setSecretCredential(ctx, projectID, provider, name, description, AuthTypeID, []SecretEnvVar{{
		Key:   firstProviderEnvVar(provider),
		Value: value,
	}}, agentVisible)
}

// SetCustomCredential creates or updates a custom env bundle credential.
func (s *CredentialService) SetCustomCredential(ctx context.Context, projectID, credentialID, name, description string, envVars []SecretEnvVar, agentVisible bool) (*CredentialInfo, error) {
	provider := customProviderPrefix + uuid.NewString()
	if credentialID != "" {
		existing, err := s.store.GetCredentialByIDForProject(ctx, projectID, credentialID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		if existing != nil {
			provider = existing.Provider
		}
	}
	return s.setSecretCredential(ctx, projectID, provider, name, description, AuthTypeAPIKey, envVars, agentVisible)
}

func (s *CredentialService) setSecretCredential(ctx context.Context, projectID, provider, name, description, authType string, envVars []SecretEnvVar, agentVisible bool) (*CredentialInfo, error) {
	if !isValidProvider(provider) {
		return nil, ErrInvalidProvider
	}

	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)

	existing, err := s.store.GetCredentialByProvider(ctx, projectID, provider)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	normalizedEnvVars := normalizeSecretEnvVars(envVars)
	if existing != nil && existing.AuthType != AuthTypeOAuth {
		existingData, err := s.getSecretData(existing)
		if err != nil {
			return nil, err
		}
		normalizedEnvVars = mergeSecretEnvVars(existingData.EnvVars, normalizedEnvVars)
	}

	data := SecretCredentialData{EnvVars: normalizedEnvVars}
	encrypted, err := s.encryptor.EncryptJSON(data)
	if err != nil {
		return nil, ErrEncryptionFailed
	}

	var descriptionPtr *string
	if description != "" {
		descriptionPtr = &description
	}

	if existing != nil {
		existing.Name = name
		existing.Description = descriptionPtr
		existing.AuthType = authType
		existing.EncryptedData = encrypted
		existing.IsConfigured = true
		existing.AgentVisible = agentVisible
		if err := s.store.UpdateCredential(ctx, existing); err != nil {
			return nil, err
		}
		info := s.toCredentialInfo(existing)
		info.EnvKeys = secretEnvKeys(data.EnvVars)
		return &info, nil
	}

	cred := &model.Credential{
		ProjectID:     projectID,
		Provider:      provider,
		Name:          name,
		Description:   descriptionPtr,
		AuthType:      authType,
		EncryptedData: encrypted,
		IsConfigured:  true,
		AgentVisible:  agentVisible,
	}
	if err := s.store.CreateCredential(ctx, cred); err != nil {
		return nil, err
	}

	info := s.toCredentialInfo(cred)
	info.EnvKeys = secretEnvKeys(data.EnvVars)
	return &info, nil
}

// SetOAuthTokens creates or updates OAuth tokens for a credential
func (s *CredentialService) SetOAuthTokens(ctx context.Context, projectID, provider, name string, tokens *OAuthCredential) (*CredentialInfo, error) {
	if !isValidProvider(provider) {
		return nil, ErrInvalidProvider
	}
	name = strings.TrimSpace(name)

	encrypted, err := s.encryptor.EncryptJSON(tokens)
	if err != nil {
		return nil, ErrEncryptionFailed
	}

	existing, err := s.store.GetCredentialByProvider(ctx, projectID, provider)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	if existing != nil {
		existing.Name = name
		existing.AuthType = AuthTypeOAuth
		existing.EncryptedData = encrypted
		existing.IsConfigured = true
		if existing.Description == nil && defaultCredentialDescription(provider) != "" {
			description := defaultCredentialDescription(provider)
			existing.Description = &description
		}
		existing.AgentVisible = defaultCredentialAgentVisible(provider)
		if err := s.store.UpdateCredential(ctx, existing); err != nil {
			return nil, err
		}
		info := s.toCredentialInfo(existing)
		return &info, nil
	}

	description := defaultCredentialDescription(provider)
	var descriptionPtr *string
	if description != "" {
		descriptionPtr = &description
	}
	cred := &model.Credential{
		ProjectID:     projectID,
		Provider:      provider,
		Name:          name,
		Description:   descriptionPtr,
		AuthType:      AuthTypeOAuth,
		EncryptedData: encrypted,
		IsConfigured:  true,
		AgentVisible:  defaultCredentialAgentVisible(provider),
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

	if cred.AuthType != AuthTypeAPIKey && cred.AuthType != AuthTypeID {
		return nil, errors.New("credential is not a secret type")
	}

	data, err := s.getSecretData(cred)
	if err != nil {
		return nil, err
	}
	if len(data.EnvVars) == 0 {
		return &APIKeyCredential{}, nil
	}

	return &APIKeyCredential{APIKey: data.EnvVars[0].Value}, nil
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

// Delete removes a credential by ID.
func (s *CredentialService) Delete(ctx context.Context, projectID, credentialID string) error {
	cred, err := s.store.GetCredentialByIDForProject(ctx, projectID, credentialID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrCredentialNotFound
		}
		return err
	}
	if err := s.store.DeleteSessionCredentialAssignmentsForCredential(ctx, cred.ID); err != nil {
		return err
	}
	return s.store.DeleteCredentialByID(ctx, credentialID)
}

// CredentialEnvVar represents a credential value with its target environment variable.
// Used for passing credentials to agent containers.
type CredentialEnvVar struct {
	CredentialID string `json:"credentialId,omitempty"`
	EnvVar       string `json:"envVar"`
	Value        string `json:"value"`
	Provider     string `json:"provider"`
	AuthType     string `json:"authType"` // "api_key", "id", or "oauth"
	AgentVisible bool   `json:"agentVisible"`
	ExpiresAt    int64  `json:"expiresAt,omitempty"` // OAuth only (unix timestamp)
}

// GetAllDecrypted returns all configured credentials for a project as environment variable mappings.
func (s *CredentialService) GetAllDecrypted(ctx context.Context, projectID string) ([]CredentialEnvVar, error) {
	creds, err := s.store.ListCredentialsByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return s.mapCredentialsToEnvVars(ctx, projectID, creds)
}

// GetAllForSession returns all credentials for a session with effective visibility applied.
func (s *CredentialService) GetAllForSession(ctx context.Context, projectID, sessionID string) ([]CredentialEnvVar, error) {
	creds, err := s.store.ListCredentialsByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	assignments, err := s.store.ListSessionCredentialAssignments(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	assignmentByCredentialID := make(map[string]*model.SessionCredentialAssignment, len(assignments))
	for _, assignment := range assignments {
		assignmentByCredentialID[assignment.CredentialID] = assignment
	}
	effective := make([]*model.Credential, 0, len(creds))
	for _, cred := range creds {
		clone := *cred
		if assignment, ok := assignmentByCredentialID[cred.ID]; ok {
			clone.AgentVisible = assignment.AgentVisible
		}
		effective = append(effective, &clone)
	}
	return s.mapCredentialsToEnvVars(ctx, projectID, effective)
}

// GetVisibleEnvVarsForSession returns only the credentials that should be exposed
// to subprocess environments for the given session.
func (s *CredentialService) GetVisibleEnvVarsForSession(ctx context.Context, sessionID string) (map[string]string, error) {
	sess, err := s.store.GetSessionByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	envVars, err := s.GetAllForSession(ctx, sess.ProjectID, sessionID)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(envVars))
	for _, envVar := range envVars {
		if !envVar.AgentVisible {
			continue
		}
		result[envVar.EnvVar] = envVar.Value
	}
	return result, nil
}

func (s *CredentialService) mapCredentialsToEnvVars(ctx context.Context, projectID string, creds []*model.Credential) ([]CredentialEnvVar, error) {
	result := make([]CredentialEnvVar, 0, len(creds))
	var mcpTokens []MCPTokenData

	for _, c := range creds {
		if !c.IsConfigured {
			continue
		}

		if strings.HasPrefix(c.Provider, mcpProviderPrefix) {
			var token MCPTokenData
			if err := s.encryptor.DecryptJSON(c.EncryptedData, &token); err != nil {
				log.Printf("Warning: Failed to decrypt MCP token for provider %s: %v", c.Provider, err)
				continue
			}
			mcpTokens = append(mcpTokens, token)
			continue
		}

		switch c.AuthType {
		case AuthTypeAPIKey, AuthTypeID:
			data, err := s.getSecretData(c)
			if err != nil {
				continue
			}
			for _, envVar := range data.EnvVars {
				result = append(result, CredentialEnvVar{
					CredentialID: c.ID,
					EnvVar:       envVar.Key,
					Value:        envVar.Value,
					Provider:     c.Provider,
					AuthType:     c.AuthType,
					AgentVisible: c.AgentVisible,
				})
			}
		case AuthTypeOAuth:
			tokens, err := s.GetOAuthTokens(ctx, projectID, c.Provider)
			if err != nil {
				log.Printf("Warning: Failed to get OAuth tokens for provider %s: %v", c.Provider, err)
				continue
			}
			if tokens.AccessToken != "" {
				envVar := firstProviderEnvVar(c.Provider)
				if oauthEnvVar, exists := oauthEnvVars[c.Provider]; exists {
					envVar = oauthEnvVar
				}
				if envVar == "" {
					continue
				}
				result = append(result, CredentialEnvVar{
					CredentialID: c.ID,
					EnvVar:       envVar,
					Value:        tokens.AccessToken,
					Provider:     c.Provider,
					AuthType:     AuthTypeOAuth,
					AgentVisible: c.AgentVisible,
					ExpiresAt:    tokens.ExpiresAt.Unix(),
				})
			}
		}
	}

	if len(mcpTokens) > 0 {
		tokensJSON, err := json.Marshal(mcpTokens)
		if err != nil {
			log.Printf("Warning: Failed to marshal MCP tokens: %v", err)
		} else {
			result = append(result, CredentialEnvVar{
				EnvVar:       "MCP_OAUTH_TOKENS",
				Value:        string(tokensJSON),
				Provider:     "mcp-oauth-tokens",
				AuthType:     AuthTypeOAuth,
				AgentVisible: true,
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
		existing.AgentVisible = true
		return s.store.UpdateCredential(ctx, existing)
	}

	cred := &model.Credential{
		ProjectID:     projectID,
		Provider:      providerKey,
		Name:          "MCP OAuth: " + data.URL,
		AuthType:      AuthTypeOAuth,
		EncryptedData: encrypted,
		IsConfigured:  true,
		AgentVisible:  true,
	}
	return s.store.CreateCredential(ctx, cred)
}

func (s *CredentialService) getSecretData(cred *model.Credential) (*SecretCredentialData, error) {
	var data SecretCredentialData
	if err := s.encryptor.DecryptJSON(cred.EncryptedData, &data); err == nil {
		data.EnvVars = normalizeSecretEnvVars(data.EnvVars)
		return &data, nil
	}

	// Backward compatibility: older secret credentials stored a single api_key field.
	var legacy APIKeyCredential
	if err := s.encryptor.DecryptJSON(cred.EncryptedData, &legacy); err != nil {
		return nil, ErrDecryptionFailed
	}

	key := firstProviderEnvVar(cred.Provider)
	if key == "" {
		key = "VALUE"
	}
	data.EnvVars = []SecretEnvVar{{Key: key, Value: legacy.APIKey}}
	return &data, nil
}

func normalizeSecretEnvVars(envVars []SecretEnvVar) []SecretEnvVar {
	result := make([]SecretEnvVar, 0, len(envVars))
	seen := map[string]struct{}{}
	for _, envVar := range envVars {
		key := strings.TrimSpace(envVar.Key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, SecretEnvVar{Key: key, Value: envVar.Value})
	}
	return result
}

func mergeSecretEnvVars(existingEnvVars, updatedEnvVars []SecretEnvVar) []SecretEnvVar {
	existingValues := make(map[string]string, len(existingEnvVars))
	for _, envVar := range normalizeSecretEnvVars(existingEnvVars) {
		existingValues[envVar.Key] = envVar.Value
	}

	result := make([]SecretEnvVar, 0, len(updatedEnvVars))
	for _, envVar := range normalizeSecretEnvVars(updatedEnvVars) {
		if strings.TrimSpace(envVar.Value) != "" {
			result = append(result, envVar)
			continue
		}
		if existingValue, ok := existingValues[envVar.Key]; ok {
			result = append(result, SecretEnvVar{Key: envVar.Key, Value: existingValue})
		}
	}
	return result
}

func secretEnvKeys(envVars []SecretEnvVar) []string {
	keys := make([]string, 0, len(envVars))
	for _, envVar := range envVars {
		if strings.TrimSpace(envVar.Key) == "" {
			continue
		}
		keys = append(keys, strings.TrimSpace(envVar.Key))
	}
	return keys
}

func firstProviderEnvVar(provider string) string {
	envVars := providers.GetEnvVars(provider)
	if len(envVars) == 0 {
		return ""
	}
	return envVars[0]
}

func defaultCredentialAgentVisible(_ string) bool {
	return false
}

func defaultCredentialDescription(provider string) string {
	switch provider {
	case ProviderOpenAI:
		return "Used internally for OpenAI model access."
	case ProviderTavily:
		return "Used internally for Tavily-backed tools."
	default:
		return ""
	}
}

// isValidProvider checks if a provider is supported
func isValidProvider(provider string) bool {
	if strings.HasPrefix(provider, customProviderPrefix) || strings.HasPrefix(provider, mcpProviderPrefix) {
		return true
	}
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
		Name:         strings.TrimSpace(c.Name),
		AuthType:     c.AuthType,
		IsConfigured: c.IsConfigured,
		AgentVisible: c.AgentVisible,
		CreatedAt:    c.CreatedAt,
		UpdatedAt:    c.UpdatedAt,
	}
	if c.Description != nil {
		info.Description = *c.Description
	}

	if c.AuthType == AuthTypeOAuth && c.IsConfigured {
		var tokens OAuthCredential
		if err := s.encryptor.DecryptJSON(c.EncryptedData, &tokens); err == nil {
			if !tokens.ExpiresAt.IsZero() {
				info.ExpiresAt = &tokens.ExpiresAt
			}
		}
		return info
	}

	if data, err := s.getSecretData(c); err == nil {
		info.EnvKeys = secretEnvKeys(data.EnvVars)
	}

	return info
}
