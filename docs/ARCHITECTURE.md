# Discobot Architecture

This document describes the overall architecture of Discobot, an IDE-like chat interface for managing coding sessions with AI agents.

## Component Documentation

- [UI Architecture](./ui/ARCHITECTURE.md) - Frontend Svelte + Vite architecture
- [Server Documentation](../server/README.md) - Go backend server
- [Sandbox Init Documentation](../sandbox-init/README.md) - Container init process (PID 1)
- [Agent API Documentation](../agent-api/README.md) - Container agent API service
- [Proxy Documentation](../proxy/README.md) - HTTP/SOCKS5 proxy with header injection

## Overview

Discobot is a web-based development environment built around Discobot's own coding agent runtime. Each workspace can contain multiple chat sessions, and users can configure the built-in agent with custom prompts, MCP servers, and supported model providers such as Anthropic and OpenAI.

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

| Field      | Description                                     |
| ---------- | ----------------------------------------------- |
| path       | Local path or git URL                           |
| sourceType | `local` or `git`                                |
| status     | `initializing` → `cloning` → `ready` or `error` |

For git workspaces:

- The server passes the git URL and target ref to the sandbox
- The sandbox agent performs the clone locally
- Git object mirrors are cached in the sandbox's persistent `/home/discobot/.cache`
- Tracks current commit SHA
- Supports branch operations, diffs, commits

### Session

A chat thread within a workspace, bound to a specific AI agent configuration.

| Field       | Description                      |
| ----------- | -------------------------------- |
| name        | Display name for the session     |
| status      | Lifecycle state (see below)      |
| agentId     | Which agent configuration to use |
| workspaceId | Parent workspace                 |

Prompt submissions are also persisted server-side before delivery to the sandbox. This durable handoff lets the server replay pending prompts after a restart or sandbox-creation failure so a submitted user turn is not lost while waiting for the sandbox to become ready. The stored prompt payload is encrypted at rest and is cleared immediately after the sandbox accepts the submission, so the database retains only the minimal metadata needed for idempotent retries and status reporting.

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
- On server startup, VM-backed sandboxes are re-enumerated from persisted
  project disks before image reconciliation runs, so stale containers are
  still visible after a restart
- If a stopped sandbox was built from an older runtime image, Discobot removes
  it and recreates it on demand instead of restarting the stale container

### Agent

Configuration for Discobot's built-in AI coding assistant. It stores the project's agent settings and can customize:

- System prompt
- MCP servers (stdio or HTTP)
- Selected mode and model

One agent per project can be marked as `isDefault`.

### Credential

Encrypted storage for provider authentication and custom thread-assigned environment bundles:

- API keys (e.g., `ANTHROPIC_API_KEY`)
- OAuth tokens:
  - Anthropic OAuth tokens
  - OpenAI OAuth tokens
  - Other provider-specific auth tokens as supported
- Custom credentials containing one or more arbitrary environment variable entries
- Project-scoped credential UI metadata is served by `GET /api/projects/{projectId}/credentials/types`
- Threads store only selected credential IDs; secret material remains encrypted in server storage
- Credentials can be marked `agentVisible=false` so they remain available to server/agent-go internals without being exposed to the agent tool environment

Credentials are encrypted with AES-256-GCM before storage.

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
         │    │   │ systemd PID 1 │   │     │   │ + TLS MITM   │   │
         │    │   │      ↓       │   │     │   └──────────────┘   │
         │    │   │ sandbox setup│   │     │                      │
         │    │   │ script       │   │     │                      │
         │    │   │      ↓       │   │     │                      │
         │    │   │ discobot-agent-api      │   │ ──▶ │                      │
         │    │   │ + AI CLI     │   │     │                      │
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

The per-session agent container now also owns a session-scoped browser runtime.
The runtime image packages the Browser Use harness as the `browser-harness`
command and exposes a built-in browser-harness skill to teach the agent how to
use it. `discobot-agent-api` can launch a local Chromium profile, expose a
CDP WebSocket endpoint at `/sessions/{sessionId}/browser/cdp?token=...`, and
inject that endpoint into agent tool subprocesses. This keeps browser state
shared across threads within a session while preserving a stable seam for a
future host- or Electron-backed browser broker. Tool subprocesses receive a
thread-scoped variant of that URL so CDP request/response pairs can also be
persisted against the active turn in a separate append-only browser event log.
For action-like CDP commands, the proxy also captures a follow-up screenshot,
saves it in a thread-scoped content-addressable browser artifact store, stores
the file reference on the corresponding browser event record, and emits a live
browser-event data chunk into the thread stream while the turn is active.
Browser artifacts are exposed to the UI with `artifacts://...` URIs and are
read back through a thread-scoped artifact endpoint instead of the workspace
file API so screenshots remain outside the editable workspace tree.

Sandbox images replace `/usr/bin/sudo` with a Discobot-owned gate binary. The
gate reads `/etc/discobot/sudo-gate.json`, which must be root-owned and
inaccessible to group/other users, to find the real sudo binary and local
agent authorization endpoint. It calls the local `discobot-agent-api`
`/sudo/authorize` endpoint before it execs the real sudo binary from a
root-only path. Interactive console terminals receive a per-terminal random
console sudo token in their exec environment. The agent API registers that token
internally while creating the PTY, before the process can invoke `sudo`, and
revokes it when the persistent terminal PTY exits. There is no standalone HTTP
API for registering or revoking console sudo tokens. Console sudo is allowed
only when invoked from a TTY with that internally registered token, so console
sudo grants do not remain valid after the terminal session closes.
Agent Bash tool invocations must first obtain an approved `DISCOBOT_SUDO_TOKEN`
credential through the normal request-credential flow; the Bash tool injects
the returned credential/use metadata so the gate can validate the sudo request
against the approved use before privilege escalation. Sudo approvals carry the
same per-use expiration metadata as other credential-use grants, and the local
agent rejects expired approved uses even if it has not yet received a refreshed
credential header. Exec API requests that ask to run as a different OS user are
also started through `/usr/bin/sudo` instead of direct process credential
switching, so root and non-root user acquisition follow the same gate and
sudoers policy.

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

| Model          | Purpose                            |
| -------------- | ---------------------------------- |
| User           | OAuth-authenticated user           |
| UserSession    | Login session (token hashed in DB) |
| Project        | Multi-tenant container             |
| ProjectMember  | User ↔ Project with role           |
| Workspace      | Local folder or git repo           |
| Session        | Chat thread in workspace           |
| Agent          | AI agent configuration             |
| AgentMCPServer | MCP server config per agent        |
| Message        | Chat message in session            |
| Credential     | Encrypted AI provider credentials  |

## Frontend Architecture

For detailed UI architecture, see [UI Architecture](./ui/ARCHITECTURE.md).

### Tech Stack

- **Framework**: React Router 7 + Vite
- **Language**: TypeScript
- **Styling**: Tailwind CSS v4 with CSS custom properties
- **UI Components**: shadcn/ui (Radix primitives)
- **State Management**: SWR for data fetching
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

| Component        | Purpose                      |
| ---------------- | ---------------------------- |
| `SidebarTree`    | Workspace/session navigation |
| `AgentsPanel`    | Agent list and selection     |
| `ChatPanel`      | AI conversation interface    |
| `TerminalView`   | xterm.js terminal emulator   |
| `TabbedDiffView` | File diff viewer with tabs   |
| `FilePanel`      | Session file tree            |

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

| Layer   | Responsibility                                    |
| ------- | ------------------------------------------------- |
| Handler | Request parsing, response formatting, auth checks |
| Service | Business rules, cross-cutting concerns            |
| Store   | CRUD operations, query building                   |

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
- **Automatic CA trust**: `proxy init-certs` generates the CA and installs system/NSS trust during startup
- **Node.js support**: Sets `NODE_EXTRA_CA_CERTS` for Node.js and Electron-based tooling
- **Header injection**: Per-domain rules for setting/removing headers
- **Domain filtering**: Glob-pattern allowlists (e.g., `*.anthropic.com`)
- **TLS interception**: Dynamic certificate generation signed by container CA
- **Runtime configuration**: REST API for updating rules without restart
- **Safe defaults**: Sandbox init writes a built-in proxy config instead of reading untrusted workspace files

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
- **Persistent storage**: Cache survives container restarts at `/.data/cache/proxy`
- **Safe config source**: Startup uses the built-in proxy config written by sandbox-init

See [sandbox-init/docs/design/proxy-integration.md](../sandbox-init/docs/design/proxy-integration.md) for implementation details.

## Security Model

### Credential Encryption

AI provider credentials (API keys, OAuth tokens, and custom environment-variable bundles) are encrypted using AES-256-GCM before storage. The encryption key is configured via `ENCRYPTION_KEY` environment variable. When a thread runs, the server resolves that thread’s selected credential IDs, sends the assigned credentials to agent-go, and agent-go only exposes entries marked `agentVisible=true` to the tool environment while still keeping internal-only credentials available to Go-side provider logic.

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
- Multi-provider support (Anthropic and OpenAI today, with more providers to come)
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

## HCS Windows Guest Artifacts

Windows HCS packaging mirrors the macOS VZ guest image flow:

- `Dockerfile` target `vz-image-builder` creates the shared Linux rootfs:
  - `/rootfs.squashfs`
  - `/discobot-rootfs.tar.zst`
- `Dockerfile` target `hcs-image` converts `/rootfs.squashfs` into:
  - `/discobot-rootfs.vhd`
  - `/wsl-kernel`
  - `/kernel-version`
  - `/wsl-kernel-ref`
  - `/HcsLinuxVmLauncher.exe`
  - `/gvproxy.exe`
  - `/gvforwarder`
- The fixed VHD stores the SquashFS image at byte zero and appends the VHD
  footer required by HCS virtual disk attachment.
- The HCS guest boots the SquashFS VHD with:
  - `root=/dev/sda`
  - `rootfstype=squashfs`
- The WSL2 kernel is built from Microsoft's
  `microsoft/WSL2-Linux-Kernel` release tag selected by `WSL_KERNEL_REF`.
- CI builds `hcs-image` for both `linux/amd64` and `linux/arm64`, so Windows
  Electron packages get matching Intel and Arm64 WSL2 kernels.
- `/usr/local/bin/gvforwarder` is enabled through `gvforwarder.service` in the
  guest image for HCS `user-vsock` networking.
- `scripts/extract-hcs-image.mjs` extracts the image into
  `electron/resources/hcs`.
- Electron sets:
  - `HCS_LAUNCHER_PATH`
  - `HCS_KERNEL_PATH`
  - `HCS_ROOT_DISK_PATH`

Manual build commands:

```bash
# Build the HCS artifact image for Intel.
depot build \
  --target hcs-image \
  --platform linux/amd64 \
  --build-arg PRELOAD_IMAGE=ghcr.io/obot-platform/discobot:main \
  --build-arg WSL_KERNEL_REF=linux-msft-wsl-6.18.26.3 \
  --output type=oci,dest=/tmp/discobot-hcs-amd64.oci \
  .

# Build the HCS artifact image for Arm64.
depot build \
  --target hcs-image \
  --platform linux/arm64 \
  --build-arg PRELOAD_IMAGE=ghcr.io/obot-platform/discobot:main \
  --build-arg WSL_KERNEL_REF=linux-msft-wsl-6.18.26.3 \
  --output type=oci,dest=/tmp/discobot-hcs-arm64.oci \
  .
```

## References

- [UI Architecture](./ui/ARCHITECTURE.md) - Frontend architecture and components
- [Server README](../server/README.md) - Go backend documentation
- [Sandbox Init README](../sandbox-init/README.md) - Container init process documentation
- [Agent API README](../agent-api/README.md) - Container agent API documentation
- [CLAUDE.md](../CLAUDE.md) - Repository agent guidelines
