package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/obot-platform/discobot/server/internal/middleware"
	"github.com/obot-platform/discobot/server/internal/oauth"
	"github.com/obot-platform/discobot/server/internal/providers"
	"github.com/obot-platform/discobot/server/internal/service"
)

// CreateCredentialRequest is the request body for creating/updating a credential
type createCredentialEnvVarRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type CreateCredentialRequest struct {
	Provider     string                          `json:"provider,omitempty"`
	CredentialID string                          `json:"credentialId,omitempty"`
	Name         string                          `json:"name"`
	Description  string                          `json:"description,omitempty"`
	AuthType     string                          `json:"authType"` // "api_key", "id", or "oauth"
	APIKey       string                          `json:"apiKey,omitempty"`
	EnvVars      []createCredentialEnvVarRequest `json:"envVars,omitempty"`
	AgentVisible *bool                           `json:"agentVisible,omitempty"`
	Inactive     *bool                           `json:"inactive,omitempty"`
}

// GetCredentialTypes returns the credential choices used by the current UI.
func (h *Handler) GetCredentialTypes(w http.ResponseWriter, _ *http.Request) {
	h.JSON(w, http.StatusOK, map[string]any{"credentialTypes": providers.GetCredentialTypes()})
}

// ListCredentials returns all credentials for a project (safe info only)
func (h *Handler) ListCredentials(w http.ResponseWriter, r *http.Request) {
	projectID := middleware.GetProjectID(r.Context())

	credentials, err := h.credentialService.List(r.Context(), projectID)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, "Failed to list credentials")
		return
	}

	h.JSON(w, http.StatusOK, map[string]any{"credentials": credentials})
}

// CreateCredential creates or updates a credential
func (h *Handler) CreateCredential(w http.ResponseWriter, r *http.Request) {
	projectID := middleware.GetProjectID(r.Context())

	var req CreateCredentialRequest
	if err := h.DecodeJSON(r, &req); err != nil {
		h.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name != "" {
		req.Name = strings.TrimSpace(req.Name)
	}

	agentVisible := false
	if req.AgentVisible != nil {
		agentVisible = *req.AgentVisible
	}
	inactive := false

	var existingCredential *service.CredentialInfo
	if req.CredentialID != "" {
		info, err := h.credentialService.GetByID(r.Context(), projectID, req.CredentialID)
		if err != nil {
			if errors.Is(err, service.ErrCredentialNotFound) {
				h.Error(w, http.StatusNotFound, "Credential not found")
				return
			}
			h.Error(w, http.StatusInternalServerError, "Failed to load credential")
			return
		}
		existingCredential = info
		if req.Provider == "custom" && !strings.HasPrefix(existingCredential.Provider, "custom:") {
			h.Error(w, http.StatusBadRequest, "Credential provider mismatch")
			return
		}
		if req.Provider != "" && req.Provider != "custom" && req.Provider != existingCredential.Provider {
			h.Error(w, http.StatusBadRequest, "Credential provider mismatch")
			return
		}
		if req.Provider == "" {
			req.Provider = existingCredential.Provider
		}
		inactive = existingCredential.Inactive
	}
	if req.Inactive != nil {
		inactive = *req.Inactive
	}

	envVars := make([]service.SecretEnvVar, 0, len(req.EnvVars))
	for _, envVar := range req.EnvVars {
		envVars = append(envVars, service.SecretEnvVar{Key: envVar.Key, Value: envVar.Value})
	}

	if len(envVars) > 0 || req.Provider == "custom" || req.Provider == "" {
		info, err := h.credentialService.SetCustomCredential(r.Context(), projectID, req.CredentialID, req.Name, req.Description, envVars, agentVisible, inactive)
		if err != nil {
			if errors.Is(err, service.ErrCredentialNotFound) {
				h.Error(w, http.StatusNotFound, "Credential not found")
				return
			}
			h.Error(w, http.StatusInternalServerError, "Failed to create credential")
			return
		}
		h.JSON(w, http.StatusOK, info)
		return
	}

	if req.Provider == "" {
		h.Error(w, http.StatusBadRequest, "provider is required")
		return
	}

	if req.AuthType == "" || req.AuthType == service.AuthTypeAPIKey {
		if req.APIKey == "" {
			if req.CredentialID != "" {
				info, err := h.credentialService.UpdateMetadata(r.Context(), projectID, req.CredentialID, req.Name, req.Description, agentVisible, inactive)
				if err != nil {
					if errors.Is(err, service.ErrCredentialNotFound) {
						h.Error(w, http.StatusNotFound, "Credential not found")
						return
					}
					h.Error(w, http.StatusInternalServerError, "Failed to update credential")
					return
				}
				h.JSON(w, http.StatusOK, info)
				return
			}
			h.Error(w, http.StatusBadRequest, "api_key is required for api_key auth type")
			return
		}

		info, err := h.credentialService.SetAPIKeyWithMetadata(r.Context(), projectID, req.Provider, req.Name, req.Description, req.APIKey, agentVisible, inactive)
		if err != nil {
			if errors.Is(err, service.ErrInvalidProvider) {
				h.Error(w, http.StatusBadRequest, "Invalid provider")
				return
			}
			h.Error(w, http.StatusInternalServerError, "Failed to create credential")
			return
		}

		h.JSON(w, http.StatusOK, info)
		return
	}

	if req.AuthType == service.AuthTypeID {
		if req.APIKey == "" {
			if req.CredentialID != "" {
				info, err := h.credentialService.UpdateMetadata(r.Context(), projectID, req.CredentialID, req.Name, req.Description, agentVisible, inactive)
				if err != nil {
					if errors.Is(err, service.ErrCredentialNotFound) {
						h.Error(w, http.StatusNotFound, "Credential not found")
						return
					}
					h.Error(w, http.StatusInternalServerError, "Failed to update credential")
					return
				}
				h.JSON(w, http.StatusOK, info)
				return
			}
			h.Error(w, http.StatusBadRequest, "api_key is required for id auth type")
			return
		}

		info, err := h.credentialService.SetIDWithMetadata(r.Context(), projectID, req.Provider, req.Name, req.Description, req.APIKey, agentVisible, inactive)
		if err != nil {
			if errors.Is(err, service.ErrInvalidProvider) {
				h.Error(w, http.StatusBadRequest, "Invalid provider")
				return
			}
			h.Error(w, http.StatusInternalServerError, "Failed to create credential")
			return
		}

		h.JSON(w, http.StatusOK, info)
		return
	}

	if req.AuthType == service.AuthTypeOAuth && req.CredentialID != "" {
		info, err := h.credentialService.UpdateMetadata(r.Context(), projectID, req.CredentialID, req.Name, req.Description, agentVisible, inactive)
		if err != nil {
			if errors.Is(err, service.ErrCredentialNotFound) {
				h.Error(w, http.StatusNotFound, "Credential not found")
				return
			}
			h.Error(w, http.StatusInternalServerError, "Failed to update credential")
			return
		}
		h.JSON(w, http.StatusOK, info)
		return
	}

	h.Error(w, http.StatusBadRequest, "OAuth credentials must be set via OAuth flow endpoints")
}

// GetCredential returns a single credential.
func (h *Handler) GetCredential(w http.ResponseWriter, r *http.Request) {
	projectID := middleware.GetProjectID(r.Context())
	identifier := chi.URLParam(r, "provider")

	info, err := h.credentialService.Get(r.Context(), projectID, identifier)
	if err != nil {
		info, err = h.credentialService.GetByID(r.Context(), projectID, identifier)
	}
	if err != nil {
		if errors.Is(err, service.ErrCredentialNotFound) {
			h.Error(w, http.StatusNotFound, "Credential not found")
			return
		}
		h.Error(w, http.StatusInternalServerError, "Failed to get credential")
		return
	}

	h.JSON(w, http.StatusOK, info)
}

// DeleteCredential deletes a credential.
func (h *Handler) DeleteCredential(w http.ResponseWriter, r *http.Request) {
	projectID := middleware.GetProjectID(r.Context())
	identifier := chi.URLParam(r, "provider")

	credential, err := h.credentialService.Get(r.Context(), projectID, identifier)
	if err != nil {
		credential, err = h.credentialService.GetByID(r.Context(), projectID, identifier)
	}
	if err != nil {
		if errors.Is(err, service.ErrCredentialNotFound) {
			h.Error(w, http.StatusNotFound, "Credential not found")
			return
		}
		h.Error(w, http.StatusInternalServerError, "Failed to delete credential")
		return
	}

	if err := h.credentialService.Delete(r.Context(), projectID, credential.ID); err != nil {
		if errors.Is(err, service.ErrCredentialNotFound) {
			h.Error(w, http.StatusNotFound, "Credential not found")
			return
		}
		h.Error(w, http.StatusInternalServerError, "Failed to delete credential")
		return
	}

	h.JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type setSessionCredentialAssignmentRequest struct {
	CredentialID string `json:"credentialId"`
	AgentVisible bool   `json:"agentVisible"`
}

// ListSessionCredentialAssignments returns credentials assigned to a session.
func (h *Handler) ListSessionCredentialAssignments(w http.ResponseWriter, r *http.Request) {
	projectID := middleware.GetProjectID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	assignments, err := h.credentialService.ListSessionAssignments(r.Context(), projectID, sessionID)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, "Failed to list session credentials")
		return
	}

	h.JSON(w, http.StatusOK, map[string]any{"credentials": assignments})
}

// SetSessionCredentialAssignments replaces credentials assigned to a session.
func (h *Handler) SetSessionCredentialAssignments(w http.ResponseWriter, r *http.Request) {
	projectID := middleware.GetProjectID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	var req struct {
		Credentials []setSessionCredentialAssignmentRequest `json:"credentials"`
	}
	if err := h.DecodeJSON(r, &req); err != nil {
		h.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	assignments := make([]service.SessionCredentialAssignmentInfo, 0, len(req.Credentials))
	for _, credential := range req.Credentials {
		assignments = append(assignments, service.SessionCredentialAssignmentInfo{
			CredentialID: credential.CredentialID,
			AgentVisible: credential.AgentVisible,
		})
	}

	updated, err := h.credentialService.SetSessionAssignments(r.Context(), projectID, sessionID, assignments)
	if err != nil {
		if errors.Is(err, service.ErrCredentialNotFound) {
			h.Error(w, http.StatusNotFound, "Credential not found")
			return
		}
		h.Error(w, http.StatusInternalServerError, "Failed to set session credentials")
		return
	}

	h.JSON(w, http.StatusOK, map[string]any{"credentials": updated})
}

// RefreshCredential manually refreshes OAuth tokens for a credential
func (h *Handler) RefreshCredential(w http.ResponseWriter, r *http.Request) {
	projectID := middleware.GetProjectID(r.Context())
	provider := chi.URLParam(r, "provider")

	tokens, err := h.credentialService.RefreshOAuthTokens(r.Context(), projectID, provider)
	if err != nil {
		if errors.Is(err, service.ErrCredentialNotFound) {
			h.Error(w, http.StatusNotFound, "Credential not found")
			return
		}
		h.Error(w, http.StatusInternalServerError, "Failed to refresh token: "+err.Error())
		return
	}

	// Return success response with new expiration time
	response := map[string]any{
		"success":   true,
		"expiresAt": tokens.ExpiresAt,
	}
	if !tokens.ExpiresAt.IsZero() {
		response["expiresIn"] = int(time.Until(tokens.ExpiresAt).Seconds())
	}

	h.JSON(w, http.StatusOK, response)
}

// AnthropicExchangeRequest is the request for exchanging code for tokens
type AnthropicExchangeRequest struct {
	Code         string `json:"code"`
	CodeVerifier string `json:"verifier"`
}

// AnthropicAuthorize generates PKCE and returns OAuth URL
func (h *Handler) AnthropicAuthorize(w http.ResponseWriter, _ *http.Request) {
	provider := oauth.NewAnthropicProvider(h.cfg.AnthropicClientID)
	authResp, err := provider.Authorize()
	if err != nil {
		h.Error(w, http.StatusInternalServerError, "Failed to generate authorization URL")
		return
	}

	h.JSON(w, http.StatusOK, authResp)
}

// AnthropicExchange exchanges code for tokens or accepts direct access tokens
func (h *Handler) AnthropicExchange(w http.ResponseWriter, r *http.Request) {
	projectID := middleware.GetProjectID(r.Context())

	var req AnthropicExchangeRequest
	if err := h.DecodeJSON(r, &req); err != nil {
		h.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Code == "" {
		h.Error(w, http.StatusBadRequest, "code is required")
		return
	}

	var oauthCred *service.OAuthCredential
	var expiresAt time.Time

	// Check if this is a direct access token from 'claude setup-token'
	if strings.HasPrefix(req.Code, "sk-ant-oat0") {
		// Direct access token - store it with 1 year expiration
		expiresAt = time.Now().Add(365 * 24 * time.Hour)
		oauthCred = &service.OAuthCredential{
			AccessToken: req.Code,
			TokenType:   "Bearer",
			ExpiresAt:   expiresAt,
			// No refresh token for direct tokens
		}
	} else {
		// Regular OAuth flow - exchange authorization code for tokens
		if req.CodeVerifier == "" {
			h.Error(w, http.StatusBadRequest, "verifier is required for authorization code")
			return
		}

		provider := oauth.NewAnthropicProvider(h.cfg.AnthropicClientID)
		tokenResp, err := provider.Exchange(r.Context(), req.Code, req.CodeVerifier)
		if err != nil {
			// Return as JSON with success: false so frontend can display the error
			h.JSON(w, http.StatusOK, map[string]any{
				"success": false,
				"error":   err.Error(),
			})
			return
		}

		expiresAt = tokenResp.ExpiresAt
		oauthCred = &service.OAuthCredential{
			AccessToken:  tokenResp.AccessToken,
			RefreshToken: tokenResp.RefreshToken,
			TokenType:    tokenResp.TokenType,
			ExpiresAt:    tokenResp.ExpiresAt,
			Scope:        tokenResp.Scope,
		}
	}

	// Store the credential (works for both OAuth and direct tokens)
	info, err := h.credentialService.SetOAuthTokens(r.Context(), projectID, service.ProviderAnthropic, "Anthropic OAuth", oauthCred)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, "Failed to store credential")
		return
	}

	// Return success response with credential info
	response := map[string]any{
		"success":    true,
		"credential": info,
		"expiresAt":  expiresAt,
	}
	if !expiresAt.IsZero() {
		response["expiresIn"] = int(time.Until(expiresAt).Seconds())
	}

	h.JSON(w, http.StatusOK, response)
}

// GitHubCopilotDeviceCodeRequest is the request for initiating device flow
type GitHubCopilotDeviceCodeRequest struct {
	DeploymentType string `json:"deploymentType"` // "github.com" or "enterprise"
	EnterpriseURL  string `json:"enterpriseUrl,omitempty"`
}

// GitHubCopilotPollRequest is the request for polling device authorization
type GitHubCopilotPollRequest struct {
	DeviceCode string `json:"deviceCode"`
	Domain     string `json:"domain"`
}

// GitHubCopilotDeviceCodeResponse is the camelCase response for frontend
type GitHubCopilotDeviceCodeResponse struct {
	DeviceCode      string `json:"deviceCode"`
	UserCode        string `json:"userCode"`
	VerificationURI string `json:"verificationUri"`
	ExpiresIn       int    `json:"expiresIn"`
	Interval        int    `json:"interval"`
	Domain          string `json:"domain"`
}

// GitHubCopilotPollResponse is the response for poll requests
type GitHubCopilotPollResponse struct {
	Status string `json:"status"` // "pending", "success", or "error"
	Error  string `json:"error,omitempty"`
}

// GitHubCopilotDeviceCode initiates device flow
func (h *Handler) GitHubCopilotDeviceCode(w http.ResponseWriter, r *http.Request) {
	var req GitHubCopilotDeviceCodeRequest
	if err := h.DecodeJSON(r, &req); err != nil {
		// Allow empty body, default to github.com
		req.DeploymentType = "github.com"
	}

	// Determine domain based on deployment type
	domain := oauth.DefaultGitHubDomain
	if req.DeploymentType == "enterprise" && req.EnterpriseURL != "" {
		// Extract domain from enterprise URL
		domain = req.EnterpriseURL
		// Strip protocol if present
		if idx := strings.Index(domain, "://"); idx != -1 {
			domain = domain[idx+3:]
		}
		// Strip trailing slash and path
		if idx := strings.Index(domain, "/"); idx != -1 {
			domain = domain[:idx]
		}
	}

	provider := oauth.NewGitHubCopilotProvider(h.cfg.GitHubCopilotClientID, domain)
	deviceResp, err := provider.RequestDeviceCode(r.Context())
	if err != nil {
		h.Error(w, http.StatusInternalServerError, "Failed to request device code: "+err.Error())
		return
	}

	// Convert to camelCase for frontend
	h.JSON(w, http.StatusOK, GitHubCopilotDeviceCodeResponse{
		DeviceCode:      deviceResp.DeviceCode,
		UserCode:        deviceResp.UserCode,
		VerificationURI: deviceResp.VerificationURI,
		ExpiresIn:       deviceResp.ExpiresIn,
		Interval:        deviceResp.Interval,
		Domain:          domain,
	})
}

// GitHubCopilotPoll polls for device authorization
func (h *Handler) GitHubCopilotPoll(w http.ResponseWriter, r *http.Request) {
	projectID := middleware.GetProjectID(r.Context())

	var req GitHubCopilotPollRequest
	if err := h.DecodeJSON(r, &req); err != nil {
		h.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.DeviceCode == "" {
		h.Error(w, http.StatusBadRequest, "deviceCode is required")
		return
	}

	// Use domain from request, default to github.com
	domain := req.Domain
	if domain == "" {
		domain = oauth.DefaultGitHubDomain
	}

	provider := oauth.NewGitHubCopilotProvider(h.cfg.GitHubCopilotClientID, domain)
	pollResp, err := provider.PollForToken(r.Context(), req.DeviceCode)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, "Poll request failed: "+err.Error())
		return
	}

	// Check if authorization is still pending
	if pollResp.IsAuthorizationPending() {
		h.JSON(w, http.StatusAccepted, map[string]string{
			"status": "pending",
			"error":  "authorization_pending",
		})
		return
	}

	// Check for slow down request
	if pollResp.IsSlowDown() {
		h.JSON(w, http.StatusTooManyRequests, map[string]string{
			"status": "slow_down",
			"error":  "slow_down",
		})
		return
	}

	// Check for expired token
	if pollResp.IsExpired() {
		h.Error(w, http.StatusGone, "Device code expired")
		return
	}

	// Check for access denied
	if pollResp.IsAccessDenied() {
		h.Error(w, http.StatusForbidden, "Access denied by user")
		return
	}

	// Check for other errors
	if pollResp.Error != "" {
		h.Error(w, http.StatusBadRequest, pollResp.ErrorDesc)
		return
	}

	// We have a token! Store it
	if !pollResp.HasToken() {
		h.Error(w, http.StatusInternalServerError, "Unexpected response: no token received")
		return
	}

	oauthCred := &service.OAuthCredential{
		AccessToken:  pollResp.AccessToken,
		RefreshToken: pollResp.RefreshToken,
		TokenType:    pollResp.TokenType,
		Scope:        pollResp.Scope,
	}

	info, err := h.credentialService.SetOAuthTokens(r.Context(), projectID, service.ProviderGitHubCopilot, "GitHub Copilot", oauthCred)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, "Failed to store credential")
		return
	}

	h.JSON(w, http.StatusOK, map[string]any{
		"status":     "success",
		"credential": info,
	})
}

// GitHubDeviceCodeRequest is the request for initiating GitHub device flow
type GitHubDeviceCodeRequest struct {
	EnterpriseURL string `json:"enterpriseUrl,omitempty"`
}

// GitHubPollRequest is the request for polling GitHub device authorization
type GitHubPollRequest struct {
	DeviceCode string `json:"deviceCode"`
	Domain     string `json:"domain"`
}

// GitHubDeviceCode initiates device flow for GitHub git operations (repo scope)
func (h *Handler) GitHubDeviceCode(w http.ResponseWriter, r *http.Request) {
	var req GitHubDeviceCodeRequest
	// Allow empty body, default to github.com
	_ = h.DecodeJSON(r, &req)

	domain := oauth.DefaultGitHubDomain
	if req.EnterpriseURL != "" {
		domain = req.EnterpriseURL
		if idx := strings.Index(domain, "://"); idx != -1 {
			domain = domain[idx+3:]
		}
		if idx := strings.Index(domain, "/"); idx != -1 {
			domain = domain[:idx]
		}
	}

	if h.cfg.GitHubOAuthClientID == "" {
		h.Error(w, http.StatusServiceUnavailable, "GitHub OAuth not configured")
		return
	}

	provider := oauth.NewGitHubProvider(h.cfg.GitHubOAuthClientID, domain)
	deviceResp, err := provider.RequestDeviceCode(r.Context())
	if err != nil {
		h.Error(w, http.StatusInternalServerError, "Failed to request device code: "+err.Error())
		return
	}

	h.JSON(w, http.StatusOK, GitHubCopilotDeviceCodeResponse{
		DeviceCode:      deviceResp.DeviceCode,
		UserCode:        deviceResp.UserCode,
		VerificationURI: deviceResp.VerificationURI,
		ExpiresIn:       deviceResp.ExpiresIn,
		Interval:        deviceResp.Interval,
		Domain:          domain,
	})
}

// GitHubPoll polls for GitHub device authorization
func (h *Handler) GitHubPoll(w http.ResponseWriter, r *http.Request) {
	projectID := middleware.GetProjectID(r.Context())

	var req GitHubPollRequest
	if err := h.DecodeJSON(r, &req); err != nil {
		h.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.DeviceCode == "" {
		h.Error(w, http.StatusBadRequest, "deviceCode is required")
		return
	}

	domain := req.Domain
	if domain == "" {
		domain = oauth.DefaultGitHubDomain
	}

	provider := oauth.NewGitHubProvider(h.cfg.GitHubOAuthClientID, domain)
	pollResp, err := provider.PollForToken(r.Context(), req.DeviceCode)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, "Poll request failed: "+err.Error())
		return
	}

	if pollResp.IsAuthorizationPending() {
		h.JSON(w, http.StatusAccepted, map[string]string{"status": "pending", "error": "authorization_pending"})
		return
	}

	if pollResp.IsSlowDown() {
		h.JSON(w, http.StatusTooManyRequests, map[string]string{"status": "slow_down", "error": "slow_down"})
		return
	}

	if pollResp.IsExpired() {
		h.Error(w, http.StatusGone, "Device code expired")
		return
	}

	if pollResp.IsAccessDenied() {
		h.Error(w, http.StatusForbidden, "Access denied by user")
		return
	}

	if pollResp.Error != "" {
		h.Error(w, http.StatusBadRequest, pollResp.ErrorDesc)
		return
	}

	if !pollResp.HasToken() {
		h.Error(w, http.StatusInternalServerError, "Unexpected response: no token received")
		return
	}

	oauthCred := &service.OAuthCredential{
		AccessToken: pollResp.AccessToken,
		TokenType:   pollResp.TokenType,
		Scope:       pollResp.Scope,
		// GitHub device flow does not issue refresh tokens
	}

	info, err := h.credentialService.SetOAuthTokens(r.Context(), projectID, service.ProviderGitHub, "GitHub", oauthCred)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, "Failed to store credential")
		return
	}

	h.JSON(w, http.StatusOK, map[string]any{
		"status":     "success",
		"credential": info,
	})
}

type CodexDeviceCodeResponse struct {
	DeviceAuthID    string `json:"deviceAuthId"`
	UserCode        string `json:"userCode"`
	VerificationURI string `json:"verificationUri"`
	Interval        int    `json:"interval"`
}

type CodexPollRequest struct {
	DeviceAuthID string `json:"deviceAuthId"`
	UserCode     string `json:"userCode"`
}

// PostMCPToken stores an MCP OAuth token posted by the agent after completing
// an OAuth exchange. The token is keyed by resource URL so it can be reused
// across sessions that share the same MCP server URL.
// Body: { "url": "https://api.example.com/mcp",
//
//	"accessToken": "...", "refreshToken": "...", "expiresAt": 1234567890 }
func (h *Handler) PostMCPToken(w http.ResponseWriter, r *http.Request) {
	projectID := middleware.GetProjectID(r.Context())

	var body service.MCPTokenData
	if err := h.DecodeJSON(r, &body); err != nil {
		h.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if body.URL == "" {
		h.Error(w, http.StatusBadRequest, "url is required")
		return
	}
	if body.AccessToken == "" {
		h.Error(w, http.StatusBadRequest, "accessToken is required")
		return
	}

	if err := h.credentialService.StoreMCPToken(r.Context(), projectID, body); err != nil {
		h.Error(w, http.StatusInternalServerError, "Failed to store MCP token")
		return
	}

	h.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// CodexDeviceCode initiates the device-code flow for Codex/OpenAI OAuth.
func (h *Handler) CodexDeviceCode(w http.ResponseWriter, r *http.Request) {
	if h.cfg.CodexClientID == "" {
		h.Error(w, http.StatusServiceUnavailable, "Codex OAuth not configured")
		return
	}

	provider := oauth.NewCodexProvider(h.cfg.CodexClientID)
	deviceResp, err := provider.RequestDeviceCode(r.Context())
	if err != nil {
		h.Error(w, http.StatusInternalServerError, "Failed to request device code: "+err.Error())
		return
	}

	interval, err := strconv.Atoi(deviceResp.Interval)
	if err != nil || interval < 1 {
		interval = 5
	}

	h.JSON(w, http.StatusOK, CodexDeviceCodeResponse{
		DeviceAuthID:    deviceResp.DeviceAuthID,
		UserCode:        deviceResp.UserCode,
		VerificationURI: oauth.CodexDevicePageURL,
		Interval:        interval,
	})
}

// CodexPoll polls the device-code flow for completion and stores the resulting credential.
func (h *Handler) CodexPoll(w http.ResponseWriter, r *http.Request) {
	projectID := middleware.GetProjectID(r.Context())

	var req CodexPollRequest
	if err := h.DecodeJSON(r, &req); err != nil {
		h.Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.DeviceAuthID == "" {
		h.Error(w, http.StatusBadRequest, "deviceAuthId is required")
		return
	}
	if req.UserCode == "" {
		h.Error(w, http.StatusBadRequest, "userCode is required")
		return
	}

	provider := oauth.NewCodexProvider(h.cfg.CodexClientID)
	pollResp, statusCode, err := provider.PollDeviceCode(r.Context(), req.DeviceAuthID, req.UserCode)
	if err != nil {
		h.Error(w, http.StatusBadRequest, "Poll request failed: "+err.Error())
		return
	}
	if statusCode == http.StatusForbidden || statusCode == http.StatusNotFound {
		h.JSON(w, http.StatusAccepted, map[string]string{"status": "pending"})
		return
	}
	if pollResp.AuthorizationCode == "" {
		h.Error(w, http.StatusInternalServerError, "Unexpected response: no authorization code received")
		return
	}
	if pollResp.CodeVerifier == "" {
		h.Error(w, http.StatusInternalServerError, "Unexpected response: no code verifier received")
		return
	}

	tokenResp, err := provider.Exchange(r.Context(), pollResp.AuthorizationCode, oauth.CodexDeviceCallbackURI, pollResp.CodeVerifier)
	if err != nil {
		h.Error(w, http.StatusBadRequest, "Token exchange failed: "+err.Error())
		return
	}

	// Store the tokens as a credential
	oauthCred := &service.OAuthCredential{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		ExpiresAt:    tokenResp.ExpiresAt,
		Scope:        tokenResp.Scope,
	}

	info, err := h.credentialService.SetOAuthTokens(r.Context(), projectID, service.ProviderCodex, "OpenAI Codex", oauthCred)
	if err != nil {
		h.Error(w, http.StatusInternalServerError, "Failed to store credential")
		return
	}

	// Return credential info with token expiration
	response := map[string]any{
		"status":     "success",
		"credential": info,
		"expiresAt":  tokenResp.ExpiresAt,
	}
	if !tokenResp.ExpiresAt.IsZero() {
		response["expiresIn"] = int(time.Until(tokenResp.ExpiresAt).Seconds())
	}

	h.JSON(w, http.StatusOK, response)
}
