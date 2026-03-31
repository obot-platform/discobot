# Discobot Architecture

This document describes the overall architecture of Discobot, an IDE-like chat interface for managing coding sessions with AI agents.

## Component Documentation

- [UI Architecture](./ui/ARCHITECTURE.md) - Frontend React + Vite architecture
- [Server Documentation](../server/README.md) - Go backend server
- [Agent Documentation](../agent/README.md) - Container init process (PID 1)
- [Agent API Documentation](../agent-api/README.md) - Container agent API service
- [Proxy Documentation](../proxy/README.md) - HTTP/SOCKS5 proxy with header injection

## Overview

Discobot is a web-based development environment that lets users interact with AI coding agents (Claude Code, Gemini CLI, etc.) within isolated workspaces. Each workspace can contain multiple chat sessions, and users can configure different AI agents with custom prompts and MCP servers.

## Core Concepts

### Hierarchy

```
Project
└── Workspace (local folder or git repo)
    └── Session (chat thread with an agent)
        ├── Messages (conversation history)
        └── Files (diffs/changes made in session)
```

### Project

A multi-tenant container that owns all resources. In single-user mode, a default `local` project is used automatically.

- Owns: Workspaces, Agents, Credentials
- Has: Members with roles (owner, admin, member)
- Supports: Team collaboration via invitations

### Workspace

A working directory linked to either a local folder or a git repository.

| Field | Description |
|-------|-------------|
| path | Local path or git URL |
| sourceType | `local` or `git` |
| status | `initializing` → `cloning` → `ready` or `error` |

For git workspaces:
- Repository is cloned to a local cache
- Tracks current commit SHA
- Supports branch operations, diffs, commits

### Session

A chat thread within a workspace, bound to a specific AI agent configuration.

| Field | Description |
|-------|-------------|
| name | Display name for the session |
| status | Lifecycle state (see below) |
| agentId | Which agent configuration to use |
| workspaceId | Parent workspace |

Sessions also persist encrypted sandbox-only secret material when needed. For SSH-enabled sandboxes, Discobot stores a per-session SSH keypair encrypted at rest in the database and reuses it when the sandbox is recreated.

**Session Lifecycle:**
```
initializing → cloning → pulling_image → creating_sandbox → ready ⇄ running
                                                             ↓
                                                          stopped
                                   (any stage) → error
                                   (delete) → removing → removed
```

States:
- `ready`: Session is ready for chat requests
- `running`: Session has an active chat completion in progress
- `stopped`: Sandbox is stopped, will restart on demand
- `error`: Setup or operation failed

The `ready` ⇄ `running` transition happens automatically:
- When a chat request starts, status moves to `running`
- When the chat completes, status returns to `ready`
- On server startup, sessions in `running` state are reconciled with the agent API

### Agent

Configuration for an AI coding assistant. References a `SupportedAgentType` (Claude Code, Gemini CLI, etc.) and can customize:

- System prompt
- MCP servers (stdio or HTTP)
- Selected mode and model

One agent per project can be marked as `isDefault`.

### Credential

Encrypted storage for AI provider authentication:

- API keys (e.g., `ANTHROPIC_API_KEY`)
- OAuth tokens:
  - Anthropic OAuth: `CLAUDE_CODE_OAUTH_TOKEN`
  - GitHub Copilot: `GITHUB_TOKEN`
  - OpenAI Codex: `CODEX_TOKEN`
- Project-scoped credential UI metadata is served by `GET /api/projects/{projectId}/credentials/types`

Credentials are encrypted with AES-256-GCM before storage.

### Sandbox Secret Provisioning

Sandbox secrets follow the same encrypted-at-rest pattern as credentials and env sets.

- The server generates per-session SSH identities for sandbox use.
- Private/public key material is encrypted in the database before being stored on the session record.
- When a Docker sandbox is created, the decrypted key is copied into a private staging path inside the container (`/.discobot-secrets/ssh/`).
- During `discobot-agent setup`, the staged files are installed into `/home/discobot/.ssh/` as a named identity (`discobot_sandbox`), not as a default SSH key.
- Recreated sandboxes reuse the same session keypair so external hosts can continue trusting the same public key.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    React + Vite Frontend                         │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────────────┐ │
│  │  Sidebar │  │  Chat    │  │ Terminal │  │ File Diff Viewer │ │
│  │  Tree    │  │  Panel   │  │  View    │  │ (Tabbed)         │ │
│  └──────────┘  └──────────┘  └──────────┘  └──────────────────┘ │
│                         ↓ SWR Hooks                              │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │                    API Client Layer                         │ │
│  └────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
                              │ HTTP/SSE
                              ↓
┌─────────────────────────────────────────────────────────────────┐
│                         Go Backend                               │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────────────┐ │
│  │ Handlers │→ │ Services │→ │  Store   │→ │ GORM (DB)        │ │
│  └──────────┘  └──────────┘  └──────────┘  └──────────────────┘ │
│        ↓                                                         │
│  ┌──────────┐  ┌──────────┐  ┌──────────────────────────────┐   │
│  │   Auth   │  │   Git    │  │      Docker / Container      │   │
│  │ (OAuth)  │  │ Provider │  │       (Sandbox Runtime)      │   │
│  └──────────┘  └──────────┘  └──────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
         │                              │
         │               ┌──────────────┴──────────────┐
         │               ↓                             ↓
         │    ┌──────────────────────┐     ┌──────────────────────┐
         │    │   Agent Container    │     │   MITM Proxy         │
         │    │   (per session)      │     │   (per container)    │
         │    │   ┌──────────────┐   │     │   ┌──────────────┐   │
         │    │   │ discobot-agent   │   │     │   │ HTTP/SOCKS5  │   │
         │    │   │ (PID 1 init) │   │     │   │ + TLS MITM   │   │
         │    │   │      ↓       │   │     │   └──────────────┘   │
         │    │   │ discobot-agent-  │   │ ──▶ │                      │
         │    │   │ api + AI CLI │   │     │                      │
         │    │   └──────────────┘   │     │                      │
         │    └──────────────────────┘     └──────────────────────┘
         │                                            │
         └────────────────┬───────────────────────────┘
                          ↓
              ┌───────────┼───────────┐
              ↓           ↓           ↓
        ┌──────────┐ ┌──────────┐ ┌──────────┐
        │ SQLite   │ │ Postgres │ │ AI APIs  │
        │ (dev)    │ │ (prod)   │ │          │
        └──────────┘ └──────────┘ └──────────┘
```

## Data Model

### Entity Relationships

```
User ─────────┬──────────────── UserSession (login sessions)
              │
              └─── ProjectMember ─── Project
                        │               │
                        │               ├─── Workspace ─── Session ─── Message
                        │               │                     │
                        │               ├─── Agent ───────────┘
                        │               │      │
                        │               │      └─── AgentMCPServer
                        │               │
                        │               ├─── Credential
                        │               │
                        │               └─── TerminalHistory
                        │
                        └─── ProjectInvitation
```

### Key Models

| Model | Purpose |
|-------|---------|
| User | OAuth-authenticated user |
| UserSession | Login session (token hashed in DB) |
| Project | Multi-tenant container |
| ProjectMember | User ↔ Project with role |
| Workspace | Local folder or git repo |
| Session | Chat thread in workspace |
| Agent | AI agent configuration |
| AgentMCPServer | MCP server config per agent |
| Message | Chat message in session |
| Credential | Encrypted AI provider credentials |

## Frontend Architecture

For detailed UI architecture, see [UI Architecture](./ui/ARCHITECTURE.md).

### Tech Stack

- **Framework**: React Router 7 + Vite
- **Language**: TypeScript
- **Styling**: Tailwind CSS v4 with CSS custom properties
- **UI Components**: shadcn/ui (Radix primitives)
- **State Management**: SWR for data fetching
- **AI SDK**: Vercel AI SDK v5
- **Terminal**: xterm.js v6

### Layout Structure

```
┌─────────────────────────────────────────────────────────────┐
│ Header (session info, agent selector, theme toggle)         │
├───────────┬─────────────────────────────────────┬───────────┤
│           │                                     │           │
│ Sidebar   │      Main Content Area              │  File     │
│           │  ┌───────────────────────────────┐  │  Panel    │
│ Workspaces│  │   Tabbed Diff Viewer          │  │           │
│   └─Sessions│ │   (file changes per session) │  │  (tree    │
│           │  └───────────────────────────────┘  │   view)   │
│           │  ┌───────────────────────────────┐  │           │
│ Agents    │  │   Chat / Terminal (toggle)    │  │           │
│           │  └───────────────────────────────┘  │           │
└───────────┴─────────────────────────────────────┴───────────┘
```

### Key Components

| Component | Purpose |
|-----------|---------|
| `SidebarTree` | Workspace/session navigation |
| `AgentsPanel` | Agent list and selection |
| `ChatPanel` | AI conversation interface |
| `TerminalView` | xterm.js terminal emulator |
| `TabbedDiffView` | File diff viewer with tabs |
| `FilePanel` | Session file tree |

### Data Flow

```
User Action → SWR Hook → API Client → Backend
                ↓
            Cache Update → Component Re-render
```

SWR hooks provide:
- Automatic caching and revalidation
- Optimistic updates for mutations
- Loading and error states

## Backend Architecture

### Tech Stack

- **Language**: Go
- **Router**: Chi
- **ORM**: GORM (PostgreSQL + SQLite)
- **Auth**: OAuth (GitHub, Google)

### Layer Structure

```
Handler (HTTP) → Service (Business Logic) → Store (Data Access) → Database
```

| Layer | Responsibility |
|-------|----------------|
| Handler | Request parsing, response formatting, auth checks |
| Service | Business rules, cross-cutting concerns |
| Store | CRUD operations, query building |

### API Design

All resources are scoped under `/api/projects/{projectId}/`:

```
/api/projects/{projectId}/workspaces
/api/projects/{projectId}/workspaces/{id}/sessions
/api/projects/{projectId}/sessions/{id}
/api/projects/{projectId}/agents
/api/projects/{projectId}/credentials
```

### Authentication

1. OAuth login (GitHub/Google) at `/auth/login/{provider}`
2. Session token stored in HttpOnly cookie
3. Token hashed (SHA256) before DB storage
4. Middleware validates session on protected routes

**Anonymous Mode**: When `AUTH_ENABLED=false`, all requests use a default anonymous user with access to the `local` project.

## Proxy Architecture

The MITM proxy runs inside each agent container to:
- **Cache Docker registry pulls** (5-10x faster, 70-90% bandwidth reduction)
- Inject authentication headers for AI provider APIs
- Enforce domain allowlists for network isolation
- Log all outbound HTTP/HTTPS/SOCKS5 traffic

### Features

- **Docker registry caching**: Content-addressable caching of immutable blob layers and manifests
- **Multi-protocol**: HTTP, HTTPS (MITM), and SOCKS5 support
- **Automatic CA trust**: Generates CA certificate and installs in system trust store on startup
- **Node.js support**: Sets `NODE_EXTRA_CA_CERTS` for Electron apps (Claude Code)
- **Header injection**: Per-domain rules for setting/removing headers
- **Domain filtering**: Glob-pattern allowlists (e.g., `*.anthropic.com`)
- **TLS interception**: Dynamic certificate generation signed by container CA
- **Runtime configuration**: REST API for updating rules without restart
- **Workspace-aware**: Custom config via `.discobot/proxy/config.yaml`

### Data Flow

```
Container Process → Proxy (localhost:17080) → Cache → TLS MITM → Header Injection → Remote API
                          ↓
                    Proxy API (:17081)
                          ↓
                    Runtime config updates
```

### Docker Caching

The proxy caches Docker registry responses:
- **Blob layers**: `sha256:*` digests are immutable and safe to cache indefinitely
- **Manifests by digest**: Also immutable when referenced by `sha256:*`
- **LRU eviction**: 20GB cache limit with least-recently-used eviction
- **Persistent storage**: Cache survives container restarts at `/.data/proxy/cache`
- **Workspace config**: Teams can customize caching patterns per workspace

See [agent/docs/design/proxy-integration.md](../agent/docs/design/proxy-integration.md) for implementation details.

## Security Model

### Credential Encryption

AI provider credentials (API keys, OAuth tokens) are encrypted using AES-256-GCM before storage. The encryption key is configured via `ENCRYPTION_KEY` environment variable. At session startup, decrypted credential values are injected into the sandbox environment for the active project, including tool credentials such as `TAVILY_API_KEY` and `DISCOBOT_TOKEN`.

### Multi-tenancy

- All resources belong to a Project
- ProjectMember middleware validates membership on all project-scoped routes
- Roles: owner (full control), admin (manage members), member (use resources)

### Session Security

- Session tokens are random, high-entropy strings
- Only SHA256 hash stored in database
- HttpOnly cookies prevent XSS token theft
- 30-day expiration

## Planned Features

### Docker Terminal (Phase 8)

Each workspace will have an associated Docker container:
- WebSocket endpoint for PTY attachment
- Container lifecycle management
- Terminal history persistence

### AI Chat Streaming (Phase 9)

- Streaming chat endpoint
- Multi-provider support (Anthropic, OpenAI, Google)
- Tool use / function calling
- Message persistence

### Git Integration (Phase 7 - Complete)

- Abstract `git.Provider` interface
- Local provider with efficient caching
- Full git operations (clone, fetch, checkout, diff, commit)
- File read/write at any ref

## Open Questions

1. **Session isolation**: Should each session have its own container, or share a workspace container?

2. **File persistence**: How should file changes be persisted? Per-session branches? Stashed changes?

3. **Real-time collaboration**: Should multiple users be able to view/interact with the same session?

4. **Agent process management**: How to handle long-running agent processes? Separate daemon?

5. **Resource limits**: How to limit container resources (CPU, memory, disk) per workspace/session?

## References

- [UI Architecture](./ui/ARCHITECTURE.md) - Frontend architecture and components
- [Server README](../server/README.md) - Go backend documentation
- [Agent README](../agent/README.md) - Container init process documentation
- [Agent API README](../agent-api/README.md) - Container agent API documentation
- [CLAUDE.md](../CLAUDE.md) - AI coding agent guidelines
