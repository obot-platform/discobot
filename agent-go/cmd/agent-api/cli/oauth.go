package cli

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/obot-platform/discobot/agent-go/agentimpl"
)

// watchMCPOAuth polls the MCP manager's server status and prints authorization
// URLs to stderr when a server requires OAuth. Runs until ctx is cancelled.
func watchMCPOAuth(ctx context.Context, a *agentimpl.DefaultAgent) {
	notified := make(map[string]bool)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			mgr := a.MCPManager()
			if mgr == nil {
				continue
			}
			for _, info := range mgr.Status() {
				if info.OAuthURL != "" && !notified[info.Name] {
					notified[info.Name] = true
					fmt.Fprintf(os.Stderr, "\nMCP server %q requires authorization.\n", info.Name)
					fmt.Fprintf(os.Stderr, "Open this URL in your browser:\n  %s\n", info.OAuthURL)
					fmt.Fprint(os.Stderr, formatPromptHint()) // re-print the prompt hint
				}
			}
		}
	}
}

// startOAuthServer starts a local HTTP server on a random loopback port to
// receive OAuth callbacks from MCP servers.
//
// Returns the server's base URL (e.g. "http://127.0.0.1:12345") and the
// *http.Server. The caller should call wireOAuthCallbacks after constructing
// the agent, and close the server when done.
//
// On failure (e.g. no ports available), returns ("", nil) — MCP OAuth will
// still work if MCPOAuthRedirectBase is set via environment variable.
func startOAuthServer() (string, *http.Server) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Printf("cli: could not start OAuth callback server: %v", err)
		return "", nil
	}

	port := l.Addr().(*net.TCPAddr).Port
	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	srv := &http.Server{
		// Handler is set by wireOAuthCallbacks after the agent is constructed.
		Handler: http.NotFoundHandler(),
	}

	go func() {
		if err := srv.Serve(l); err != nil && err != http.ErrServerClosed {
			log.Printf("cli: OAuth server error: %v", err)
		}
	}()

	return base, srv
}

// wireOAuthCallbacks replaces the OAuth server's handler with one that routes
// authorization callbacks to the correct MCP server connection.
//
// Expected path: /sessions/{sessionID}/mcp/{serverName}/callback?code=...&state=...
func wireOAuthCallbacks(srv *http.Server, a *agentimpl.DefaultAgent) {
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Parse: /sessions/{sessionID}/mcp/{serverName}/callback
		// After stripping "/sessions/": "{sessionID}/mcp/{serverName}/callback"
		tail := strings.TrimPrefix(r.URL.Path, "/sessions/")
		parts := strings.SplitN(tail, "/", 4)
		// parts: [sessionID, "mcp", serverName, "callback"]
		if len(parts) < 4 || parts[1] != "mcp" || parts[3] != "callback" {
			http.NotFound(w, r)
			return
		}
		serverName := parts[2]

		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code parameter", http.StatusBadRequest)
			return
		}
		state := r.URL.Query().Get("state")

		mgr := a.MCPManager()
		if mgr == nil {
			http.Error(w, "MCP manager not available", http.StatusServiceUnavailable)
			return
		}

		if err := mgr.SubmitOAuthCode(serverName, code, state); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html>
<html>
<head><title>Authorization successful</title></head>
<body style="font-family:sans-serif;text-align:center;padding:3em">
  <h2>Authorization successful</h2>
  <p>You can close this tab and return to your terminal.</p>
</body>
</html>`)
	})
}
