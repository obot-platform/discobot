# Discobot Server

The Discobot Server is a Go backend that provides REST APIs for workspace management, session orchestration, and sandbox lifecycle management.

## Overview

The server handles:
- Workspace creation and git operations
- Session lifecycle and sandbox management
- Agent configuration and credential storage
- Real-time events via Server-Sent Events
- Chat message routing to sandboxes

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                       Go Server                                  │
│                                                                  │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │                    HTTP Handlers                          │  │
│  │  /api/projects/{id}/workspaces, sessions, agents, etc.   │  │
│  └──────────────────────────────────────────────────────────┘  │
│                           │                                      │
│                           ▼                                      │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │                    Service Layer                          │  │
│  │  Business logic, validation, orchestration               │  │
│  └──────────────────────────────────────────────────────────┘  │
│                           │                                      │
│         ┌─────────────────┼─────────────────┐                   │
│         ▼                 ▼                 ▼                   │
│  ┌────────────┐   ┌────────────┐   ┌────────────────────┐      │
│  │   Store    │   │  Sandbox   │   │   Git Provider     │      │
│  │   (GORM)   │   │  Provider  │   │   (local git)      │      │
│  └────────────┘   └────────────┘   └────────────────────┘      │
│         │                 │                 │                   │
│         ▼                 ▼                 ▼                   │
│    PostgreSQL/       Docker API       File System               │
│     SQLite                                                      │
└─────────────────────────────────────────────────────────────────┘
```

## Documentation

- [Architecture Overview](./docs/ARCHITECTURE.md) - System design and data flow
- [Handler Module](./docs/design/handler.md) - HTTP request handlers
- [Service Module](./docs/design/service.md) - Business logic layer
- [Store Module](./docs/design/store.md) - Data access layer
- [Sandbox Module](./docs/design/sandbox.md) - Docker integration
- [Cache System](./docs/design/cache.md) - Project-scoped cache volumes
- [Events Module](./docs/design/events.md) - SSE and event system
- [Jobs Module](./docs/design/jobs.md) - Background job processing

## Getting Started

### Prerequisites

- Go 1.23+
- Docker (for sandbox runtime)
- PostgreSQL or SQLite

### Development

```bash
# Run with auto-reload
cd server
air

# Or run directly
go run cmd/server/main.go

# Run tests
go test ./...

# Run linter
golangci-lint run
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `3001` | HTTP server port |
| `DATABASE_DSN` | `discobot.db` | Database connection string |
| `AUTH_ENABLED` | `false` | Enable authentication |
| `WORKSPACE_DIR` | `/tmp/workspaces` | Base directory for workspaces |
| `SANDBOX_IMAGE` | `ghcr.io/obot-platform/discobot:main` | Default sandbox image |
| `CACHE_ENABLED` | `true` | Enable project-scoped cache volumes |
| `ENCRYPTION_KEY` | (required) | Key for credential encryption |

On startup, the server first waits until its API port can be bound. It probes the configured `PORT` every 10 seconds for up to 2 minutes, immediately releases the probe listener on success, and only then continues with the normal startup sequence. This avoids restart races with a previous process that is still shutting down.

### Building

```bash
go build -o discobot-server ./cmd/server
```

## API Endpoints

### Projects

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/projects` | List projects |
| POST | `/api/projects` | Create project |
| GET | `/api/projects/{id}` | Get project |
| PUT | `/api/projects/{id}` | Update project |
| DELETE | `/api/projects/{id}` | Delete project |

### Workspaces

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/projects/{id}/workspaces` | List workspaces |
| POST | `/api/projects/{id}/workspaces` | Create workspace |
| GET | `/api/projects/{id}/workspaces/{wid}` | Get workspace |
| DELETE | `/api/projects/{id}/workspaces/{wid}` | Delete workspace |

### Sessions

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/projects/{id}/sessions/{sid}` | Get session |
| PUT | `/api/projects/{id}/sessions/{sid}` | Update session |
| DELETE | `/api/projects/{id}/sessions/{sid}` | Delete session |

### Chat

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/projects/{id}/chat` | Start chat request and return JSON metadata |

### Agents

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/projects/{id}/agents` | List agents |
| POST | `/api/projects/{id}/agents` | Create agent |
| PUT | `/api/projects/{id}/agents/{aid}` | Update agent |
| DELETE | `/api/projects/{id}/agents/{aid}` | Delete agent |
| GET | `/api/projects/{id}/agents/types` | List agent types |

### User Preferences

User preferences are scoped to the authenticated user (not project-scoped).

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/preferences` | List all preferences |
| PUT | `/api/preferences` | Set multiple preferences |
| GET | `/api/preferences/{key}` | Get preference by key |
| PUT | `/api/preferences/{key}` | Set/update preference |
| DELETE | `/api/preferences/{key}` | Delete preference |

**Examples:**

```bash
# Set a preference
curl -X PUT http://localhost:3001/api/preferences/theme \
  -H "Content-Type: application/json" \
  -d '{"value": "dark"}'

# Get a preference
curl http://localhost:3001/api/preferences/theme

# Set multiple preferences
curl -X PUT http://localhost:3001/api/preferences \
  -H "Content-Type: application/json" \
  -d '{"preferences": {"theme": "dark", "editor": "vim"}}'

# List all preferences
curl http://localhost:3001/api/preferences

# Delete a preference
curl -X DELETE http://localhost:3001/api/preferences/theme
```

### Events

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/projects/{id}/events` | SSE event stream |

## Project Structure

```
server/
├── cmd/server/
│   └── main.go              # Application entry point
├── internal/
│   ├── config/              # Configuration loading
│   ├── database/            # Database connection
│   ├── model/               # GORM models (User, Project, Session, UserPreference, etc.)
│   ├── store/               # Data access layer
│   ├── handler/             # HTTP handlers
│   │   ├── handler.go       # Base handler setup
│   │   ├── preferences.go   # User preferences handlers
│   │   └── ...
│   ├── service/             # Business logic
│   │   ├── preference.go    # User preferences service
│   │   └── ...
│   ├── sandbox/             # Sandbox runtime
│   │   ├── docker/          # Docker implementation
│   │   └── mock/            # Mock for testing
│   ├── git/                 # Git operations
│   ├── dispatcher/          # Job dispatcher
│   ├── jobs/                # Background jobs
│   ├── events/              # Event system
│   ├── middleware/          # HTTP middleware
│   ├── encryption/          # Credential encryption
│   └── integration/         # Integration tests
├── go.mod
└── go.sum
```

## Dependencies

| Package | Purpose |
|---------|---------|
| `go-chi/chi` | HTTP routing |
| `gorm.io/gorm` | ORM |
| `docker/docker` | Docker SDK |
| `google/uuid` | UUID generation |
| `gorilla/websocket` | WebSocket support |

## Testing

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run specific package
go test ./internal/service/...

# Run integration tests
go test ./internal/integration/...
```

## License

MIT
