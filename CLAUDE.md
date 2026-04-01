# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Discobot is a coding agent manager. It runs, monitors, and manages AI coding agents (currently Claude Code) across isolated sandboxed sessions. Each session gets its own container with a copy-on-write filesystem, MITM proxy, and agent API.

## Commands

### Development

```bash
pnpm dev                # Start all services (backend + Tauri app)
pnpm dev:backend        # Backend only (Svelte UI + Go server + agent watcher)
pnpm dev:app            # Tauri desktop app only
pnpm dev:frontend       # Active frontend only (Svelte UI on port 3100)
pnpm dev:server         # Go backend with hot-reload via air (port 3001)
```

### Build

```bash
pnpm build              # Build server + active frontend + Tauri app
pnpm build:server       # Build Go server binary
pnpm typecheck          # TypeScript type checking
```

### Lint & Format

```bash
pnpm check              # Run active frontend checks, backend checks, and shellcheck
pnpm check:fix          # Run UI Prettier + ESLint fixes, backend autofixes, and shellcheck
pnpm check:frontend     # Delegate to the Svelte UI's Prettier + ESLint + typecheck flow
pnpm check:backend      # golangci-lint (server + proxy)
pnpm format             # Run the Svelte UI Prettier formatter
```

### Tests

```bash
pnpm test               # All unit + integration tests
pnpm test:unit          # All unit tests (server, proxy, agent-go, watcher, frontend)
pnpm test:frontend      # Frontend tests only

# Go tests
pnpm test:server        # All server tests
pnpm test:server:unit   # Server unit tests (excludes integration/)
pnpm test:server:integration  # Server integration tests
pnpm test:proxy         # All proxy tests
pnpm test:agent-go      # Agent Go tests

# Single Go test
cd server && go test -v -run TestName ./internal/path/...

# Single frontend test
node --import ./test/setup.js --import tsx --test lib/hooks/use-messages.test.ts
```

### CI

```bash
pnpm ci                 # Full CI pipeline: check → test:unit → build
```

## Architecture

### Components

| Component | Language | Port | Purpose |
|-----------|----------|------|---------|
| Frontend | TypeScript (Svelte + Vite) | 3100 | Active web UI |
| Server | Go (Chi + GORM) | 3001 | REST API, session orchestration, container management |
| Agent | Go | — | Container PID 1 init process (workspace setup, AgentFS mount) |
| Agent API | Go | 3002 | Per-container API that drives the AI CLI, SSE streaming |
| Proxy | Go | 17080/17081 | Per-container MITM proxy (auth header injection, Docker registry caching) |

### Data Flow

```
Frontend → REST API (/api/projects/{projectId}/...) → Go Server
                                                        ↓
                                              Docker/VM Container
                                              ├── Agent (PID 1 init)
                                              ├── Agent API (chat/SSE)
                                              └── Proxy (MITM + cache)
```

### Backend Layers

```
Handler (HTTP) → Service (Business Logic) → Store (Data Access) → GORM (SQLite/PostgreSQL)
```

### Resource Hierarchy

```
Project → Workspace (git repo or local folder) → Session (chat thread + container) → Messages + Files
       → Agent (AI config: type, prompt, MCP servers, mode, model)
       → Credential (encrypted API keys / OAuth tokens)
```

### Frontend Patterns

- `./ui` is the current UI and should be used for active frontend work.
- `./components` and `./lib` are the old React UI, are slated for deletion, and do not need to be maintained unless a task explicitly targets them.
- **Data fetching**: SWR hooks in `lib/hooks/` with optimistic mutations
- **API client**: `lib/api-client.ts` — all calls go through `getApiBase()` (`/api/projects/local/...`)
- **Types**: All shared interfaces in `lib/api-types.ts`
- **Styling**: Tailwind CSS v4 with CSS custom properties. Use design tokens (`bg-background`, `text-foreground`, `border-border`) and IDE tokens (`bg-tree-hover`, `bg-diff-add`)
- **Icons**: Theme-aware via `IconRenderer` component. SVGs with `currentColor` must be inlined, not `<img>`
- **AI chat**: Vercel AI SDK v6 with `useChat` hook and custom elements in `components/ai-elements/`
- **React Compiler**: Enabled via babel plugin — run `/vercel-react-best-practices` skill when working on React code

### Svelte UI (`./ui`)

The Svelte UI is the active frontend. Build and develop it from the `./ui` directory:

```bash
cd ui && pnpm build       # Production build
cd ui && pnpm dev         # Dev server (port 3100)
cd ui && pnpm typecheck   # Type-check (svelte-check)
```

#### Component folders

Components live under `ui/src/lib/components/` in three folders, each with a distinct role:

| Folder | Role | Context |
|--------|------|---------|
| `ui/` | Pure primitives — buttons, inputs, dialogs, etc. | None |
| `ai/` | Self-contained compound components | Component-local only |
| `app/` | App shell — session UI, composer, panels | Global app/session/thread contexts |

**`ui/` is always pure.** Never add context consumers here.

**`ai/` is self-contained.** Each subdirectory is a compound component group with its own `context.ts`. The root component (e.g. `AudioPlayer.svelte`) sets local context; children (e.g. `AudioPlayerPlayButton.svelte`) consume it. These components never use the global app/session/thread contexts.

**`app/` root** contains context consumers and providers — every component here reads from at least one global context. **`app/parts/`** contains pure sub-components that are props-only and used as implementation details by the context consumers at the root. When adding to `app/`, the rule is simple: if it calls `useAppContext`, `useSessionContext`, or `useThreadContext`, it belongs at the `app/` root; if it only takes props, it belongs in `app/parts/`.

#### Global context system

Three contexts flow top-down, each set by a single provider:

| Context | Provider | Provides |
|---------|----------|---------|
| `AppContext` | `routes/+layout.svelte` | Sessions, workspaces, models, credentials, preferences |
| `SessionContext` | `app/SessionWorkspace.svelte` | Threads, files, hooks, services, session credentials |
| `ThreadContext` | `app/ThreadWorkspace.svelte` | Conversation, messages, plan entries |

Access via `useAppContext()`, `useSessionContext()`, `useThreadContext()` from `$lib/context/`.

#### Pure vs context consumer

Make a component **pure** when it can be described and tested without knowing its parent, and all the data it displays is passed directly via props.

Make a component a **context consumer** when it would require threading the same data through multiple intermediate components that don't use it, or when it needs to coordinate with siblings that share the same ambient state.

The practical test: if removing `useXxxContext()` would mean adding three or more props that just relay data the context already holds, it belongs in context. If the component makes sense anywhere in the tree without a specific ancestor, it should be pure.

### Adding Features

For active UI work, prefer `./ui`. The steps below apply to the legacy React UI only and generally should be ignored unless the task explicitly targets that code.

1. Define types in `lib/api-types.ts`
2. Add Go handler/service/store in `server/internal/`
3. Add API client method in `lib/api-client.ts`
4. Create SWR hook in `lib/hooks/`
5. Build UI in `components/app/`

## Testing

**Frontend tests use Node's built-in `node:test`** — NOT vitest or jest.

The `test/setup.js` file initializes jsdom globals and must be loaded BEFORE React via `--import`. This order is critical:

```bash
node --import ./test/setup.js --import tsx --test <test-file>
```

**Go tests** use standard `go test`. Integration tests are under `*/internal/integration/`.

## Formatting & Style

- **Package manager**: pnpm only (never npm or yarn)
- **TypeScript / Svelte UI**: Prettier + ESLint — tabs, double quotes, organized imports where applicable
- **Go**: gofmt + goimports with local prefix `github.com/obot-platform/discobot`
- **Go version**: 1.26 — use `new(value)` to create a pointer to a value (e.g. `new(true)` for `*bool`); avoid `boolPtr`/`intPtr` helper functions
- **Go linters**: golangci-lint (errcheck, govet, staticcheck, revive, unused, etc.)

## Documentation

When making changes, update the relevant docs:

- `docs/ARCHITECTURE.md` — System-wide architecture
- `docs/ui/ARCHITECTURE.md` — Frontend patterns
- `docs/ui/design/` — UI module design docs
- `server/docs/` — Server architecture and design docs
- `agent/docs/` — Agent init process docs
- `agent-go/docs/` — Agent API docs
- `server/README.md`, `agent/README.md`, `agent-go/README.md`, `proxy/README.md` — Component READMEs

## Known Quirks

1. **ResizeObserver errors**: Suppressed globally via `ResizeObserverFix` component
2. **Monaco Editor errors**: Internal cursor tracking errors suppressed via `MonacoEditorFix` component
3. **Terminal resize**: Uses debounced `requestAnimationFrame` to avoid loops
4. **Icon rendering**: SVGs with `currentColor` must be inlined, not used as `<img>`
