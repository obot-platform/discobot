package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// OpenAI Codex OAuth endpoints
	codexAuthURL           = "https://auth.openai.com/oauth/authorize"
	codexTokenURL          = "https://auth.openai.com/oauth/token"
	codexDeviceUserCodeURL = "https://auth.openai.com/api/accounts/deviceauth/usercode"
	codexDevicePollURL     = "https://auth.openai.com/api/accounts/deviceauth/token"
	CodexDevicePageURL     = "https://auth.openai.com/codex/device"
	CodexDeviceCallbackURI = "https://auth.openai.com/deviceauth/callback"
)

// CodexProvider handles OpenAI Codex OAuth 2.0 with PKCE.
type CodexProvider struct {
	ClientID string
	Scopes   []string
}

type CodexDeviceCodeResponse struct {
	DeviceAuthID string `json:"device_auth_id"`
	UserCode     string `json:"user_code"`
	Interval     string `json:"interval"`
}

type CodexDevicePollResponse struct {
	AuthorizationCode string `json:"authorization_code,omitempty"`
	CodeVerifier      string `json:"code_verifier,omitempty"`
}

// NewCodexProvider creates a new Codex OAuth provider.
func NewCodexProvider(clientID string) *CodexProvider {
	return &CodexProvider{
		ClientID: clientID,
		Scopes:   []string{"openid", "profile", "email", "offline_access"},
	}
}

// Authorize generates the authorization URL and PKCE challenge.
func (p *CodexProvider) Authorize(redirectURI string) (*AuthorizeResponse, error) {
	// Generate PKCE challenge
	pkce, err := GeneratePKCE()
	if err != nil {
		return nil, fmt.Errorf("failed to generate PKCE: %w", err)
	}

	// Generate state
	state, err := GenerateState()
	if err != nil {
		return nil, fmt.Errorf("failed to generate state: %w", err)
	}

	// Build authorization URL
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", p.ClientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("scope", strings.Join(p.Scopes, " "))
	params.Set("state", state)
	params.Set("code_challenge", pkce.CodeChallenge)
	params.Set("code_challenge_method", pkce.CodeChallengeMethod)
	// Extra params from opencode reference implementation
	params.Set("id_token_add_organizations", "true")
	params.Set("codex_cli_simplified_flow", "true")
	params.Set("originator", "codex_cli")

	authURL := codexAuthURL + "?" + params.Encode()

	return &AuthorizeResponse{
		URL:                 authURL,
		State:               state,
		Verifier:            pkce.CodeVerifier,
		CodeChallenge:       pkce.CodeChallenge,
		CodeChallengeMethod: pkce.CodeChallengeMethod,
	}, nil
}

// RequestDeviceCode initiates the Codex device-code flow.
func (p *CodexProvider) RequestDeviceCode(ctx context.Context) (*CodexDeviceCodeResponse, error) {
	bodyJSON := fmt.Sprintf(`{"client_id":%q}`, p.ClientID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, codexDeviceUserCodeURL, strings.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("device code request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device code request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var deviceResp CodexDeviceCodeResponse
	if err := json.Unmarshal(body, &deviceResp); err != nil {
		return nil, fmt.Errorf("failed to parse device code response: %w", err)
	}

	return &deviceResp, nil
}

// PollDeviceCode checks whether the user has completed the Codex device flow.
// It returns the parsed poll response along with the HTTP status code.
func (p *CodexProvider) PollDeviceCode(ctx context.Context, deviceAuthID, userCode string) (*CodexDevicePollResponse, int, error) {
	bodyJSON := fmt.Sprintf(`{"device_auth_id":%q,"user_code":%q}`, deviceAuthID, userCode)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, codexDevicePollURL, strings.NewReader(bodyJSON))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("poll request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		return &CodexDevicePollResponse{}, resp.StatusCode, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("poll request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var pollResp CodexDevicePollResponse
	if err := json.Unmarshal(body, &pollResp); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to parse poll response: %w", err)
	}

	return &pollResp, resp.StatusCode, nil
}

// Exchange exchanges an authorization code for tokens.
func (p *CodexProvider) Exchange(ctx context.Context, code, redirectURI, codeVerifier string) (*TokenResponse, error) {
	// Build token request
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", p.ClientID)
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)
	data.Set("code_verifier", codeVerifier)

	req, err := http.NewRequestWithContext(ctx, "POST", codexTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	// Calculate expiration time if ExpiresIn is provided
	if tokenResp.ExpiresIn > 0 {
		tokenResp.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	return &tokenResp, nil
}

// Refresh refreshes an access token using a refresh token.
func (p *CodexProvider) Refresh(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	if refreshToken == "" {
		return nil, fmt.Errorf("refresh token is required")
	}

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("client_id", p.ClientID)
	data.Set("refresh_token", refreshToken)

	req, err := http.NewRequestWithContext(ctx, "POST", codexTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	if tokenResp.ExpiresIn > 0 {
		tokenResp.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}
	if tokenResp.RefreshToken == "" {
		tokenResp.RefreshToken = refreshToken
	}

	return &tokenResp, nil
}
