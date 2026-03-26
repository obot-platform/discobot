# Server Architecture

This document describes the architecture of the Discobot Go server, which provides REST APIs and manages workspace/session/sandbox lifecycle.

## Overview

The server follows a layered architecture:

```
┌─────────────────────────────────────────────────────────────────┐
│                        HTTP Layer                                │
│  Middleware → Router → Handlers                                 │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Service Layer                               │
│  Business logic, validation, orchestration                      │
└─────────────────────────────────────────────────────────────────┘
                              │
        ┌─────────────────────┼─────────────────────┐
        ▼                     ▼                     ▼
┌─────────────┐       ┌─────────────┐       ┌─────────────┐
│    Store    │       │   Sandbox   │       │     Git     │
│   (GORM)    │       │   Provider  │       │   Provider  │
└─────────────┘       └─────────────┘       └─────────────┘
        │                     │                     │
        ▼                     ▼                     ▼
   Database              Docker API            File System
```

## Module Documentation

- [Handler Module](./design/handler.md) - HTTP request handlers
- [Service Module](./design/service.md) - Business logic layer
- [Store Module](./design/store.md) - Data access layer
- [Sandbox Module](./design/sandbox.md) - Docker integration
- [Cache System](./design/cache.md) - Project-scoped cache volumes
- [Events Module](./design/events.md) - SSE and event system
- [Jobs Module](./design/jobs.md) - Background job processing

## Directory Structure

```
server/
├── cmd/server/main.go          # Entry point
├── internal/
│   ├── config/config.go        # Environment configuration
│   ├── database/database.go    # DB connection
│   ├── model/model.go          # GORM models
│   ├── store/store.go          # Data access layer
│   ├── handler/                # HTTP handlers
│   │   ├── handler.go          # Base handler
│   │   ├── auth.go
│   │   ├── projects.go
│   │   ├── workspaces.go
│   │   ├── sessions.go
│   │   ├── agents.go
│   │   ├── chat.go
│   │   ├── credentials.go
│   │   ├── preferences.go      # User preferences API
│   │   ├── files.go
│   │   ├── terminal.go
│   │   ├── git.go
│   │   ├── events.go
│   │   └── status.go
│   ├── service/                # Business logic
│   │   ├── auth.go
│   │   ├── project.go
│   │   ├── workspace.go
│   │   ├── session.go
│   │   ├── agent.go
│   │   ├── chat.go
│   │   ├── sandbox.go
│   │   ├── sandbox_client.go
│   │   ├── credential.go
│   │   ├── preference.go       # User preferences (key/value store)
│   │   └── git.go
│   ├── sandbox/                # Sandbox abstraction
│   │   ├── runtime.go          # Interface
│   │   ├── docker/provider.go  # Docker impl
│   │   └── mock/provider.go    # Mock impl
│   ├── git/                    # Git provider
│   │   ├── git.go              # Interface
│   │   └── local.go            # Local impl
│   ├── dispatcher/             # Job dispatcher
│   ├── jobs/                   # Background jobs
│   ├── events/                 # Event system
│   ├── middleware/             # HTTP middleware
│   ├── encryption/             # AES-256-GCM
│   └── integration/            # Integration tests
```

## Initialization Flow

The `main()` function initializes all components:

```go
func main() {
    // 1. Load configuration
    cfg := config.Load()

    // 2. Connect database
    db := database.Connect(cfg.DatabaseDSN)
    database.Migrate(db)
    database.Seed(db)

    // 3. Create providers
    gitProvider := git.NewLocalProvider(cfg)
    sandboxProvider := sandbox.NewDockerProvider(cfg)

    // 4. Create store (separate read/write pools for SQLite)
    store := store.New(db.DB, db.ReadDB)

    // 5. Create services
    services := service.NewServices(store, gitProvider, sandboxProvider)

    // 6. Create event system
    eventBroker := events.NewBroker(store)
    eventPoller := events.NewPoller(store, eventBroker)
    go eventPoller.Start()

    // 7. Create job dispatcher
    jobQueue := jobs.NewQueue(store)
    dispatcher := dispatcher.New(cfg, store, jobQueue)
    dispatcher.RegisterExecutor("workspace_init", ...)
    dispatcher.RegisterExecutor("session_init", ...)
    go dispatcher.Start()

    // 8. Create router
    r := chi.NewRouter()
    r.Use(middleware.RequestID)
    r.Use(middleware.Logger)
    r.Use(middleware.CORS)

    // 9. Register handlers
    h := handler.New(cfg, store, services, eventBroker)
    h.RegisterRoutes(r)

    // 10. Start server
    http.ListenAndServe(":"+cfg.Port, r)
}
```

## Request Flow

### Standard API Request

```
Client Request
      │
      ▼
┌─────────────────┐
│   Middleware    │ → Request ID, Logging, Auth
└─────────────────┘
      │
      ▼
┌─────────────────┐
│    Handler      │ → Parse request, validate
└─────────────────┘
      │
      ▼
┌─────────────────┐
│    Service      │ → Business logic
└─────────────────┘
      │
      ▼
┌─────────────────┐
│     Store       │ → Database query
└─────────────────┘
      │
      ▼
     JSON Response
```

### Chat Request (SSE)

```
Client POST /sessions/{sessionId}/threads/{threadId}/chat
      │
      ▼
┌─────────────────┐
│  Chat Handler   │ → Validate session
└─────────────────┘
      │
      ▼
┌─────────────────┐
│  Chat Service   │ → Create/get session
└─────────────────┘
      │
      ▼
┌─────────────────────┐
│  Sandbox Client     │ → POST to sandbox:3002/chat
└─────────────────────┘
      │
      ▼
   SSE Stream ──────────▶ Client
```

## Data Model

### Entity Relationships

```
User
 │
 └──▶ Project
       │
       ├──▶ Workspace ──▶ Session ──▶ Messages
       │
       └──▶ Agent ──▶ MCPServer
```

### Key Models

```go
type Workspace struct {
    ID          string
    ProjectID   string
    Name        string
    Path        string     // Local path or git URL (actual location)
    DisplayName *string    // Optional: custom display name for UI (nil = use path)
    Status      string     // initializing, ready, error
    Sessions    []Session
}

type Session struct {
    ID          string
    WorkspaceID string
    AgentID     string
    Name        string
    Status      string  // initializing, ready, stopped, error, removing, removed
    SandboxID   string
}

type Agent struct {
    ID        string
    ProjectID string
    Name      string
    Type      string  // claude-code, gemini-cli, etc.
    Mode      string
    Model     string
    IsDefault bool
}

type UserPreference struct {
    ID        string
    UserID    string    // Scoped to user, not project
    Key       string    // e.g., "theme", "preferredIDE"
    Value     string    // Stored as text (can be JSON)
    CreatedAt time.Time
    UpdatedAt time.Time
}
```

## Authentication

### Anonymous Mode (Default)

When `AUTH_ENABLED=false`:
- Uses hardcoded anonymous user
- No session validation
- Suitable for local development

### Authenticated Mode

When `AUTH_ENABLED=true`:
- OAuth2 with PKCE
- Session cookies
- Project membership validation

## Event System

### Event Publishing

```go
// Service emits event
eventBroker.Publish(events.Event{
    Type:      "session_updated",
    ProjectID: projectID,
    Payload: map[string]string{
        "sessionId": sessionID,
        "status":    "ready",
    },
})
```

### Event Subscription (SSE)

```go
// Handler subscribes client
subscriber := eventBroker.Subscribe(projectID)
defer eventBroker.Unsubscribe(subscriber)

for event := range subscriber.Events {
    fmt.Fprintf(w, "data: %s\n\n", event.JSON())
    flusher.Flush()
}
```

## Job System

### Job Types

- `workspace_init` - Clone git repo, setup workspace
- `session_init` - Create sandbox, start agent

### Job Flow

```
1. Handler enqueues job
   │
2. Job saved to database
   │
3. Dispatcher polls for jobs
   │
4. Executor runs job
   │
5. Job status updated
   │
6. Event published (optional)
```

## Sandbox Integration

### Lifecycle

```
Create Workspace → Enqueue workspace_init job
                        │
                        ▼
                   Clone/setup workspace
                        │
                        ▼
Start Chat → Enqueue session_init job
                        │
                        ▼
               Create Docker sandbox
               Mount workspace
               Start agent process
                        │
                        ▼
Chat Message → Update session status to "running"
            → POST sandbox:3002/chat
                        │
                        ▼
               Stream SSE response
                        │
                        ▼
            → Update session status to "ready"
```

### Sandbox Configuration

```go
type SandboxOptions struct {
    Image       string            // e.g., "discobot-agent-api:latest"
    Binds       []string          // Volume mounts
    Env         []string          // Environment variables
    NetworkMode string            // Docker network
    Labels      map[string]string // Sandbox labels
}
```

## Error Handling

### HTTP Errors

```go
// handlers return appropriate status codes
func (h *Handler) Error(w http.ResponseWriter, err error, status int) {
    h.JSON(w, map[string]string{"error": err.Error()}, status)
}
```

### Service Errors

```go
// services return typed errors
var ErrNotFound = errors.New("not found")
var ErrUnauthorized = errors.New("unauthorized")
```

## Configuration

Key environment variables:

| Variable | Description |
|----------|-------------|
| `PORT` | Server port (default: 3001) |
| `DATABASE_DSN` | Database connection string |
| `WORKSPACE_DIR` | Base directory for workspaces |
| `SANDBOX_IMAGE` | Default sandbox image |
| `AUTH_ENABLED` | Enable authentication |
| `ENCRYPTION_KEY` | AES-256 key for credentials |

During process startup, the server probes its configured API port before doing the rest of initialization. It retries the bind check every 10 seconds for up to 2 minutes, closes the temporary listener as soon as the port becomes available, and then proceeds with normal startup.

## Testing

The server includes:
- Unit tests for each package
- Integration tests with real database
- Mock sandbox provider for testing
