# Handler Module

This module implements HTTP request handlers for all REST API endpoints.

## Files

| File | Description |
|------|-------------|
| `internal/handler/handler.go` | Base handler struct and helpers |
| `internal/handler/auth.go` | Authentication endpoints |
| `internal/handler/projects.go` | Project CRUD |
| `internal/handler/workspaces.go` | Workspace CRUD and git ops |
| `internal/handler/sessions.go` | Session CRUD |
| `internal/handler/agents.go` | Agent CRUD and types |
| `internal/handler/chat.go` | Chat streaming endpoint |
| `internal/handler/credentials.go` | Credential management |
| `internal/handler/files.go` | File operations |
| `internal/handler/terminal.go` | Terminal WebSocket |
| `internal/handler/git.go` | Git operations |
| `internal/handler/events.go` | SSE event streaming |
| `internal/handler/status.go` | Health check |

## Base Handler

### Structure

```go
type Handler struct {
    config           *config.Config
    store            *store.Store
    services         *service.Services
    eventBroker      *events.Broker
    sandboxProvider  sandbox.Provider
    gitProvider      git.Provider
}

func New(
    cfg *config.Config,
    store *store.Store,
    services *service.Services,
    broker *events.Broker,
) *Handler
```

### Helper Methods

```go
// JSON response
func (h *Handler) JSON(w http.ResponseWriter, data any, status int)

// Error response
func (h *Handler) Error(w http.ResponseWriter, err error, status int)

// Decode JSON body
func (h *Handler) DecodeJSON(r *http.Request, v any) error

// Get path parameter
func (h *Handler) PathParam(r *http.Request, key string) string
```

## Route Registration

```go
func (h *Handler) RegisterRoutes(r chi.Router) {
    // Public routes
    r.Get("/api/status", h.GetStatus)
    r.Get("/auth/login/{provider}", h.Login)
    r.Get("/auth/callback/{provider}", h.Callback)

    // Protected routes
    r.Group(func(r chi.Router) {
        r.Use(h.authMiddleware)

        // Projects
        r.Get("/api/projects", h.ListProjects)
        r.Post("/api/projects", h.CreateProject)

        // Project-scoped routes
        r.Route("/api/projects/{projectId}", func(r chi.Router) {
            r.Use(h.projectMiddleware)

            // Workspaces
            r.Get("/workspaces", h.ListWorkspaces)
            r.Post("/workspaces", h.CreateWorkspace)
            r.Get("/workspaces/{workspaceId}", h.GetWorkspace)
            r.Delete("/workspaces/{workspaceId}", h.DeleteWorkspace)

            // Sessions
            r.Get("/sessions/{sessionId}", h.GetSession)
            r.Put("/sessions/{sessionId}", h.UpdateSession)
            r.Delete("/sessions/{sessionId}", h.DeleteSession)

            // Agents
            r.Get("/agents", h.ListAgents)
            r.Post("/agents", h.CreateAgent)
            r.Get("/agents/types", h.ListAgentTypes)

            // Chat
            r.Post("/chat", h.Chat)

            // Terminal
            r.Get("/sessions/{sessionId}/terminal/ws", h.TerminalWebSocket)
            r.Get("/sessions/{sessionId}/terminal/status", h.GetTerminalStatus)

            // Events
            r.Get("/events", h.Events)
        })
    })
}
```

## Handler Implementations

### Authentication Handlers (auth.go)

```go
// GET /auth/login/{provider}
func (h *Handler) Login(w http.ResponseWriter, r *http.Request)
// Initiates OAuth flow, redirects to provider

// GET /auth/callback/{provider}
func (h *Handler) Callback(w http.ResponseWriter, r *http.Request)
// Handles OAuth callback, creates session

// POST /auth/logout
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request)
// Clears session cookie

// GET /auth/me
func (h *Handler) Me(w http.ResponseWriter, r *http.Request)
// Returns current user info
```

### Workspace Handlers (workspaces.go)

```go
// GET /api/projects/{projectId}/workspaces
func (h *Handler) ListWorkspaces(w http.ResponseWriter, r *http.Request) {
    projectID := h.PathParam(r, "projectId")
    workspaces, err := h.store.ListWorkspaces(r.Context(), projectID)
    if err != nil {
        h.Error(w, err, http.StatusInternalServerError)
        return
    }
    h.JSON(w, workspaces, http.StatusOK)
}

// POST /api/projects/{projectId}/workspaces
func (h *Handler) CreateWorkspace(w http.ResponseWriter, r *http.Request) {
    var req CreateWorkspaceRequest
    if err := h.DecodeJSON(r, &req); err != nil {
        h.Error(w, err, http.StatusBadRequest)
        return
    }

    workspace, err := h.services.Workspace.Create(r.Context(), req)
    if err != nil {
        h.Error(w, err, http.StatusInternalServerError)
        return
    }

    // Enqueue initialization job
    h.services.Jobs.EnqueueWorkspaceInit(workspace.ID)

    h.JSON(w, workspace, http.StatusCreated)
}
```

### Chat Handler (chat.go)

The chat handler provides thread-scoped endpoints for AI SDK integration:

**POST /api/projects/{projectId}/sessions/{sessionId}/threads/{threadId}/chat** - Create/validate a session and start a chat request
**GET /api/projects/{projectId}/sessions/{sessionId}/threads/{threadId}/stream** - Stream or resume chat events

Fresh stream requests (no `Last-Event-ID`, or an invalid one) now emit:
1. `event: history-start`
2. one `event: message` per persisted UI message in session history
3. the initial in-memory chunk snapshot for the current completion, if one exists (even if that completion has already finished)
4. `event: history-end`
5. then any later live chunk events or idle pings

Valid `Last-Event-ID` reconnects skip the history replay and continue from the requested chunk offset.

For new sessions, `workspaceId` is optional: when omitted, the handler creates a local workspace under Discobot's configured data workspace directory, marks it `autoGenerated: true`, and uses it for the session. Session and thread identity now come from the URL path rather than the JSON body. `POST /sessions/{sessionId}/threads/{threadId}/chat` returns normal JSON metadata (`workspaceId`, `sessionId`, `threadId`, and submitted `messageId`) after initiation; clients stream output separately from `GET /sessions/{sessionId}/threads/{threadId}/stream`. If `messages` is omitted, `null`, or `[]`, the handler still creates/validates the session and returns JSON without contacting the sandbox.

```go
// POST /api/projects/{projectId}/sessions/{sessionId}/threads/{threadId}/chat
func (h *Handler) Chat(w http.ResponseWriter, r *http.Request) {
    var req ChatRequest
    if err := h.DecodeJSON(r, &req); err != nil {
        h.Error(w, http.StatusBadRequest, "Invalid request body")
        return
    }

    emptySubmission := isEmptyChatMessages(req.Messages)
    sessionID := r.PathValue("sessionId")
    if sessionID == "" {
        h.Error(w, http.StatusBadRequest, "sessionId is required")
        return
    }
    threadID := r.PathValue("threadId")
    if threadID == "" {
        h.Error(w, http.StatusBadRequest, "threadId is required")
        return
    }

    // Check if session exists, create if needed
    existingSession, err := h.chatService.GetSessionByID(ctx, sessionID)
    if err != nil {
        // Session doesn't exist - create it
        workspaceID, err := h.resolveWorkspaceIDForNewSession(ctx, projectID, req.WorkspaceID)
        if err != nil {
            h.Error(w, http.StatusInternalServerError, err.Error())
            return
        }

        _, err := h.chatService.NewSession(ctx, service.NewSessionRequest{
            SessionID:   sessionID,
            ProjectID:   projectID,
            WorkspaceID: workspaceID,
            Model:       req.Model,
            Reasoning:   req.Reasoning,
            Mode:        req.Mode,
            Messages:    req.Messages,
        })
        if err != nil {
            h.Error(w, http.StatusBadRequest, err.Error())
            return
        }
    }

    response := ChatResponse{
        WorkspaceID: workspaceID,
        SessionID:   sessionID,
        ThreadID:    threadID,
        MessageID:   extractSubmittedMessageID(req.Messages, req.MessageID),
    }

    if emptySubmission {
        h.JSON(w, http.StatusOK, response)
        return
    }

    sendCtx := context.WithoutCancel(ctx)
    go func() {
        _, err := h.chatService.StartChat(sendCtx, projectID, sessionID, threadID, req.Messages, req.Model, req.Reasoning, req.Mode)
        if err != nil {
            log.Printf("failed to start chat for %s: %v", sessionID, err)
        }
    }()
    h.JSON(w, http.StatusOK, response)
}

// GET /api/projects/{projectId}/sessions/{sessionId}/threads/{threadId}/stream
// Resumes an in-progress chat stream (for AI SDK resume functionality)
func (h *Handler) ChatStream(w http.ResponseWriter, r *http.Request) {
    sessionID := r.PathValue("sessionId")
    threadID := r.PathValue("threadId")

    // Validate session exists and belongs to project
    existingSession, err := h.chatService.GetSessionByID(ctx, sessionID)
    if err != nil {
        w.WriteHeader(http.StatusNoContent) // No session = no stream
        return
    }
    if existingSession.ProjectID != projectID {
        h.Error(w, "session does not belong to this project", http.StatusForbidden)
        return
    }

    // Get the stream from sandbox
    sseCh, err := h.chatService.GetStream(ctx, projectID, sessionID, threadID)
    if err != nil {
        w.WriteHeader(http.StatusNoContent) // No active stream
        return
    }

    // Check if channel has data (with non-blocking select)
    // IMPORTANT: Store any consumed message to send it after headers
    var firstLine *service.SSELine
    select {
    case line, ok := <-sseCh:
        if !ok {
            w.WriteHeader(http.StatusNoContent) // Channel closed = no stream
            return
        }
        firstLine = &line // Store the consumed message
    default:
        // Channel not ready yet - we have a stream
    }

    // Set SSE headers
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    w.Header().Set("x-vercel-ai-ui-message-stream", "v1")

    flusher := w.(http.Flusher)

    // Send the first message if we consumed one during the check
    if firstLine != nil {
        if firstLine.Done {
            fmt.Fprintf(w, "data: [DONE]\n\n")
            flusher.Flush()
            return
        }
        fmt.Fprintf(w, "data: %s\n\n", firstLine.Data)
        flusher.Flush()
    }

    // Stream remaining messages
    for line := range sseCh {
        if line.Done {
            fmt.Fprintf(w, "data: [DONE]\n\n")
            flusher.Flush()
            return
        }
        fmt.Fprintf(w, "data: %s\n\n", line.Data)
        flusher.Flush()
    }
}
```

**Stream Resume Fix:**

The `ChatStream` handler includes a critical fix for stream resumption. When checking if a channel has data using a non-blocking `select`, any consumed message is stored in `firstLine` and sent after setting headers. This prevents message loss during the channel check, which was causing state corruption in the AI SDK.

### Events Handler (events.go)

```go
// GET /api/projects/{projectId}/events
func (h *Handler) Events(w http.ResponseWriter, r *http.Request) {
    projectID := h.PathParam(r, "projectId")

    // Set SSE headers
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")

    // Subscribe to events
    subscriber := h.eventBroker.Subscribe(projectID)
    defer h.eventBroker.Unsubscribe(subscriber)

    flusher := w.(http.Flusher)

    // Stream events
    for {
        select {
        case event := <-subscriber.Events:
            data, _ := json.Marshal(event)
            fmt.Fprintf(w, "data: %s\n\n", data)
            flusher.Flush()

        case <-r.Context().Done():
            return
        }
    }
}
```

### Terminal Handler (terminal.go)

```go
// GET /sessions/{sessionId}/terminal/ws
func (h *Handler) TerminalWebSocket(w http.ResponseWriter, r *http.Request)
// Upgrades to WebSocket, attaches to sandbox PTY
// Query params: rows, cols, root (true/false)
```

**WebSocket Message Protocol:**

```go
type TerminalMessage struct {
    Type string          `json:"type"` // "input", "output", "resize", "error"
    Data json.RawMessage `json:"data,omitempty"`
}

type ResizeData struct {
    Rows int `json:"rows"`
    Cols int `json:"cols"`
}
```

**Connection Flow:**

1. Upgrade HTTP to WebSocket
2. Parse query params (rows, cols, root)
3. Ensure sandbox is running via `sandboxService.EnsureRunning()`
4. Get default user via `sandboxService.GetUserInfo()` (calls agent-api `/user`)
5. Attach to sandbox PTY with user as `UID:GID` format (or "root" if `?root=true`)
6. Bidirectional relay: WebSocket ↔ PTY

**User Resolution:**

```go
var user string
if runAsRoot {
    user = "root"
} else {
    // Get user info from sandbox's agent-api
    _, uid, gid, err := h.sandboxService.GetUserInfo(ctx, sessionID)
    if err != nil {
        user = "root"  // Fallback
    } else {
        user = fmt.Sprintf("%d:%d", uid, gid)  // UID:GID format
    }
}
```

## Request/Response Types

### Workspace Types

```go
type CreateWorkspaceRequest struct {
    Name    string `json:"name"`
    Path    string `json:"path,omitempty"`
    GitRepo string `json:"gitRepo,omitempty"`
}

type WorkspaceResponse struct {
    ID        string            `json:"id"`
    Name      string            `json:"name"`
    Path      string            `json:"path,omitempty"`
    GitRepo   string            `json:"gitRepo,omitempty"`
    Status    string            `json:"status"`
    Sessions  []SessionResponse `json:"sessions"`
    CreatedAt time.Time         `json:"createdAt"`
}
```

### Session Types

```go
type SessionResponse struct {
    ID          string    `json:"id"`
    Name        string    `json:"name"`
    WorkspaceID string    `json:"workspaceId"`
    AgentID     string    `json:"agentId"`
    Status      string    `json:"status"`
    CreatedAt   time.Time `json:"createdAt"`
}

type UpdateSessionRequest struct {
    Name   string `json:"name,omitempty"`
    Status string `json:"status,omitempty"`
}
```

### Chat Types

```go
type ChatRequest struct {
    ID          string      `json:"id,omitempty"`
    WorkspaceID string      `json:"workspaceId"`
    AgentID     string      `json:"agentId"`
    Messages    []UIMessage `json:"messages"`
}
```

## Error Handling

```go
func (h *Handler) handleError(w http.ResponseWriter, err error) {
    switch {
    case errors.Is(err, store.ErrNotFound):
        h.Error(w, err, http.StatusNotFound)
    case errors.Is(err, service.ErrUnauthorized):
        h.Error(w, err, http.StatusUnauthorized)
    case errors.Is(err, service.ErrForbidden):
        h.Error(w, err, http.StatusForbidden)
    case errors.Is(err, service.ErrValidation):
        h.Error(w, err, http.StatusBadRequest)
    default:
        h.Error(w, err, http.StatusInternalServerError)
    }
}
```

## Middleware

### Auth Middleware

```go
func (h *Handler) authMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if !h.config.AuthEnabled {
            // Use anonymous user
            ctx := context.WithValue(r.Context(), userKey, anonymousUser)
            next.ServeHTTP(w, r.WithContext(ctx))
            return
        }

        // Validate session cookie
        user, err := h.services.Auth.ValidateSession(r)
        if err != nil {
            h.Error(w, err, http.StatusUnauthorized)
            return
        }

        ctx := context.WithValue(r.Context(), userKey, user)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

### Project Middleware

```go
func (h *Handler) projectMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        projectID := chi.URLParam(r, "projectId")
        user := r.Context().Value(userKey).(*model.User)

        // Check membership
        if !h.services.Project.IsMember(r.Context(), projectID, user.ID) {
            h.Error(w, ErrForbidden, http.StatusForbidden)
            return
        }

        next.ServeHTTP(w, r)
    })
}
```

## Testing

```go
func TestListWorkspaces(t *testing.T) {
    h := setupTestHandler(t)

    req := httptest.NewRequest("GET", "/api/projects/test/workspaces", nil)
    w := httptest.NewRecorder()

    h.ListWorkspaces(w, req)

    assert.Equal(t, http.StatusOK, w.Code)

    var workspaces []WorkspaceResponse
    json.Unmarshal(w.Body.Bytes(), &workspaces)
    assert.NotEmpty(t, workspaces)
}
```
