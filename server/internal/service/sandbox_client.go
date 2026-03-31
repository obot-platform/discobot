package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/obot-platform/discobot/server/internal/sandbox"
	"github.com/obot-platform/discobot/server/internal/sandbox/sandboxapi"
	"github.com/obot-platform/discobot/server/internal/store"
)

// Retry configuration for sandbox requests.
// Uses aggressive initial backoff to catch container startup quickly.
const (
	retryInitialDelay = 50 * time.Millisecond // Start very aggressive
	retryMaxDelay     = 2 * time.Second       // Cap delay
	retryMaxAttempts  = 15                    // Total attempts
	retryMultiplier   = 2.0                   // Double each time
)

// CredentialFetcher is a function that retrieves credentials for a session.
// It looks up the session to get the project ID, then fetches decrypted credentials.
type CredentialFetcher func(ctx context.Context, sessionID string) ([]CredentialEnvVar, error)

// MakeCredentialFetcher creates a CredentialFetcher that looks up credentials for a session.
// Also injects env vars from the session's active env set, if envSetSvc is provided.
// Returns nil if credSvc is nil (credentials will not be fetched).
func MakeCredentialFetcher(s *store.Store, credSvc *CredentialService, envSetSvc *EnvSetService) CredentialFetcher {
	if credSvc == nil {
		return nil
	}
	return func(ctx context.Context, sessionID string) ([]CredentialEnvVar, error) {
		sess, err := s.GetSessionByID(ctx, sessionID)
		if err != nil {
			return nil, fmt.Errorf("failed to get session: %w", err)
		}
		creds, err := credSvc.GetAllDecrypted(ctx, sess.ProjectID)
		if err != nil {
			return nil, err
		}

		// Append active env set vars if configured
		if envSetSvc != nil {
			envVars, err := envSetSvc.GetEnvVarsForSession(ctx, sessionID)
			if err != nil {
				// Log and skip rather than failing the entire credential fetch
				log.Printf("Warning: failed to get env set vars for session %s: %v", sessionID, err)
			} else {
				for k, v := range envVars {
					creds = append(creds, CredentialEnvVar{
						EnvVar:   k,
						Value:    v,
						Provider: "env-set",
						AuthType: "env_set",
					})
				}
			}
		}

		return creds, nil
	}
}

// SandboxChatClient handles communication with the agent running in a sandbox.
type SandboxChatClient struct {
	provider          sandbox.Provider
	credentialFetcher CredentialFetcher

	// gitUserName and gitUserEmail are sent on every request via
	// X-Discobot-Git-User-Name and X-Discobot-Git-User-Email headers.
	gitUserName  string
	gitUserEmail string
}

// SandboxChatStartError preserves a structured non-2xx response from the
// sandbox POST /chat endpoint so API handlers can forward it cleanly.
type SandboxChatStartError struct {
	StatusCode   int
	ErrorCode    string
	Message      string
	CompletionID string
	QuestionID   string
}

func (e *SandboxChatStartError) Error() string {
	message := e.Message
	if message == "" {
		message = e.ErrorCode
	}
	if message == "" {
		message = "sandbox chat start failed"
	}
	return fmt.Sprintf("sandbox returned status %d: %s", e.StatusCode, message)
}

// SandboxChatClientConfig contains optional configuration for a SandboxChatClient.
// All fields are optional — pass nil for defaults.
type SandboxChatClientConfig struct {
	// GitUserName is the git user.name sent on every request.
	GitUserName string
	// GitUserEmail is the git user.email sent on every request.
	GitUserEmail string
}

// NewSandboxChatClient creates a new sandbox chat client.
// The fetcher parameter is optional - if nil, credentials will not be automatically fetched.
// config is optional — pass nil to create a bare client without git or session config.
func NewSandboxChatClient(provider sandbox.Provider, fetcher CredentialFetcher, config *SandboxChatClientConfig) *SandboxChatClient {
	c := &SandboxChatClient{
		provider:          provider,
		credentialFetcher: fetcher,
	}
	if config != nil {
		c.gitUserName = config.GitUserName
		c.gitUserEmail = config.GitUserEmail
	}
	return c
}

// threadsURL returns the agent-go thread collection route.
func (c *SandboxChatClient) threadsURL() string {
	return "http://sandbox/threads"
}

// threadURL returns a URL under the agent-go thread-scoped route.
// For example, threadURL("thread-123", "/chat") returns "http://sandbox/threads/thread-123/chat".
// The HTTP client's transport routes the request to the correct sandbox container
// based on the sessionID passed to getHTTPClient — the URL host is always "sandbox".
func (c *SandboxChatClient) threadURL(threadID, path string) string {
	return c.threadsURL() + "/" + url.PathEscape(threadID) + path
}

// isRetryableError checks if an error is a transient protocol error that should be retried.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	// Connection refused - container not ready yet
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	// Connection reset - container restarting
	if errors.Is(err, syscall.ECONNRESET) {
		return true
	}
	// EOF - connection closed before response (container still starting)
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	// Check for common network error patterns in the error string
	errStr := err.Error()
	return strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "Connection reset by peer") ||
		strings.Contains(errStr, "no such host") ||
		strings.Contains(errStr, "i/o timeout") ||
		strings.Contains(errStr, "vsock connect") ||
		strings.Contains(errStr, "EOF")
}

// isRetryableStatus checks if an HTTP status code should trigger a retry.
func isRetryableStatus(statusCode int) bool {
	return statusCode >= 500 && statusCode < 600
}

// retryWithBackoff executes fn with exponential backoff on retryable errors.
// Returns the result of fn or the last error after max attempts.
func retryWithBackoff[T any](ctx context.Context, fn func() (T, int, error)) (T, error) {
	var zero T
	delay := retryInitialDelay

	for attempt := 1; attempt <= retryMaxAttempts; attempt++ {
		result, statusCode, err := fn()

		// Success
		if err == nil && !isRetryableStatus(statusCode) {
			return result, nil
		}

		// Check if we should retry
		shouldRetry := isRetryableError(err) || isRetryableStatus(statusCode)
		if !shouldRetry || attempt == retryMaxAttempts {
			if err != nil {
				return zero, err
			}
			return result, nil
		}

		// Wait before retry, respecting context cancellation
		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-time.After(delay):
		}

		// Increase delay for next iteration
		delay = min(time.Duration(float64(delay)*retryMultiplier), retryMaxDelay)
	}

	return zero, fmt.Errorf("max retry attempts exceeded")
}

// SSELine represents a raw SSE event from the sandbox.
// The content is passed through without parsing - the sandbox
// is expected to send data in AI SDK UIMessage Stream format.
type SSELine struct {
	// ID is the optional SSE event id.
	ID string
	// Event is the optional SSE event name.
	Event string
	// Data is the raw JSON payload (without "data: " prefix).
	Data string
	// Done indicates the upstream emitted a terminal done marker.
	// Agent-go no longer sends this, but the field is kept so the client can
	// tolerate older stream sources during the transition.
	Done bool
}

// getHTTPClient returns an HTTP client configured for the sandbox.
// This uses the provider's HTTPClient which handles transport-level details
// (TCP for Docker, vsock for vz, mock transport for testing).
func (c *SandboxChatClient) getHTTPClient(ctx context.Context, sessionID string) (*http.Client, error) {
	return c.provider.HTTPClient(ctx, sessionID)
}

// RequestOptions contains optional parameters for sandbox requests.
type RequestOptions struct {
	// SkipCredentials opts out of automatic credential fetching.
	// By default, credentials are fetched and sent with requests.
	SkipCredentials bool

	// Reasoning controls extended thinking using a string reasoning level such as
	// "auto", "low", "medium", "high", "xhigh", "none", "default", or
	// "" for model/provider default behavior.
	Reasoning string

	// Mode is the permission mode: "plan" for planning mode, "" for default.
	Mode string

	// LastEventID forwards the client's SSE resume cursor when reconnecting.
	LastEventID string
}

// applyRequestAuth sets Authorization and credentials headers on a request.
// Credentials are automatically fetched unless SkipCredentials is set.
func (c *SandboxChatClient) applyRequestAuth(ctx context.Context, req *http.Request, sessionID string, opts *RequestOptions) error {
	// Add Authorization header with Bearer token
	secret, err := c.provider.GetSecret(ctx, sessionID)
	if err == nil && secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}

	// Auto-fetch credentials if fetcher is set and not skipped
	skipCreds := opts != nil && opts.SkipCredentials
	if c.credentialFetcher != nil && !skipCreds {
		creds, err := c.credentialFetcher(ctx, sessionID)
		if err != nil {
			// Log warning but don't fail - credentials are optional
			fmt.Printf("Warning: failed to fetch credentials for session %s: %v\n", sessionID, err)
		} else if len(creds) > 0 {
			credJSON, err := json.Marshal(creds)
			if err != nil {
				return fmt.Errorf("failed to marshal credentials: %w", err)
			}
			req.Header.Set("X-Discobot-Credentials", string(credJSON))
		}
	}

	// Add git user config headers from client configuration
	if c.gitUserName != "" {
		req.Header.Set("X-Discobot-Git-User-Name", c.gitUserName)
	}
	if c.gitUserEmail != "" {
		req.Header.Set("X-Discobot-Git-User-Email", c.gitUserEmail)
	}

	return nil
}

// StartChat sends messages to the sandbox and returns the initial completion metadata.
func (c *SandboxChatClient) StartChat(ctx context.Context, sessionID, threadID string, messages json.RawMessage, model string, opts *RequestOptions) (*sandboxapi.ChatStartedResponse, error) {
	// Build the request body once - pass messages through as-is
	reasoning := ""
	mode := ""
	if opts != nil {
		reasoning = opts.Reasoning
		mode = opts.Mode
	}
	reqBody := sandboxapi.ChatRequest{
		Messages:  messages,
		Model:     model,
		Reasoning: reasoning,
		Mode:      mode,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			return nil, 0, err
		}

		req, err := http.NewRequestWithContext(ctx, "POST", c.threadURL(threadID, "/chat"), bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		if err := c.applyRequestAuth(ctx, req, sessionID, opts); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}

		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		var conflict sandboxapi.ChatConflictResponse
		if err := json.Unmarshal(body, &conflict); err == nil && conflict.Error != "" {
			return nil, &SandboxChatStartError{
				StatusCode:   resp.StatusCode,
				ErrorCode:    conflict.Error,
				CompletionID: conflict.CompletionID,
			}
		}
		var turnConflict sandboxapi.ChatTurnStateConflictResponse
		if err := json.Unmarshal(body, &turnConflict); err == nil && turnConflict.Error != "" {
			return nil, &SandboxChatStartError{
				StatusCode:   resp.StatusCode,
				ErrorCode:    turnConflict.Error,
				Message:      turnConflict.Message,
				QuestionID:   turnConflict.QuestionID,
				CompletionID: turnConflict.CompletionID,
			}
		}
		var apiErr sandboxapi.ErrorResponse
		if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Error != "" {
			return nil, &SandboxChatStartError{
				StatusCode: resp.StatusCode,
				ErrorCode:  apiErr.Error,
			}
		}
		return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read sandbox response: %w", err)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return &sandboxapi.ChatStartedResponse{Status: "started"}, nil
	}

	var started sandboxapi.ChatStartedResponse
	if err := json.Unmarshal(body, &started); err != nil {
		return nil, fmt.Errorf("failed to decode sandbox response: %w", err)
	}
	if started.Status == "" {
		started.Status = "started"
	}
	return &started, nil
}

// SendMessages sends messages to the sandbox and returns a channel of raw SSE lines.
// The sandbox is expected to respond with SSE events in AI SDK UIMessage Stream format.
// Messages and responses are passed through without parsing.
// Retries with exponential backoff on connection errors and 5xx responses.
func (c *SandboxChatClient) SendMessages(ctx context.Context, sessionID, threadID string, messages json.RawMessage, model string, opts *RequestOptions) (<-chan SSELine, error) {
	if _, err := c.StartChat(ctx, sessionID, threadID, messages, model, opts); err != nil {
		return nil, err
	}

	// POST returns 202 Accepted - now GET the SSE stream
	return c.GetStream(ctx, sessionID, threadID, opts)
}

// GetStream connects to the sandbox's long-lived SSE stream for a thread.
// Retries with exponential backoff on connection errors and 5xx responses.
func (c *SandboxChatClient) GetStream(ctx context.Context, sessionID, threadID string, opts *RequestOptions) (<-chan SSELine, error) {
	// Use retry logic to handle transient connection errors during container startup
	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			// Don't retry on sandbox not running - let caller handle reconciliation
			return nil, 0, err
		}
		client.Timeout = 0 // SSE stream - no timeout

		// URL host is ignored - the client's transport handles routing to the sandbox
		req, err := http.NewRequestWithContext(ctx, "GET", c.threadURL(threadID, "/chat/stream"), nil)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Accept", "text/event-stream")
		if opts != nil && opts.LastEventID != "" {
			req.Header.Set("Last-Event-ID", opts.LastEventID)
		}

		if err := c.applyRequestAuth(ctx, req, sessionID, opts); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}

		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
	}

	// Create channel for raw SSE lines
	lineCh := make(chan SSELine, 100)

	// Start goroutine to read SSE lines and pass through
	go func() {
		defer close(lineCh)
		defer func() { _ = resp.Body.Close() }()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		current := SSELine{}
		hasCurrent := false
		emitCurrent := func() bool {
			if !hasCurrent {
				return true
			}
			if current.Event == "done" {
				current.Done = true
			}
			if current.Data == "[DONE]" {
				current.Event = "done"
				current.Data = `{}`
				current.Done = true
			}
			select {
			case lineCh <- current:
				current = SSELine{}
				hasCurrent = false
				return true
			case <-ctx.Done():
				return false
			}
		}

		for scanner.Scan() {
			line := scanner.Text()

			if strings.HasPrefix(line, ":") {
				continue
			}
			if line == "" {
				if !emitCurrent() {
					return
				}
				continue
			}

			hasCurrent = true
			switch {
			case strings.HasPrefix(line, "id: "):
				current.ID = line[4:]
			case strings.HasPrefix(line, "event: "):
				current.Event = line[7:]
			case strings.HasPrefix(line, "data: "):
				data := line[6:]
				if current.Data == "" {
					current.Data = data
				} else {
					current.Data += "\n" + data
				}
			}
		}
		if hasCurrent {
			if !emitCurrent() {
				return
			}
		}

		if err := scanner.Err(); err != nil && ctx.Err() == nil {
			log.Printf("[SandboxChatClient] Error reading chat stream for session %s: %v", sessionID, err)
			errorData, marshalErr := json.Marshal(struct {
				Type      string `json:"type"`
				ErrorText string `json:"errorText"`
			}{
				Type:      "error",
				ErrorText: fmt.Sprintf("failed to read chat stream: %v", err),
			})
			if marshalErr == nil {
				select {
				case lineCh <- SSELine{Data: string(errorData)}:
				case <-ctx.Done():
				}
			}
		}
	}()

	return lineCh, nil
}

// ListThreads retrieves all threads from the sandbox agent.
func (c *SandboxChatClient) ListThreads(ctx context.Context, sessionID string) (*sandboxapi.ListThreadsResponse, error) {
	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			return nil, 0, err
		}

		req, err := http.NewRequestWithContext(ctx, "GET", c.threadsURL(), nil)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}
		if err := c.applyRequestAuth(ctx, req, sessionID, nil); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}
		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list threads: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
	}

	var result sandboxapi.ListThreadsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetThread retrieves a specific thread from the sandbox agent.
func (c *SandboxChatClient) GetThread(ctx context.Context, sessionID, threadID string) (*sandboxapi.Thread, error) {
	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			return nil, 0, err
		}

		req, err := http.NewRequestWithContext(ctx, "GET", c.threadURL(threadID, ""), nil)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}
		if err := c.applyRequestAuth(ctx, req, sessionID, nil); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}
		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get thread: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
	}

	var result sandboxapi.Thread
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// CreateThread creates a new thread in the sandbox agent.
func (c *SandboxChatClient) CreateThread(ctx context.Context, sessionID string, reqBody *sandboxapi.CreateThreadRequest) (*sandboxapi.Thread, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			return nil, 0, err
		}

		req, err := http.NewRequestWithContext(ctx, "POST", c.threadsURL(), bytes.NewReader(body))
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if err := c.applyRequestAuth(ctx, req, sessionID, nil); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}
		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create thread: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
	}

	var result sandboxapi.Thread
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// UpdateThread updates a thread in the sandbox agent.
func (c *SandboxChatClient) UpdateThread(ctx context.Context, sessionID, threadID string, reqBody *sandboxapi.UpdateThreadRequest) (*sandboxapi.Thread, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			return nil, 0, err
		}

		req, err := http.NewRequestWithContext(ctx, "PUT", c.threadURL(threadID, ""), bytes.NewReader(body))
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if err := c.applyRequestAuth(ctx, req, sessionID, nil); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}
		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update thread: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
	}

	var result sandboxapi.Thread
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// DeleteThread removes a thread from the sandbox agent.
func (c *SandboxChatClient) DeleteThread(ctx context.Context, sessionID, threadID string) (*sandboxapi.DeleteThreadResponse, error) {
	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			return nil, 0, err
		}

		req, err := http.NewRequestWithContext(ctx, "DELETE", c.threadURL(threadID, ""), nil)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}
		if err := c.applyRequestAuth(ctx, req, sessionID, nil); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}
		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to delete thread: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
	}

	var result sandboxapi.DeleteThreadResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetChatStatus retrieves the completion status from the sandbox.
// Calls GET /threads/{id}/chat/status which returns {"isRunning": bool}.
func (c *SandboxChatClient) GetChatStatus(ctx context.Context, sessionID string) (*sandboxapi.ChatStatusResponse, error) {
	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			return nil, 0, err
		}

		req, err := http.NewRequestWithContext(ctx, "GET", c.threadURL(sessionID, "/chat/status"), nil)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}

		if err := c.applyRequestAuth(ctx, req, sessionID, &RequestOptions{SkipCredentials: true}); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}

		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get chat status: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
	}

	var status sandboxapi.ChatStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("failed to decode chat status: %w", err)
	}
	return &status, nil
}

// CancelCompletion cancels an in-progress completion in the sandbox.
// Returns ErrNoActiveCompletion if no completion is active (409 status).
// Retries with exponential backoff on connection errors.
func (c *SandboxChatClient) CancelCompletion(ctx context.Context, sessionID, threadID string) (*CancelCompletionResponse, error) {
	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			return nil, 0, err
		}

		req, err := http.NewRequestWithContext(ctx, "POST", c.threadURL(threadID, "/chat/cancel"), nil)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}

		if err := c.applyRequestAuth(ctx, req, sessionID, nil); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}

		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to cancel completion: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusConflict {
		return nil, ErrNoActiveCompletion
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
	}

	var result CancelCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// ============================================================================
// AskUserQuestion Methods
// ============================================================================

// GetQuestion returns the pending AskUserQuestion for a specific question ID.
func (c *SandboxChatClient) GetQuestion(ctx context.Context, sessionID, threadID string, toolUseID string) (*sandboxapi.PendingQuestionResponse, error) {
	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			return nil, 0, err
		}

		url := c.threadURL(threadID, "/chat/question/"+toolUseID)

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}

		if err := c.applyRequestAuth(ctx, req, sessionID, &RequestOptions{SkipCredentials: true}); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}

		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get question: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
	}

	var result sandboxapi.PendingQuestionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// AnswerQuestion submits the user's answer to a pending AskUserQuestion.
func (c *SandboxChatClient) AnswerQuestion(ctx context.Context, sessionID, threadID string, req *sandboxapi.AnswerQuestionRequest) (*sandboxapi.AnswerQuestionResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			return nil, 0, err
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", c.threadURL(threadID, "/chat/answer/"+req.ToolUseID), bytes.NewReader(body))
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")

		if err := c.applyRequestAuth(ctx, httpReq, sessionID, &RequestOptions{SkipCredentials: true}); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, 0, err
		}

		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to answer question: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNoActiveCompletion
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
	}

	var result sandboxapi.AnswerQuestionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// ============================================================================
// File System Methods
// ============================================================================

// ListFiles lists directory contents in the sandbox.
// Retries with exponential backoff on connection errors and 5xx responses.
func (c *SandboxChatClient) ListFiles(ctx context.Context, sessionID string, path string, includeHidden bool) (*sandboxapi.ListFilesResponse, error) {
	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			return nil, 0, err
		}

		params := url.Values{}
		params.Set("path", path)
		if includeHidden {
			params.Set("hidden", "true")
		}

		req, err := http.NewRequestWithContext(ctx, "GET", "http://sandbox/files?"+params.Encode(), nil)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}

		if err := c.applyRequestAuth(ctx, req, sessionID, nil); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}

		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
	}

	var result sandboxapi.ListFilesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// SearchFiles performs a fuzzy search over workspace files in the sandbox.
// Retries with exponential backoff on connection errors and 5xx responses.
func (c *SandboxChatClient) SearchFiles(ctx context.Context, sessionID string, query string, limit int) (*sandboxapi.SearchFilesResponse, error) {
	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			return nil, 0, err
		}

		params := url.Values{}
		params.Set("q", query)
		params.Set("limit", fmt.Sprintf("%d", limit))

		req, err := http.NewRequestWithContext(ctx, "GET", "http://sandbox/files/search?"+params.Encode(), nil)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}

		if err := c.applyRequestAuth(ctx, req, sessionID, nil); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}

		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to search files: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
	}

	var result sandboxapi.SearchFilesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// ReadFile reads file content from the sandbox.
// Retries with exponential backoff on connection errors and 5xx responses.
func (c *SandboxChatClient) ReadFile(ctx context.Context, sessionID string, path string) (*sandboxapi.ReadFileResponse, error) {
	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			return nil, 0, err
		}

		params := url.Values{}
		params.Set("path", path)

		req, err := http.NewRequestWithContext(ctx, "GET", "http://sandbox/files/read?"+params.Encode(), nil)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}

		if err := c.applyRequestAuth(ctx, req, sessionID, nil); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}

		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
	}

	var result sandboxapi.ReadFileResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// WriteFile writes file content to the sandbox.
// Retries with exponential backoff on connection errors and 5xx responses.
func (c *SandboxChatClient) WriteFile(ctx context.Context, sessionID string, req *sandboxapi.WriteFileRequest) (*sandboxapi.WriteFileResponse, error) {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			return nil, 0, err
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", "http://sandbox/files/write", bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")

		if err := c.applyRequestAuth(ctx, httpReq, sessionID, nil); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, 0, err
		}

		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to write file: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
	}

	var result sandboxapi.WriteFileResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// DeleteFile deletes a file or directory in the sandbox.
// Retries with exponential backoff on connection errors and 5xx responses.
func (c *SandboxChatClient) DeleteFile(ctx context.Context, sessionID string, req *sandboxapi.DeleteFileRequest) (*sandboxapi.DeleteFileResponse, error) {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			return nil, 0, err
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", "http://sandbox/files/delete", bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")

		if err := c.applyRequestAuth(ctx, httpReq, sessionID, nil); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, 0, err
		}

		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to delete file: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
	}

	var result sandboxapi.DeleteFileResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// RenameFile renames/moves a file or directory in the sandbox.
// Retries with exponential backoff on connection errors and 5xx responses.
func (c *SandboxChatClient) RenameFile(ctx context.Context, sessionID string, req *sandboxapi.RenameFileRequest) (*sandboxapi.RenameFileResponse, error) {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			return nil, 0, err
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST", "http://sandbox/files/rename", bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")

		if err := c.applyRequestAuth(ctx, httpReq, sessionID, nil); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(httpReq)
		if err != nil {
			return nil, 0, err
		}

		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to rename file: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
	}

	var result sandboxapi.RenameFileResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetUserInfo retrieves the default user info from the sandbox.
// This is used to determine which user to run terminal sessions as.
// Retries with exponential backoff on connection errors and 5xx responses.
func (c *SandboxChatClient) GetUserInfo(ctx context.Context, sessionID string) (*sandboxapi.UserResponse, error) {
	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			return nil, 0, err
		}

		req, err := http.NewRequestWithContext(ctx, "GET", "http://sandbox/user", nil)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}

		if err := c.applyRequestAuth(ctx, req, sessionID, nil); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}

		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
	}

	var result sandboxapi.UserResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetModels retrieves available models from the Claude API via the sandbox.
// This calls the actual Anthropic API through the Agent API to get current model availability.
// Retries with exponential backoff on connection errors and 5xx responses.
func (c *SandboxChatClient) GetModels(ctx context.Context, sessionID string) (*sandboxapi.ModelsResponse, error) {
	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			return nil, 0, err
		}

		req, err := http.NewRequestWithContext(ctx, "GET", c.threadURL(sessionID, "/models"), nil)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}

		if err := c.applyRequestAuth(ctx, req, sessionID, nil); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}

		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
	}

	var result sandboxapi.ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetDiff retrieves diff information from the sandbox.
// If path is non-empty, returns a single file diff.
// If format is "files", returns just file paths.
// Otherwise returns full diff with patches.
// The agent-api calculates the merge-base automatically.
// Retries with exponential backoff on connection errors and 5xx responses.
func (c *SandboxChatClient) GetDiff(ctx context.Context, sessionID string, path string, format string) (any, error) {
	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			return nil, 0, err
		}

		// Build URL with query parameters
		params := url.Values{}
		if path != "" {
			params.Set("path", path)
		}
		if format != "" {
			params.Set("format", format)
		}

		requestURL := "http://sandbox/diff"
		if encoded := params.Encode(); encoded != "" {
			requestURL += "?" + encoded
		}

		req, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}

		if err := c.applyRequestAuth(ctx, req, sessionID, nil); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}

		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get diff: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
	}

	// Decode based on request parameters
	if path != "" {
		var result sandboxapi.SingleFileDiffResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
		return &result, nil
	}

	if format == "files" {
		var result sandboxapi.DiffFilesResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
		return &result, nil
	}

	var result sandboxapi.DiffResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// GetCommits retrieves a serialized commit replay bundle from the sandbox for commits since a parent.
// Returns the bundle string and commit count on success, or an error on failure.
// Retries with exponential backoff on connection errors and 5xx responses.
func (c *SandboxChatClient) GetCommits(ctx context.Context, sessionID string, parentCommit string) (*sandboxapi.CommitsResponse, error) {
	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			return nil, 0, err
		}

		// Build URL with query parameter
		url := "http://sandbox/commits?parent=" + parentCommit

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}

		if err := c.applyRequestAuth(ctx, req, sessionID, nil); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}

		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get commits: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Check for error responses (400, 404, 409)
	if resp.StatusCode != http.StatusOK {
		var errResp sandboxapi.CommitsErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
		}
		return nil, fmt.Errorf("commits error (%s): %s", errResp.Error, errResp.Message)
	}

	var result sandboxapi.CommitsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// ============================================================================
// Hook Methods
// ============================================================================

// GetHooksStatus retrieves hook evaluation status from the sandbox.
// Retries with exponential backoff on connection errors and 5xx responses.
func (c *SandboxChatClient) GetHooksStatus(ctx context.Context, sessionID string) (*sandboxapi.HooksStatusResponse, error) {
	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			return nil, 0, err
		}

		req, err := http.NewRequestWithContext(ctx, "GET", "http://sandbox/hooks/status", nil)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}

		if err := c.applyRequestAuth(ctx, req, sessionID, nil); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}

		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get hooks status: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
	}

	var result sandboxapi.HooksStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetHookOutput retrieves the output log for a specific hook from the sandbox.
// Retries with exponential backoff on connection errors and 5xx responses.
func (c *SandboxChatClient) GetHookOutput(ctx context.Context, sessionID, hookID string) (*sandboxapi.HookOutputResponse, error) {
	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			return nil, 0, err
		}

		url := fmt.Sprintf("http://sandbox/hooks/%s/output", hookID)
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}

		if err := c.applyRequestAuth(ctx, req, sessionID, nil); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}

		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get hook output: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
	}

	var result sandboxapi.HookOutputResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// RerunHook manually reruns a specific hook in the sandbox.
func (c *SandboxChatClient) RerunHook(ctx context.Context, sessionID, hookID string) (*sandboxapi.HookRerunResponse, error) {
	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			return nil, 0, err
		}

		url := fmt.Sprintf("http://sandbox/hooks/%s/rerun", hookID)
		req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}

		if err := c.applyRequestAuth(ctx, req, sessionID, nil); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}

		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to rerun hook: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
	}

	var result sandboxapi.HookRerunResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// ============================================================================
// Service Methods
// ============================================================================

// ListServices retrieves all services from the sandbox.
// Retries with exponential backoff on connection errors and 5xx responses.
func (c *SandboxChatClient) ListServices(ctx context.Context, sessionID string) (*sandboxapi.ListServicesResponse, error) {
	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			return nil, 0, err
		}

		req, err := http.NewRequestWithContext(ctx, "GET", "http://sandbox/services", nil)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}

		if err := c.applyRequestAuth(ctx, req, sessionID, nil); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}

		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
	}

	var result sandboxapi.ListServicesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// StartService starts a service in the sandbox.
// Returns immediately with status "starting" (202 Accepted).
// Retries with exponential backoff on connection errors and 5xx responses.
func (c *SandboxChatClient) StartService(ctx context.Context, sessionID string, serviceID string) (*sandboxapi.StartServiceResponse, error) {
	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			return nil, 0, err
		}

		url := "http://sandbox/services/" + serviceID + "/start"
		req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}

		if err := c.applyRequestAuth(ctx, req, sessionID, nil); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}

		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start service: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// 202 Accepted is success, also handle 200 OK
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
	}

	var result sandboxapi.StartServiceResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// StopService stops a service in the sandbox.
// Retries with exponential backoff on connection errors and 5xx responses.
func (c *SandboxChatClient) StopService(ctx context.Context, sessionID string, serviceID string) (*sandboxapi.StopServiceResponse, error) {
	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			return nil, 0, err
		}

		url := "http://sandbox/services/" + serviceID + "/stop"
		req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}

		if err := c.applyRequestAuth(ctx, req, sessionID, nil); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}

		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to stop service: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
	}

	var result sandboxapi.StopServiceResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetServiceOutput connects to the sandbox's SSE stream for service output.
// Returns a channel of raw SSE lines. The channel is closed when the service
// stops or the context is cancelled.
// Retries with exponential backoff on connection errors and 5xx responses.
func (c *SandboxChatClient) GetServiceOutput(ctx context.Context, sessionID string, serviceID string) (<-chan SSELine, error) {
	resp, err := retryWithBackoff(ctx, func() (*http.Response, int, error) {
		client, err := c.getHTTPClient(ctx, sessionID)
		if err != nil {
			return nil, 0, err
		}
		client.Timeout = 0 // SSE stream - no timeout

		url := "http://sandbox/services/" + serviceID + "/output"
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Accept", "text/event-stream")

		if err := c.applyRequestAuth(ctx, req, sessionID, nil); err != nil {
			return nil, 0, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, 0, err
		}

		return resp, resp.StatusCode, nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get service output: %w", err)
	}

	// 404 means service not found
	if resp.StatusCode == http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("service not found: %s", string(body))
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("sandbox returned status %d: %s", resp.StatusCode, string(body))
	}

	// Create channel for raw SSE lines
	lineCh := make(chan SSELine, 100)

	// Start goroutine to read SSE lines and pass through
	go func() {
		defer close(lineCh)
		defer func() { _ = resp.Body.Close() }()

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()

			// Skip empty lines and comments
			if line == "" || strings.HasPrefix(line, ":") {
				continue
			}

			// Pass through SSE data lines
			if strings.HasPrefix(line, "data: ") {
				data := line[6:]

				// Check for [DONE] signal
				if data == "[DONE]" {
					select {
					case lineCh <- SSELine{Done: true}:
					case <-ctx.Done():
					}
					return
				}

				// Pass through raw data without parsing
				select {
				case lineCh <- SSELine{Data: data}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return lineCh, nil
}
