package handler

import (
	"encoding/json"
	"net/http"

	"github.com/obot-platform/discobot/server/internal/config"
	"github.com/obot-platform/discobot/server/internal/events"
	"github.com/obot-platform/discobot/server/internal/git"
	"github.com/obot-platform/discobot/server/internal/jobs"
	"github.com/obot-platform/discobot/server/internal/sandbox"
	"github.com/obot-platform/discobot/server/internal/service"
	"github.com/obot-platform/discobot/server/internal/startup"
	"github.com/obot-platform/discobot/server/internal/store"
	"github.com/obot-platform/discobot/server/internal/terminal"
)

const (
	sessionCookieName = "discobot_session"
	stateCookieName   = "discobot_oauth_state"
)

// Handler contains all HTTP handlers
type Handler struct {
	store               *store.Store
	cfg                 *config.Config
	authService         *service.AuthService
	credentialService   *service.CredentialService
	gitService          *service.GitService
	gitProvider         git.Provider
	sandboxProvider     sandbox.Provider
	sandboxManager      *sandbox.Manager
	sandboxService      *service.SandboxService
	sessionService      *service.SessionService
	chatService         *service.ChatService
	modelsService       *service.ModelsService
	workspaceService    *service.WorkspaceService
	projectService      *service.ProjectService
	preferenceService   *service.PreferenceService
	jobQueue            *jobs.Queue
	eventBroker         *events.Broker
	codexCallbackServer *CodexCallbackServer
	systemManager       *startup.SystemManager
	terminalManager     *terminal.Manager
}

// New creates a new Handler with the required git and sandbox providers.
func New(s *store.Store, cfg *config.Config, gitProvider git.Provider, sandboxProvider sandbox.Provider, sandboxManager *sandbox.Manager, eventBroker *events.Broker, jobQueue *jobs.Queue, systemManager *startup.SystemManager) *Handler {
	credSvc, err := service.NewCredentialService(s, cfg)
	if err != nil {
		// This should only fail if the encryption key is invalid
		panic("failed to create credential service: " + err.Error())
	}

	var gitSvc *service.GitService
	if gitProvider != nil {
		gitSvc = service.NewGitService(s, gitProvider)
	}

	// Create credential fetcher for sandbox client
	var credFetcher service.CredentialFetcher
	if credSvc != nil {
		credFetcher = service.MakeCredentialFetcher(s, credSvc)
	}

	// Create sandbox service with all dependencies
	var sandboxSvc *service.SandboxService
	if sandboxProvider != nil {
		sandboxSvc = service.NewSandboxService(s, sandboxProvider, cfg, credFetcher, eventBroker, jobQueue, nil)
	}

	// Create session service
	sessionSvc := service.NewSessionService(s, gitSvc, sandboxProvider, sandboxSvc, eventBroker, jobQueue)

	// Break circular dependency: SandboxService needs SessionInitializer (which is SessionService)
	if sandboxSvc != nil {
		sandboxSvc.SetSessionInitializer(sessionSvc)
		if gitSvc != nil {
			sandboxSvc.SetGitConfigProvider(gitSvc.GetUserConfig)
		}
	}

	// Create chat service
	chatSvc := service.NewChatService(s, sessionSvc, jobQueue, eventBroker, sandboxSvc, gitSvc)

	// Create remaining services
	workspaceSvc := service.NewWorkspaceService(s, gitProvider, eventBroker)
	projectSvc := service.NewProjectService(s, sandboxProvider)
	preferenceSvc := service.NewPreferenceService(s)

	modelsSvc := service.NewModelsService(s, credSvc, sandboxSvc)

	h := &Handler{
		store:             s,
		cfg:               cfg,
		authService:       service.NewAuthService(s, cfg),
		credentialService: credSvc,
		gitService:        gitSvc,
		gitProvider:       gitProvider,
		sandboxProvider:   sandboxProvider,
		sandboxManager:    sandboxManager,
		sandboxService:    sandboxSvc,
		sessionService:    sessionSvc,
		chatService:       chatSvc,
		modelsService:     modelsSvc,
		workspaceService:  workspaceSvc,
		projectService:    projectSvc,
		preferenceService: preferenceSvc,
		jobQueue:          jobQueue,
		eventBroker:       eventBroker,
		systemManager:     systemManager,
		terminalManager:   terminal.NewManager(),
	}

	// Create Codex callback server (will be started on first use)
	h.codexCallbackServer = NewCodexCallbackServer(h)

	return h
}

// JSON helper to write JSON responses
func (h *Handler) JSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		_ = json.NewEncoder(w).Encode(data)
	}
}

// Error helper to write error responses
func (h *Handler) Error(w http.ResponseWriter, status int, message string) {
	h.JSON(w, status, map[string]string{"error": message})
}

// DecodeJSON helper to decode request body
func (h *Handler) DecodeJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

// JobQueue returns the handler's job queue.
// Used by main.go to wire up dispatcher notifications.
func (h *Handler) JobQueue() *jobs.Queue {
	return h.jobQueue
}

// EventBroker returns the handler's event broker for SSE.
func (h *Handler) EventBroker() *events.Broker {
	return h.eventBroker
}

// Close cleans up handler resources
func (h *Handler) Close() {
	if h.codexCallbackServer != nil {
		h.codexCallbackServer.Stop()
	}
}

// setSessionCookie sets the session cookie
func (h *Handler) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // Set to true in production with HTTPS
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30 * 24 * 60 * 60, // 30 days
	})
}

// clearSessionCookie clears the session cookie
func (h *Handler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

// getSessionToken gets the session token from cookie
func (h *Handler) getSessionToken(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// setStateCookie sets the OAuth state cookie
func (h *Handler) setStateCookie(w http.ResponseWriter, state string) {
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   10 * 60, // 10 minutes
	})
}

// getStateCookie gets and clears the OAuth state cookie
func (h *Handler) getStateCookie(w http.ResponseWriter, r *http.Request) string {
	cookie, err := r.Cookie(stateCookieName)
	if err != nil {
		return ""
	}
	// Clear the cookie
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	return cookie.Value
}
