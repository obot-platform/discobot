package service

import (
	"context"
	"testing"
	"time"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/providers"
)

func TestGetAllDecrypted_DiscobotID_UsesCorrectEnvVar(t *testing.T) {
	st := setupTestStore(t)
	cfg := &config.Config{
		EncryptionKey: []byte("test-key-32-bytes-long-123456789"),
	}

	credSvc, err := NewCredentialService(st, cfg)
	if err != nil {
		t.Fatalf("Failed to create credential service: %v", err)
	}

	ctx := context.Background()
	projectID := "test-project"

	envVars := providers.GetEnvVars(ProviderDiscobot)
	if len(envVars) < 1 {
		t.Fatalf("Expected Discobot provider to have at least 1 env var, got %d", len(envVars))
	}
	if envVars[0] != "DISCOBOT_TOKEN" {
		t.Errorf("Expected first env var to be DISCOBOT_TOKEN, got %s", envVars[0])
	}

	_, err = credSvc.SetID(ctx, projectID, ProviderDiscobot, "Discobot Token", "discobot-token-123")
	if err != nil {
		t.Fatalf("Failed to set API key: %v", err)
	}

	envVarMappings, err := credSvc.GetAllDecrypted(ctx, projectID)
	if err != nil {
		t.Fatalf("Failed to get all decrypted: %v", err)
	}

	if len(envVarMappings) != 1 {
		t.Fatalf("Expected 1 env var mapping, got %d", len(envVarMappings))
	}
	if envVarMappings[0].AuthType != AuthTypeID {
		t.Errorf("Expected auth type %s, got %s", AuthTypeID, envVarMappings[0].AuthType)
	}
	if envVarMappings[0].EnvVar != "DISCOBOT_TOKEN" {
		t.Errorf("Expected env var DISCOBOT_TOKEN, got %s", envVarMappings[0].EnvVar)
	}
	if envVarMappings[0].Value != "discobot-token-123" {
		t.Errorf("Expected value 'discobot-token-123', got %s", envVarMappings[0].Value)
	}
}

func TestGetAllDecrypted_AnthropicOAuth_UsesCorrectEnvVar(t *testing.T) {
	// Create in-memory store
	st := setupTestStore(t)

	// Create config with encryption key (must be 32 bytes for AES-256)
	cfg := &config.Config{
		EncryptionKey: []byte("test-key-32-bytes-long-123456789"),
	}

	// Create credential service
	credSvc, err := NewCredentialService(st, cfg)
	if err != nil {
		t.Fatalf("Failed to create credential service: %v", err)
	}

	ctx := context.Background()
	projectID := "test-project"

	// Verify Anthropic provider has API key env var
	envVars := providers.GetEnvVars(ProviderAnthropic)
	if len(envVars) < 1 {
		t.Fatalf("Expected Anthropic provider to have at least 1 env var, got %d", len(envVars))
	}
	if envVars[0] != "ANTHROPIC_API_KEY" {
		t.Errorf("Expected first env var to be ANTHROPIC_API_KEY, got %s", envVars[0])
	}

	// Create an OAuth credential for Anthropic
	oauthTokens := &OAuthCredential{
		AccessToken: "oauth-token-test-123",
		TokenType:   "Bearer",
	}
	oauthInfo, err := credSvc.SetOAuthTokens(ctx, projectID, ProviderAnthropic, "OAuth Token", oauthTokens)
	if err != nil {
		t.Fatalf("Failed to set OAuth tokens: %v", err)
	}
	if oauthInfo.AuthType != AuthTypeOAuth {
		t.Errorf("Expected auth type %s, got %s", AuthTypeOAuth, oauthInfo.AuthType)
	}

	// Get all decrypted credentials
	envVarMappings, err := credSvc.GetAllDecrypted(ctx, projectID)
	if err != nil {
		t.Fatalf("Failed to get all decrypted: %v", err)
	}

	// Should have 1 mapping (OAuth)
	if len(envVarMappings) != 1 {
		t.Fatalf("Expected 1 env var mapping, got %d", len(envVarMappings))
	}

	// Verify it uses CLAUDE_CODE_OAUTH_TOKEN (second env var for Anthropic OAuth)
	if envVarMappings[0].EnvVar != "CLAUDE_CODE_OAUTH_TOKEN" {
		t.Errorf("Expected env var CLAUDE_CODE_OAUTH_TOKEN, got %s", envVarMappings[0].EnvVar)
	}
	if envVarMappings[0].Value != "oauth-token-test-123" {
		t.Errorf("Expected value 'oauth-token-test-123', got %s", envVarMappings[0].Value)
	}
}

func TestGetAllDecrypted_AnthropicAPIKey_UsesCorrectEnvVar(t *testing.T) {
	// Create in-memory store
	st := setupTestStore(t)

	// Create config with encryption key (must be 32 bytes for AES-256)
	cfg := &config.Config{
		EncryptionKey: []byte("test-key-32-bytes-long-123456789"),
	}

	// Create credential service
	credSvc, err := NewCredentialService(st, cfg)
	if err != nil {
		t.Fatalf("Failed to create credential service: %v", err)
	}

	ctx := context.Background()
	projectID := "test-project"

	// Verify Anthropic provider has API key env var
	envVars := providers.GetEnvVars(ProviderAnthropic)
	if len(envVars) < 1 {
		t.Fatalf("Expected Anthropic provider to have at least 1 env var, got %d", len(envVars))
	}
	if envVars[0] != "ANTHROPIC_API_KEY" {
		t.Errorf("Expected first env var to be ANTHROPIC_API_KEY, got %s", envVars[0])
	}

	// Create an API key credential for Anthropic
	apiKeyInfo, err := credSvc.SetAPIKey(ctx, projectID, ProviderAnthropic, "API Key", "sk-ant-test-123")
	if err != nil {
		t.Fatalf("Failed to set API key: %v", err)
	}
	if apiKeyInfo.AuthType != AuthTypeAPIKey {
		t.Errorf("Expected auth type %s, got %s", AuthTypeAPIKey, apiKeyInfo.AuthType)
	}

	// Get all decrypted credentials
	envVarMappings, err := credSvc.GetAllDecrypted(ctx, projectID)
	if err != nil {
		t.Fatalf("Failed to get all decrypted: %v", err)
	}

	// Should have 1 mapping (API key)
	if len(envVarMappings) != 1 {
		t.Fatalf("Expected 1 env var mapping, got %d", len(envVarMappings))
	}

	// Verify it uses ANTHROPIC_API_KEY (first env var for Anthropic API key)
	if envVarMappings[0].EnvVar != "ANTHROPIC_API_KEY" {
		t.Errorf("Expected env var ANTHROPIC_API_KEY, got %s", envVarMappings[0].EnvVar)
	}
	if envVarMappings[0].Value != "sk-ant-test-123" {
		t.Errorf("Expected value 'sk-ant-test-123', got %s", envVarMappings[0].Value)
	}
}

func TestGetAllDecrypted_OtherProviderOAuth_UsesFirstEnvVar(t *testing.T) {
	// Create in-memory store
	st := setupTestStore(t)

	// Create config with encryption key (must be 32 bytes for AES-256)
	cfg := &config.Config{
		EncryptionKey: []byte("test-key-32-bytes-long-123456789"),
	}

	// Create credential service
	credSvc, err := NewCredentialService(st, cfg)
	if err != nil {
		t.Fatalf("Failed to create credential service: %v", err)
	}

	ctx := context.Background()
	projectID := "test-project"

	// Create an OAuth credential for GitHub Copilot
	oauthTokens := &OAuthCredential{
		AccessToken: "github-copilot-token",
		TokenType:   "Bearer",
	}
	_, err = credSvc.SetOAuthTokens(ctx, projectID, ProviderGitHubCopilot, "GitHub Copilot OAuth", oauthTokens)
	if err != nil {
		t.Fatalf("Failed to set OAuth tokens: %v", err)
	}

	// Get all decrypted credentials
	envVarMappings, err := credSvc.GetAllDecrypted(ctx, projectID)
	if err != nil {
		t.Fatalf("Failed to get all decrypted: %v", err)
	}

	// Should have 1 mapping
	if len(envVarMappings) != 1 {
		t.Fatalf("Expected 1 env var mapping, got %d", len(envVarMappings))
	}

	// Verify it uses GITHUB_TOKEN (first env var for GitHub Copilot)
	if envVarMappings[0].EnvVar != "GITHUB_TOKEN" {
		t.Errorf("Expected env var GITHUB_TOKEN, got %s", envVarMappings[0].EnvVar)
	}
	if envVarMappings[0].Value != "github-copilot-token" {
		t.Errorf("Expected value 'github-copilot-token', got %s", envVarMappings[0].Value)
	}
}

func TestGetOAuthTokens_AutoRefresh(t *testing.T) {
	// Create in-memory store
	st := setupTestStore(t)

	// Create config with encryption key (must be 32 bytes for AES-256)
	cfg := &config.Config{
		EncryptionKey:     []byte("test-key-32-bytes-long-123456789"),
		AnthropicClientID: "test-client-id",
	}

	// Create credential service
	credSvc, err := NewCredentialService(st, cfg)
	if err != nil {
		t.Fatalf("Failed to create credential service: %v", err)
	}

	ctx := context.Background()
	projectID := "test-project"

	// Create an expired OAuth credential for Anthropic
	// Set expiration to 1 hour ago to trigger refresh
	expiredTime := time.Now().Add(-1 * time.Hour)
	oauthTokens := &OAuthCredential{
		AccessToken:  "old-access-token",
		RefreshToken: "valid-refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    expiredTime,
	}
	_, err = credSvc.SetOAuthTokens(ctx, projectID, ProviderAnthropic, "Anthropic OAuth", oauthTokens)
	if err != nil {
		t.Fatalf("Failed to set OAuth tokens: %v", err)
	}

	// Note: In a real test, you would need to mock the HTTP client
	// to simulate the refresh token exchange with Anthropic.
	// For now, this test verifies the logic structure.

	// Get tokens - should attempt auto-refresh but fail due to missing HTTP mock
	tokens, err := credSvc.GetOAuthTokens(ctx, projectID, ProviderAnthropic)
	if err != nil {
		t.Fatalf("Failed to get OAuth tokens: %v", err)
	}

	// Since we can't mock the HTTP client here, we expect to get the old token back
	// In a production scenario with a real refresh token, this would return new tokens
	if tokens.AccessToken != "old-access-token" {
		t.Errorf("Expected old access token, got %s", tokens.AccessToken)
	}
}

func TestCredentialInfo_IncludesExpiresAt(t *testing.T) {
	// Create in-memory store
	st := setupTestStore(t)

	// Create config with encryption key (must be 32 bytes for AES-256)
	cfg := &config.Config{
		EncryptionKey: []byte("test-key-32-bytes-long-123456789"),
	}

	// Create credential service
	credSvc, err := NewCredentialService(st, cfg)
	if err != nil {
		t.Fatalf("Failed to create credential service: %v", err)
	}

	ctx := context.Background()
	projectID := "test-project"

	// Create an OAuth credential with expiration time
	expiresAt := time.Now().Add(24 * time.Hour)
	oauthTokens := &OAuthCredential{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    expiresAt,
	}
	_, err = credSvc.SetOAuthTokens(ctx, projectID, ProviderAnthropic, "Anthropic OAuth", oauthTokens)
	if err != nil {
		t.Fatalf("Failed to set OAuth tokens: %v", err)
	}

	// Get credential info
	info, err := credSvc.Get(ctx, projectID, ProviderAnthropic)
	if err != nil {
		t.Fatalf("Failed to get credential: %v", err)
	}

	// Verify expiresAt is included
	if info.ExpiresAt == nil {
		t.Errorf("Expected expiresAt to be set, but it was nil")
	} else {
		// Allow for small time differences due to processing time
		timeDiff := info.ExpiresAt.Sub(expiresAt).Abs()
		if timeDiff > time.Second {
			t.Errorf("Expected expiresAt to be %v, got %v (diff: %v)", expiresAt, *info.ExpiresAt, timeDiff)
		}
	}

	// Create an API key credential (should not have expiresAt)
	_, err = credSvc.SetAPIKey(ctx, projectID, ProviderOpenAI, "OpenAI API Key", "sk-test-123")
	if err != nil {
		t.Fatalf("Failed to set API key: %v", err)
	}

	info, err = credSvc.Get(ctx, projectID, ProviderOpenAI)
	if err != nil {
		t.Fatalf("Failed to get credential: %v", err)
	}

	// Verify expiresAt is NOT included for API key credentials
	if info.ExpiresAt != nil {
		t.Errorf("Expected expiresAt to be nil for API key credential, but it was %v", *info.ExpiresAt)
	}
}

func TestDirectToken_StoredWithOneYearExpiration(t *testing.T) {
	// Create in-memory store
	st := setupTestStore(t)

	// Create config with encryption key
	cfg := &config.Config{
		EncryptionKey: []byte("test-key-32-bytes-long-123456789"),
	}

	// Create credential service
	credSvc, err := NewCredentialService(st, cfg)
	if err != nil {
		t.Fatalf("Failed to create credential service: %v", err)
	}

	ctx := context.Background()
	projectID := "test-project"

	// Simulate storing a direct token (like from 'claude setup-token')
	directToken := "sk-ant-oat0-test-token-12345"
	expiresAt := time.Now().Add(365 * 24 * time.Hour) // 1 year

	oauthTokens := &OAuthCredential{
		AccessToken: directToken,
		TokenType:   "Bearer",
		ExpiresAt:   expiresAt,
		// No refresh token for direct tokens
	}

	info, err := credSvc.SetOAuthTokens(ctx, projectID, ProviderAnthropic, "Anthropic Direct Token", oauthTokens)
	if err != nil {
		t.Fatalf("Failed to set OAuth tokens: %v", err)
	}

	// Verify credential info includes expiration
	if info.ExpiresAt == nil {
		t.Errorf("Expected expiresAt to be set for direct token")
	}

	// Retrieve the tokens and verify
	tokens, err := credSvc.GetOAuthTokens(ctx, projectID, ProviderAnthropic)
	if err != nil {
		t.Fatalf("Failed to get OAuth tokens: %v", err)
	}

	// Verify the access token is the direct token
	if tokens.AccessToken != directToken {
		t.Errorf("Expected access token to be %s, got %s", directToken, tokens.AccessToken)
	}

	// Verify no refresh token
	if tokens.RefreshToken != "" {
		t.Errorf("Expected no refresh token for direct token, got %s", tokens.RefreshToken)
	}

	// Verify expiration is approximately 1 year (allow 1 minute variance)
	expectedExpiry := time.Now().Add(365 * 24 * time.Hour)
	timeDiff := tokens.ExpiresAt.Sub(expectedExpiry).Abs()
	if timeDiff > time.Minute {
		t.Errorf("Expected expiration ~1 year from now, got %v (diff: %v)", tokens.ExpiresAt, timeDiff)
	}
}

func TestRefreshBackoff_PreventsRepeatedAttempts(t *testing.T) {
	// Create in-memory store
	st := setupTestStore(t)

	// Create config with encryption key
	cfg := &config.Config{
		EncryptionKey:     []byte("test-key-32-bytes-long-123456789"),
		AnthropicClientID: "test-client-id",
	}

	// Create credential service
	credSvc, err := NewCredentialService(st, cfg)
	if err != nil {
		t.Fatalf("Failed to create credential service: %v", err)
	}

	ctx := context.Background()
	projectID := "test-project"

	// Create an expired OAuth credential
	expiredTime := time.Now().Add(-1 * time.Hour)
	oauthTokens := &OAuthCredential{
		AccessToken:  "expired-token",
		RefreshToken: "invalid-refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    expiredTime,
	}
	_, err = credSvc.SetOAuthTokens(ctx, projectID, ProviderAnthropic, "Anthropic OAuth", oauthTokens)
	if err != nil {
		t.Fatalf("Failed to set OAuth tokens: %v", err)
	}

	// First call: should attempt refresh and fail (will try to call Anthropic API)
	tokens1, err := credSvc.GetOAuthTokens(ctx, projectID, ProviderAnthropic)
	if err != nil {
		t.Fatalf("Failed to get OAuth tokens: %v", err)
	}

	// Should return the expired token since refresh failed
	if tokens1.AccessToken != "expired-token" {
		t.Errorf("Expected expired token, got %s", tokens1.AccessToken)
	}

	// Verify backoff was recorded
	credSvc.refreshFailMutex.RLock()
	lastFail, hasFailed := credSvc.lastRefreshFail[ProviderAnthropic]
	credSvc.refreshFailMutex.RUnlock()

	if !hasFailed {
		t.Error("Expected refresh failure to be recorded")
	}

	// Second call immediately after: should skip refresh due to backoff
	tokens2, err := credSvc.GetOAuthTokens(ctx, projectID, ProviderAnthropic)
	if err != nil {
		t.Fatalf("Failed to get OAuth tokens: %v", err)
	}

	// Should still return the expired token
	if tokens2.AccessToken != "expired-token" {
		t.Errorf("Expected expired token, got %s", tokens2.AccessToken)
	}

	// Verify the last fail time hasn't changed (no new attempt)
	credSvc.refreshFailMutex.RLock()
	lastFail2 := credSvc.lastRefreshFail[ProviderAnthropic]
	credSvc.refreshFailMutex.RUnlock()

	if !lastFail2.Equal(lastFail) {
		t.Error("Expected backoff to prevent new refresh attempt")
	}
}

func TestGetAllDecrypted_WithExpiredToken_AttemptsRefresh(t *testing.T) {
	// Create in-memory store
	st := setupTestStore(t)

	// Create config with encryption key
	cfg := &config.Config{
		EncryptionKey:     []byte("test-key-32-bytes-long-123456789"),
		AnthropicClientID: "test-client-id",
	}

	// Create credential service
	credSvc, err := NewCredentialService(st, cfg)
	if err != nil {
		t.Fatalf("Failed to create credential service: %v", err)
	}

	ctx := context.Background()
	projectID := "test-project"

	// Create an expired OAuth credential
	expiredTime := time.Now().Add(-1 * time.Hour)
	oauthTokens := &OAuthCredential{
		AccessToken:  "expired-access-token",
		RefreshToken: "invalid-refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    expiredTime,
	}
	_, err = credSvc.SetOAuthTokens(ctx, projectID, ProviderAnthropic, "Anthropic OAuth", oauthTokens)
	if err != nil {
		t.Fatalf("Failed to set OAuth tokens: %v", err)
	}

	// GetAllDecrypted should trigger auto-refresh via GetOAuthTokens
	envVars, err := credSvc.GetAllDecrypted(ctx, projectID)
	if err != nil {
		t.Fatalf("Failed to get all decrypted: %v", err)
	}

	// Should still return the credential even though refresh failed
	if len(envVars) != 1 {
		t.Fatalf("Expected 1 credential, got %d", len(envVars))
	}

	// Should use the OAuth-specific env var
	if envVars[0].EnvVar != "CLAUDE_CODE_OAUTH_TOKEN" {
		t.Errorf("Expected CLAUDE_CODE_OAUTH_TOKEN, got %s", envVars[0].EnvVar)
	}

	// Should return the expired token since refresh failed
	if envVars[0].Value != "expired-access-token" {
		t.Errorf("Expected expired-access-token, got %s", envVars[0].Value)
	}
}

func TestSetCustomCredential_BlankValuesPreserveExistingSecrets(t *testing.T) {
	st := setupTestStore(t)
	cfg := &config.Config{
		EncryptionKey: []byte("test-key-32-bytes-long-123456789"),
	}

	credSvc, err := NewCredentialService(st, cfg)
	if err != nil {
		t.Fatalf("Failed to create credential service: %v", err)
	}

	ctx := context.Background()
	projectID := "test-project"

	created, err := credSvc.SetCustomCredential(ctx, projectID, "", "", "", []SecretEnvVar{
		{Key: "FOO_TOKEN", Value: "foo-secret"},
		{Key: "BAR_TOKEN", Value: "bar-secret"},
	}, false)
	if err != nil {
		t.Fatalf("Failed to create custom credential: %v", err)
	}

	updated, err := credSvc.SetCustomCredential(ctx, projectID, created.ID, "", "", []SecretEnvVar{
		{Key: "FOO_TOKEN", Value: ""},
		{Key: "BAR_TOKEN", Value: "updated-bar-secret"},
		{Key: "BAZ_TOKEN", Value: ""},
	}, false)
	if err != nil {
		t.Fatalf("Failed to update custom credential: %v", err)
	}

	if len(updated.EnvKeys) != 2 || updated.EnvKeys[0] != "FOO_TOKEN" || updated.EnvKeys[1] != "BAR_TOKEN" {
		t.Fatalf("expected updated env keys to preserve populated keys, got %#v", updated.EnvKeys)
	}

	stored, err := st.GetCredentialByIDForProject(ctx, projectID, created.ID)
	if err != nil {
		t.Fatalf("Failed to load stored credential: %v", err)
	}
	data, err := credSvc.getSecretData(stored)
	if err != nil {
		t.Fatalf("Failed to decrypt stored credential: %v", err)
	}
	if len(data.EnvVars) != 2 {
		t.Fatalf("expected 2 stored env vars, got %d", len(data.EnvVars))
	}
	if data.EnvVars[0].Key != "FOO_TOKEN" || data.EnvVars[0].Value != "foo-secret" {
		t.Fatalf("expected FOO_TOKEN secret to be preserved, got %#v", data.EnvVars[0])
	}
	if data.EnvVars[1].Key != "BAR_TOKEN" || data.EnvVars[1].Value != "updated-bar-secret" {
		t.Fatalf("expected BAR_TOKEN secret to be updated, got %#v", data.EnvVars[1])
	}
}

func TestDirectToken_NoRefreshAttemptWhenExpired(t *testing.T) {
	// Create in-memory store
	st := setupTestStore(t)

	// Create config with encryption key
	cfg := &config.Config{
		EncryptionKey: []byte("test-key-32-bytes-long-123456789"),
	}

	// Create credential service
	credSvc, err := NewCredentialService(st, cfg)
	if err != nil {
		t.Fatalf("Failed to create credential service: %v", err)
	}

	ctx := context.Background()
	projectID := "test-project"

	// Create an EXPIRED direct token (1 year ago)
	expiredTime := time.Now().Add(-365 * 24 * time.Hour)
	directToken := "sk-ant-oat0-expired-token"

	oauthTokens := &OAuthCredential{
		AccessToken: directToken,
		TokenType:   "Bearer",
		ExpiresAt:   expiredTime,
		// No refresh token for direct tokens
	}
	_, err = credSvc.SetOAuthTokens(ctx, projectID, ProviderAnthropic, "Anthropic Direct Token", oauthTokens)
	if err != nil {
		t.Fatalf("Failed to set OAuth tokens: %v", err)
	}

	// Get tokens - should NOT attempt refresh because there's no refresh token
	tokens, err := credSvc.GetOAuthTokens(ctx, projectID, ProviderAnthropic)
	if err != nil {
		t.Fatalf("Failed to get OAuth tokens: %v", err)
	}

	// Should return the expired direct token
	if tokens.AccessToken != directToken {
		t.Errorf("Expected %s, got %s", directToken, tokens.AccessToken)
	}

	// Verify no backoff was recorded (since no refresh was attempted)
	credSvc.refreshFailMutex.RLock()
	_, hasFailed := credSvc.lastRefreshFail[ProviderAnthropic]
	credSvc.refreshFailMutex.RUnlock()

	if hasFailed {
		t.Error("Expected no refresh failure to be recorded for direct token without refresh token")
	}
}
