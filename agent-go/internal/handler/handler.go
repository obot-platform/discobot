package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/obot-platform/discobot/agent-go/agent"
	"github.com/obot-platform/discobot/agent-go/agentimpl"
	"github.com/obot-platform/discobot/agent-go/internal/hooks"
	"github.com/obot-platform/discobot/agent-go/internal/routes"
	"github.com/obot-platform/discobot/agent-go/internal/services"
	"github.com/obot-platform/discobot/agent-go/message"
)

const maxHookRetries = 3

// Handler contains all HTTP handlers for the agent API.
type Handler struct {
	agentCwd       string
	completions    *agent.CompletionManager
	hookManager    *hooks.Manager          // nil if hooks are disabled
	serviceManager *services.Manager       // always initialized
	defaultAgent   *agentimpl.DefaultAgent // for MCP manager access; may be nil
	chatPingEvery  time.Duration

	hookMu         sync.Mutex
	hookRetryCount int

	answeredMu        sync.Mutex
	answeredQuestions map[string]bool // toolCallID → true (tracks answered questions for status polling)
}

// New creates a new Handler.
func New(agentCwd string, completions *agent.CompletionManager, hookManager *hooks.Manager, serviceManager *services.Manager, defaultAgent *agentimpl.DefaultAgent) *Handler {
	h := &Handler{
		agentCwd:          agentCwd,
		completions:       completions,
		hookManager:       hookManager,
		serviceManager:    serviceManager,
		defaultAgent:      defaultAgent,
		chatPingEvery:     defaultChatStreamPingInterval,
		answeredQuestions: make(map[string]bool),
	}

	// Wire up post-completion hook evaluation.
	if hookManager != nil {
		completions.AddCompletionListener(h)
	}

	return h
}

// OnTurnStart resets hook retry state when a turn begins.
func (h *Handler) OnTurnStart(_ string) {
	h.resetHookState()
}

// OnTurnComplete is called when a completion finishes. It schedules hook evaluation.
func (h *Handler) OnTurnComplete(threadID string, _ error) {
	if h.hookManager == nil || !h.hookManager.HasFileHooks() {
		return
	}
	// Fire-and-forget goroutine matching the TS scheduleHookEvaluation pattern.
	go h.scheduleHookEvaluation(threadID)
}

// startHookFailureReprompt sends a hook-failure follow-up message to the LLM.
func (h *Handler) startHookFailureReprompt(threadID string, result hooks.FileHookEvalResult) error {
	req := agent.PromptRequest{
		Metadata: func() json.RawMessage {
			if result.HookFailure == nil {
				return nil
			}
			data, err := json.Marshal(map[string]any{
				"discobot": result.HookFailure,
			})
			if err != nil {
				return nil
			}
			return data
		}(),
		UserParts: []message.UIPart{
			message.UITextPart{Text: result.LLMMessage},
		},
	}

	_, err := h.completions.Chat(threadID, req)
	return err
}

// scheduleHookEvaluation runs hook evaluation after a grace period, and
// triggers a re-prompt if a hook fails with notifyLlm=true.
func (h *Handler) scheduleHookEvaluation(threadID string) {
	// 200ms grace period to let SSE flush
	time.Sleep(200 * time.Millisecond)

	result := h.hookManager.EvaluateFileHooks()
	if !result.ShouldReprompt {
		return
	}

	h.hookMu.Lock()
	h.hookRetryCount++
	count := h.hookRetryCount
	h.hookMu.Unlock()

	if count >= maxHookRetries {
		log.Printf("hooks: max retries (%d) reached, not re-prompting", maxHookRetries)
		return
	}

	if err := h.startHookFailureReprompt(threadID, result); err != nil {
		log.Printf("hooks: failed to start re-prompt: %v", err)
	}
}

// resetHookState aborts any pending hook evaluation and resets the retry counter.
func (h *Handler) resetHookState() {
	h.hookMu.Lock()
	h.hookRetryCount = 0
	h.hookMu.Unlock()
}

// RegisterRoutes registers all API routes on the given router and records
// metadata in the global routes registry (used by GET /api/routes).
func (h *Handler) RegisterRoutes(r chi.Router) {
	reg := routes.GetRegistry()

	// Global routes (no auth required)
	reg.Register(r, routes.Route{Method: "GET", Pattern: "/", Handler: h.Root,
		Meta: routes.Meta{Group: "Health", Description: "API root / health check"}})
	reg.Register(r, routes.Route{Method: "GET", Pattern: "/health", Handler: h.Health,
		Meta: routes.Meta{Group: "Health", Description: "Health check"}})
	reg.Register(r, routes.Route{Method: "GET", Pattern: "/user", Handler: h.User,
		Meta: routes.Meta{Group: "Health", Description: "Current user info"}})

	// Thread routes
	reg.Register(r, routes.Route{Method: "GET", Pattern: "/threads", Handler: h.ListThreads,
		Meta: routes.Meta{Group: "Threads", Description: "List all threads"}})
	reg.Register(r, routes.Route{Method: "POST", Pattern: "/threads", Handler: h.CreateThread,
		Meta: routes.Meta{
			Group:       "Threads",
			Description: "Create a thread",
			Body:        map[string]any{"id": "thread-1", "name": "Debug build failure"},
		}})
	r.Route("/threads/{id}", func(r chi.Router) {
		threadReg := reg.WithPrefix("/threads/{id}")

		threadReg.Register(r, routes.Route{Method: "GET", Pattern: "/", Handler: h.GetThread,
			Meta: routes.Meta{Group: "Threads", Description: "Get thread metadata"}})
		threadReg.Register(r, routes.Route{Method: "PUT", Pattern: "/", Handler: h.UpdateThread,
			Meta: routes.Meta{
				Group:       "Threads",
				Description: "Replace thread metadata",
				Body:        map[string]any{"name": "Investigate failing CI"},
			}})
		threadReg.Register(r, routes.Route{Method: "PATCH", Pattern: "/", Handler: h.UpdateThread,
			Meta: routes.Meta{
				Group:       "Threads",
				Description: "Update thread metadata",
				Body:        map[string]any{"name": "Investigate failing CI"},
			}})
		threadReg.Register(r, routes.Route{Method: "DELETE", Pattern: "/", Handler: h.DeleteThread,
			Meta: routes.Meta{Group: "Threads", Description: "Delete a thread"}})

		threadReg.Register(r, routes.Route{Method: "GET", Pattern: "/models", Handler: h.ListModels,
			Meta: routes.Meta{Group: "Threads", Description: "List available models"}})

		threadReg.Register(r, routes.Route{Method: "POST", Pattern: "/chat", Handler: h.PostChat,
			Meta: routes.Meta{
				Group:       "Chat",
				Description: "Start a completion turn",
				Body: map[string]any{
					"messages": []map[string]any{{
						"id":   "msg-1",
						"role": "user",
						"parts": []map[string]any{{
							"type":  "text",
							"text":  "Help me understand this repository.",
							"state": "done",
						}},
					}},
				},
			}})
		threadReg.Register(r, routes.Route{Method: "GET", Pattern: "/chat/status", Handler: h.ChatStatus,
			Meta: routes.Meta{Group: "Chat", Description: "Check whether a completion is active"}})
		threadReg.Register(r, routes.Route{Method: "GET", Pattern: "/chat/stream", Handler: h.ChatStream,
			Meta: routes.Meta{Group: "Chat", Description: "Stream completion events (SSE)"}})
		threadReg.Register(r, routes.Route{Method: "POST", Pattern: "/chat/cancel", Handler: h.CancelChat,
			Meta: routes.Meta{Group: "Chat", Description: "Cancel the active completion"}})
		threadReg.Register(r, routes.Route{Method: "GET", Pattern: "/chat/question/{questionId}", Handler: h.GetQuestion,
			Meta: routes.Meta{Group: "Chat", Description: "Get pending AskUserQuestion"}})
		threadReg.Register(r, routes.Route{Method: "POST", Pattern: "/chat/answer/{questionId}", Handler: h.PostAnswer,
			Meta: routes.Meta{
				Group:       "Chat",
				Description: "Submit answer to pending question",
				Body: map[string]any{
					"answers": map[string]string{
						"Which model should we use?": "Claude Sonnet",
					},
				},
			}})
	})

	// File system routes
	reg.Register(r, routes.Route{Method: "GET", Pattern: "/files", Handler: h.ListFiles,
		Meta: routes.Meta{Group: "Files", Description: "List directory contents",
			Params: []routes.Param{{Name: "path", In: "query"}, {Name: "hidden", In: "query"}}}})
	reg.Register(r, routes.Route{Method: "GET", Pattern: "/files/search", Handler: h.SearchFiles,
		Meta: routes.Meta{Group: "Files", Description: "Fuzzy-search files",
			Params: []routes.Param{{Name: "q", In: "query", Required: true}, {Name: "limit", In: "query"}}}})
	reg.Register(r, routes.Route{Method: "GET", Pattern: "/files/read", Handler: h.ReadFile,
		Meta: routes.Meta{Group: "Files", Description: "Read file contents",
			Params: []routes.Param{{Name: "path", In: "query", Required: true}}}})
	reg.Register(r, routes.Route{Method: "POST", Pattern: "/files/write", Handler: h.WriteFile,
		Meta: routes.Meta{
			Group:       "Files",
			Description: "Write file contents",
			Body:        map[string]any{"path": "notes/todo.txt", "content": "Ship it\n", "encoding": "utf8"},
		}})
	reg.Register(r, routes.Route{Method: "POST", Pattern: "/files/delete", Handler: h.DeleteFile,
		Meta: routes.Meta{
			Group:       "Files",
			Description: "Delete a file or directory",
			Body:        map[string]any{"path": "notes/todo.txt"},
		}})
	reg.Register(r, routes.Route{Method: "POST", Pattern: "/files/rename", Handler: h.RenameFile,
		Meta: routes.Meta{
			Group:       "Files",
			Description: "Rename or move a file",
			Body:        map[string]any{"oldPath": "notes/todo.txt", "newPath": "notes/archive/todo.txt"},
		}})

	// Git routes
	reg.Register(r, routes.Route{Method: "GET", Pattern: "/diff", Handler: h.GetDiff,
		Meta: routes.Meta{Group: "Git", Description: "Get workspace diff",
			Params: []routes.Param{{Name: "path", In: "query"}, {Name: "format", In: "query"}}}})
	reg.Register(r, routes.Route{Method: "GET", Pattern: "/commits", Handler: h.GetCommits,
		Meta: routes.Meta{Group: "Git", Description: "Get recent commit replay bundle",
			Params: []routes.Param{{Name: "parent", In: "query"}}}})

	// Hook routes
	reg.Register(r, routes.Route{Method: "GET", Pattern: "/hooks/status", Handler: h.HooksStatus,
		Meta: routes.Meta{Group: "Hooks", Description: "Get hook evaluation status"}})
	reg.Register(r, routes.Route{Method: "GET", Pattern: "/hooks/{hookId}/output", Handler: h.HookOutput,
		Meta: routes.Meta{Group: "Hooks", Description: "Get hook output log"}})
	reg.Register(r, routes.Route{Method: "POST", Pattern: "/hooks/{hookId}/rerun", Handler: h.RerunHook,
		Meta: routes.Meta{Group: "Hooks", Description: "Manually rerun a hook"}})

	// Service routes
	reg.Register(r, routes.Route{Method: "GET", Pattern: "/services", Handler: h.ListServices,
		Meta: routes.Meta{Group: "Services", Description: "List all services with status"}})
	reg.Register(r, routes.Route{Method: "POST", Pattern: "/services/{serviceId}/start", Handler: h.StartService,
		Meta: routes.Meta{Group: "Services", Description: "Start a service"}})
	reg.Register(r, routes.Route{Method: "POST", Pattern: "/services/{serviceId}/stop", Handler: h.StopService,
		Meta: routes.Meta{Group: "Services", Description: "Stop a running service"}})
	reg.Register(r, routes.Route{Method: "GET", Pattern: "/services/{serviceId}/output", Handler: h.ServiceOutput,
		Meta: routes.Meta{Group: "Services", Description: "Stream service output (SSE)"}})
	// ServiceProxy is registered directly (HandleFunc, not method-specific)
	r.HandleFunc("/services/{serviceId}/http", h.ServiceProxy)
	r.HandleFunc("/services/{serviceId}/http/*", h.ServiceProxy)

	// MCP server routes
	reg.Register(r, routes.Route{Method: "GET", Pattern: "/mcp/servers", Handler: h.ListMCPServers,
		Meta: routes.Meta{Group: "MCP", Description: "List MCP server connection status"}})
	reg.Register(r, routes.Route{Method: "GET", Pattern: "/mcp/servers/{name}/oauth", Handler: h.GetMCPServerOAuth,
		Meta: routes.Meta{Group: "MCP", Description: "Get OAuth authorization URL for MCP server"}})
	reg.Register(r, routes.Route{Method: "POST", Pattern: "/mcp/servers/{name}/oauth/code", Handler: h.PostMCPServerOAuthCode,
		Meta: routes.Meta{
			Group:       "MCP",
			Description: "Submit OAuth authorization code for MCP server",
			Body:        map[string]any{"code": "abc123", "state": "xyz789"},
		}})
}

// JSON writes a JSON response with the given status code.
func (h *Handler) JSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		_ = json.NewEncoder(w).Encode(data)
	}
}

// Error writes a JSON error response.
func (h *Handler) Error(w http.ResponseWriter, status int, message string) {
	h.JSON(w, status, map[string]string{"error": message})
}

// DecodeJSON decodes the request body as JSON.
func (h *Handler) DecodeJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}
