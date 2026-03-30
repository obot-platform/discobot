// Package server implements the HTTP API server mode for agent-api.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/obot-platform/discobot/agent-go/agent"
	"github.com/obot-platform/discobot/agent-go/agentimpl"
	"github.com/obot-platform/discobot/agent-go/internal/config"
	"github.com/obot-platform/discobot/agent-go/internal/credentials"
	"github.com/obot-platform/discobot/agent-go/internal/handler"
	"github.com/obot-platform/discobot/agent-go/internal/hooks"
	"github.com/obot-platform/discobot/agent-go/internal/middleware"
	"github.com/obot-platform/discobot/agent-go/internal/routes"
	"github.com/obot-platform/discobot/agent-go/internal/services"
	"github.com/obot-platform/discobot/agent-go/message"
	"github.com/obot-platform/discobot/agent-go/providers"
	"github.com/obot-platform/discobot/agent-go/thread"
	"github.com/obot-platform/discobot/agent-go/tools"

	// Embed the API browser HTML.
	staticFiles "github.com/obot-platform/discobot/agent-go/cmd/agent-api/static"
)

// Run starts the HTTP API server and blocks until SIGINT/SIGTERM.
func Run(cfg *config.Config) {
	// ── Credential manager ───────────────────────────────────────────────────
	credMgr := credentials.NewManager()

	// ── Provider registry ────────────────────────────────────────────────────
	// Providers are built on demand from current credentials when first needed.
	// No startup registration required — credentials arrive per-request via
	// X-Discobot-Credentials and are held in the credential manager.
	reg := providers.NewProviderRegistry(credMgr)

	// ── Thread store ─────────────────────────────────────────────────────────
	store := thread.NewStore(cfg.ThreadsDir)

	// ── Tools executor ───────────────────────────────────────────────────────
	// threadID is empty here; the executor's threadID is only used for naming
	// output spill files and is set per-turn by tools that need it.
	exec := tools.New(cfg.AgentCwd, cfg.DataDir, "")
	exec.SetBashEnvAllowlist(cfg.BashEnvAllowlist)
	exec.SetEnvLookup(func(key string) string {
		if cred := credMgr.Get(key); cred != nil {
			return cred.Value
		}
		return ""
	})
	exec.SetEnvSnapshot(func() map[string]string {
		return credMgr.Snapshot()
	})

	// ── DefaultAgent ─────────────────────────────────────────────────────────
	mcpCfg := agentimpl.NewMCPConfig(
		cfg.MCPOAuthRedirectBase,
		cfg.SessionID,
		cfg.DiscobotServerURL,
		cfg.DiscobotProjectID,
	)
	a := agentimpl.NewDefaultAgent(store, reg, exec, cfg.AgentCwd, mcpCfg)

	// ── CompletionManager ────────────────────────────────────────────────────
	completions := agent.NewCompletionManager(a)

	// ── Hook manager ─────────────────────────────────────────────────────────
	var hookMgr *hooks.Manager
	if cfg.HooksEnabled {
		hookMgr = hooks.NewManager(cfg.AgentCwd, cfg.SessionID)
		if err := hookMgr.Init(); err != nil {
			log.Printf("warn: hooks init: %v", err)
		}
		if hookMgr.HasFileHooks() {
			hookMgr.SetReprompt(func(threadID, hookMessage string) error {
				_, err := completions.Chat(threadID, agent.PromptRequest{
					UserParts: []message.UIPart{message.UITextPart{Text: hookMessage}},
				})
				if err != nil {
					log.Printf("hooks: failed to start re-prompt: %v", err)
				}
				return err
			})
			completions.AddCompletionListener(hookMgr)
		}
	}

	// ── Service manager ──────────────────────────────────────────────────────
	svcMgr := services.NewManager()

	// ── HTTP handler ─────────────────────────────────────────────────────────
	h := handler.New(cfg.AgentCwd, completions, hookMgr, svcMgr, a)

	// ── Router ───────────────────────────────────────────────────────────────
	r := chi.NewRouter()

	// Standard middleware (no global timeout — SSE and long AI calls need
	// unbounded connection lifetimes).
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Recoverer)

	// Auth: validates Bearer token against DISCOBOT_SECRET hash.
	r.Use(middleware.Auth(cfg.SecretHash))

	// Credentials: applies X-Discobot-Credentials env vars and git user config.
	r.Use(middleware.Credentials(credMgr))

	// Register all agent API routes (also populates the global routes registry).
	h.RegisterRoutes(r)

	// ── Meta routes ──────────────────────────────────────────────────────────

	// GET /api/ui — embedded API browser HTML (same UI as the main server).
	r.Get("/api/ui", func(w http.ResponseWriter, _ *http.Request) {
		content, err := staticFiles.Files.ReadFile("api-ui.html")
		if err != nil {
			http.Error(w, "API UI not found", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(content)
	})

	// GET /api/routes — returns route metadata for the API browser.
	r.Get("/api/routes", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(routes.All())
	})

	// ── Idle timeout ─────────────────────────────────────────────────────────
	// Exit the process after IdleTimeout with no active completions.
	// This lets the container orchestrator restart a fresh instance rather than
	// keeping a stale one alive indefinitely.
	if cfg.IdleTimeout > 0 {
		go watchIdle(completions, cfg.IdleTimeout)
	}

	// ── HTTP server ──────────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: r,
		// No read/write/idle timeouts — SSE and long-running AI calls need
		// connections that can stay open for minutes or longer.
	}

	go func() {
		log.Printf("agent-api: listening on :%d (cwd: %s)", cfg.Port, cfg.AgentCwd)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("agent-api: server error: %v", err)
		}
	}()

	// ── Graceful shutdown ────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("agent-api: shutting down...")

	// Close MCP server connections.
	a.Close()

	// Give in-flight requests up to 5 seconds to complete.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("agent-api: shutdown error: %v", err)
	}

	log.Println("agent-api: stopped")
}

// watchIdle exits the process after idleTimeout elapses with no active completions.
func watchIdle(completions *agent.CompletionManager, idleTimeout time.Duration) {
	ticker := time.NewTicker(idleTimeout / 2)
	defer ticker.Stop()

	lastActive := time.Now()

	for range ticker.C {
		threads, err := completions.ListThreads()
		if err != nil {
			continue
		}

		active := false
		for _, threadID := range threads {
			if completions.ActiveCompletionID(threadID) != "" {
				active = true
				break
			}
		}

		if active {
			lastActive = time.Now()
			continue
		}

		if time.Since(lastActive) >= idleTimeout {
			log.Printf("agent-api: idle timeout (%s) reached, exiting", idleTimeout)
			os.Exit(0)
		}
	}
}
