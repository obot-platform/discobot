package cli

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	codexAuthURL       = "https://auth.openai.com/oauth/authorize"
	codexTokenURL      = "https://auth.openai.com/oauth/token"
	codexCallbackAddr  = "localhost:1455"
	codexRedirectURI   = "http://localhost:1455/auth/callback"
	codexDefaultClient = "app_EMoamEEZ73f0CkXaXp7hrann"

	codexDeviceUserCodeURL = "https://auth.openai.com/api/accounts/deviceauth/usercode"
	codexDevicePollURL     = "https://auth.openai.com/api/accounts/deviceauth/token"
	codexDevicePageURL     = "https://auth.openai.com/codex/device"
	codexDeviceCallbackURI = "https://auth.openai.com/deviceauth/callback"
)

// RunLogin handles the "login <provider> [--headless]" subcommand.
func RunLogin(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: login codex [--headless]")
	}
	headless := false
	filtered := args[:0]
	for _, a := range args {
		if a == "--headless" {
			headless = true
		} else {
			filtered = append(filtered, a)
		}
	}
	switch filtered[0] {
	case "codex":
		if headless {
			return runCodexLoginHeadless()
		}
		return runCodexLoginBrowser()
	default:
		return fmt.Errorf("unknown provider %q — supported: codex", filtered[0])
	}
}

// runCodexLoginBrowser performs the browser-based PKCE OAuth flow.
func runCodexLoginBrowser() error {
	clientID := os.Getenv("CODEX_CLIENT_ID")
	if clientID == "" {
		clientID = codexDefaultClient
	}

	verifier, challenge, err := generatePKCE()
	if err != nil {
		return fmt.Errorf("generate PKCE: %w", err)
	}
	state, err := randomBase64(32)
	if err != nil {
		return fmt.Errorf("generate state: %w", err)
	}

	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", clientID)
	params.Set("redirect_uri", codexRedirectURI)
	params.Set("scope", "openid profile email offline_access")
	params.Set("state", state)
	params.Set("code_challenge", challenge)
	params.Set("code_challenge_method", "S256")
	params.Set("id_token_add_organizations", "true")
	params.Set("codex_cli_simplified_flow", "true")
	params.Set("originator", "codex_cli")
	authURL := codexAuthURL + "?" + params.Encode()

	l, err := net.Listen("tcp", codexCallbackAddr)
	if err != nil {
		return fmt.Errorf("could not listen on %s (is another process using it?): %w", codexCallbackAddr, err)
	}

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/auth/callback" {
				http.NotFound(w, r)
				return
			}
			if e := r.URL.Query().Get("error"); e != "" {
				desc := r.URL.Query().Get("error_description")
				errCh <- fmt.Errorf("authorization denied: %s — %s", e, desc)
				http.Error(w, "Authorization failed. You can close this tab.", http.StatusBadRequest)
				return
			}
			if r.URL.Query().Get("state") != state {
				errCh <- fmt.Errorf("state mismatch — possible CSRF")
				http.Error(w, "State mismatch. You can close this tab.", http.StatusBadRequest)
				return
			}
			code := r.URL.Query().Get("code")
			if code == "" {
				errCh <- fmt.Errorf("no code in callback")
				http.Error(w, "No code received. You can close this tab.", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<!doctype html><html><head><title>Login successful</title></head>`+
				`<body style="font-family:sans-serif;text-align:center;padding:3em">`+
				`<h2>Login successful</h2><p>You can close this tab and return to your terminal.</p>`+
				`</body></html>`)
			codeCh <- code
		}),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	go func() {
		_ = srv.Serve(l)
	}()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	fmt.Fprintln(os.Stderr, "Opening browser for OpenAI authorization...")
	fmt.Fprintf(os.Stderr, "If the browser does not open, visit:\n  %s\n\n", authURL)
	openBrowser(authURL)

	fmt.Fprintln(os.Stderr, "Waiting for authorization...")

	select {
	case code := <-codeCh:
		tok, err := exchangeCodexCode(clientID, code, verifier, codexRedirectURI)
		if err != nil {
			return fmt.Errorf("token exchange: %w", err)
		}
		printCodexLoginInstructions(tok)
		return nil
	case err := <-errCh:
		return err
	case <-time.After(5 * time.Minute):
		return fmt.Errorf("timed out waiting for authorization")
	}
}

// runCodexLoginHeadless performs the device code (headless) OAuth flow.
// No browser redirect is needed — the user visits a URL and enters a code.
func runCodexLoginHeadless() error {
	clientID := os.Getenv("CODEX_CLIENT_ID")
	if clientID == "" {
		clientID = codexDefaultClient
	}

	// Step 1: request a device code.
	bodyJSON := fmt.Sprintf(`{"client_id":%q}`, clientID)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, codexDeviceUserCodeURL, strings.NewReader(bodyJSON))
	if err != nil {
		return fmt.Errorf("device auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("device auth: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("device auth failed (%d): %s", resp.StatusCode, string(body))
	}

	var deviceData struct {
		DeviceAuthID string `json:"device_auth_id"`
		UserCode     string `json:"user_code"`
		Interval     string `json:"interval"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&deviceData); err != nil {
		return fmt.Errorf("parse device auth response: %w", err)
	}

	// Determine polling interval (default 5s, minimum 1s).
	pollIntervalSec, _ := strconv.Atoi(deviceData.Interval)
	if pollIntervalSec < 1 {
		pollIntervalSec = 5
	}
	pollInterval := time.Duration(pollIntervalSec)*time.Second + 3*time.Second // match opencode's safety margin

	fmt.Fprintf(os.Stderr, "\nOpen this URL in your browser:\n  %s\n\n", codexDevicePageURL)
	fmt.Fprintf(os.Stderr, "Enter this code: %s\n\n", deviceData.UserCode)
	fmt.Fprintln(os.Stderr, "Waiting for authorization (5 minute timeout)...")

	// Step 2: poll until the user completes the flow.
	timeout := time.After(5 * time.Minute)
	for {
		select {
		case <-timeout:
			return fmt.Errorf("timed out waiting for device authorization")
		case <-time.After(pollInterval):
		}

		pollBodyJSON := fmt.Sprintf(`{"device_auth_id":%q,"user_code":%q}`, deviceData.DeviceAuthID, deviceData.UserCode)
		pollReq, err := http.NewRequestWithContext(context.Background(), http.MethodPost, codexDevicePollURL, strings.NewReader(pollBodyJSON))
		if err != nil {
			return fmt.Errorf("poll request: %w", err)
		}
		pollReq.Header.Set("Content-Type", "application/json")

		pollResp, err := http.DefaultClient.Do(pollReq)
		if err != nil {
			return fmt.Errorf("poll: %w", err)
		}

		if pollResp.StatusCode == http.StatusOK {
			var pollData struct {
				AuthorizationCode string `json:"authorization_code"`
				CodeVerifier      string `json:"code_verifier"`
			}
			if err := json.NewDecoder(pollResp.Body).Decode(&pollData); err != nil {
				_ = pollResp.Body.Close()
				return fmt.Errorf("parse poll response: %w", err)
			}
			_ = pollResp.Body.Close()

			// Step 3: exchange the authorization code for tokens.
			tok, err := exchangeCodexCode(clientID, pollData.AuthorizationCode, pollData.CodeVerifier, codexDeviceCallbackURI)
			if err != nil {
				return fmt.Errorf("token exchange: %w", err)
			}
			printCodexLoginInstructions(tok)
			return nil
		}

		_ = pollResp.Body.Close()

		// 403 and 404 mean "still pending" — keep polling.
		if pollResp.StatusCode != http.StatusForbidden && pollResp.StatusCode != http.StatusNotFound {
			return fmt.Errorf("unexpected poll response: %d", pollResp.StatusCode)
		}
	}
}

// printCodexLoginInstructions prints the env var export commands the user
// needs to run to configure their session after a successful Codex login.
func printCodexLoginInstructions(tok tokenResponse) {
	accountID := extractAccountID(tok)
	fmt.Fprintf(os.Stderr, "\nLogin successful. Run the following to configure your session:\n\n")
	fmt.Printf("export CODEX_TOKEN=%s\n", tok.AccessToken)
	if accountID != "" {
		fmt.Printf("export CHATGPT_ACCOUNT_ID=%s\n", accountID)
	} else {
		fmt.Fprintf(os.Stderr, "\nNote: Could not extract account ID from token.\n")
		fmt.Fprintf(os.Stderr, "If requests fail, set CHATGPT_ACCOUNT_ID to your ChatGPT account ID.\n")
	}
}

// --- JWT account ID extraction ---

// extractAccountID parses the id_token (preferred) or access_token JWT and
// returns the chatgpt_account_id claim, following the same precedence as the
// opencode Codex plugin.
func extractAccountID(tok tokenResponse) string {
	if tok.IDToken != "" {
		if claims := parseJWTClaims(tok.IDToken); claims != nil {
			if id := extractAccountIDFromClaims(claims); id != "" {
				return id
			}
		}
	}
	if claims := parseJWTClaims(tok.AccessToken); claims != nil {
		return extractAccountIDFromClaims(claims)
	}
	return ""
}

// extractAccountIDFromClaims checks chatgpt_account_id, the nested
// "https://api.openai.com/auth" claim, and organizations[0].id — in that order.
func extractAccountIDFromClaims(claims map[string]any) string {
	if id, ok := claims["chatgpt_account_id"].(string); ok && id != "" {
		return id
	}
	if auth, ok := claims["https://api.openai.com/auth"].(map[string]any); ok {
		if id, ok := auth["chatgpt_account_id"].(string); ok && id != "" {
			return id
		}
	}
	if orgs, ok := claims["organizations"].([]any); ok && len(orgs) > 0 {
		if org, ok := orgs[0].(map[string]any); ok {
			if id, ok := org["id"].(string); ok && id != "" {
				return id
			}
		}
	}
	return ""
}

// parseJWTClaims base64url-decodes the payload section of a JWT and returns
// the claims as a map. Returns nil on any parsing error.
func parseJWTClaims(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil
	}
	return claims
}

// --- Token exchange ---

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func exchangeCodexCode(clientID, code, verifier, redirectURI string) (tokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", clientID)
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)
	data.Set("code_verifier", verifier)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, codexTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return tokenResponse{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return tokenResponse{}, err
	}

	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return tokenResponse{}, fmt.Errorf("parse response: %w", err)
	}
	if tok.Error != "" {
		return tokenResponse{}, fmt.Errorf("%s: %s", tok.Error, tok.ErrorDesc)
	}
	if tok.AccessToken == "" {
		return tokenResponse{}, fmt.Errorf("no access token in response (status %d)", resp.StatusCode)
	}
	return tok, nil
}

// generatePKCE returns a (verifier, S256-challenge) pair.
func generatePKCE() (verifier, challenge string, err error) {
	verifier, err = randomBase64(48)
	if err != nil {
		return
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return
}

func randomBase64(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	_ = cmd.Start()
}
