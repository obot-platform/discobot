# UI Architecture

This document describes the architecture of the Discobot frontend, a React application built with Vite and React Router 7 that provides an IDE-like chat interface for AI coding agents.

## In-progress Svelte redesign

The repository now also contains an **isolated Svelte 5 redesign workspace** under `ui/`.

- `ui/` is currently a standalone SvelteKit SPA scaffold for the upcoming UI rewrite.
- `/` is now the redesign home/shell wireframe, while `/gallery` is the component exploration route.
- The Svelte shell now uses layered context state (`AppContext` → `SessionContext`) where AppContext owns user-accessible sessions and SessionContext owns active sandbox + thread-aware panel/chat state.
- The shell chrome (header + toolbar + sidebar) now uses a shared Discobot brand component and shadcn Svelte controls for consistent sizing/interaction styles.
- The redesign conversation pane now renders session-scoped conversation fixtures (`SessionData.conversation`) instead of hardcoded transcript text, so thread/session state changes are reflected in the timeline.
- The Svelte redesign now includes a thread chat stream reducer (`ui/src/lib/session/runtime/chat-stream-state.ts`) that buffers `history-start`/`history-message`/`history-end` replay events, applies explicit `chunk`/`done` SSE events to materialize AI SDK `UIMessage[]`, and surfaces mode/model/reasoning metadata updates for thread state.
- Markdown-rich AI message and reasoning rendering in the Svelte redesign uses a focused React-island wrapper around `streamdown` for parity while the rest of the surface remains Svelte-native.
- Streamdown link safety in that island uses Tauri-aware URL opening behavior (`@tauri-apps/plugin-opener` in desktop mode, browser-safe fallback otherwise).
- The Svelte AI component barrel now includes parity ports for `agent`, `code-block`, `inline-citation`, `sandbox`, `file-tree`, `prompt-input`, `speech-input`, `audio-player`, `canvas`, `edge`, `image-attachment`, and `link-safety-modal`.
- `AskUserQuestion` tool rendering now includes an interactive wizard flow in Svelte with step navigation, multi-select/other answers, and question fetch/submit endpoints when session context is available.
- The existing root React app remains the active production frontend and the one wired to `src-tauri/`.
- Use `pnpm ui:dev`, `pnpm ui:dev:backend`, `pnpm ui:build`, and `pnpm ui:typecheck` when working on the redesign workspace.
- `.discobot/services/ui-svelte.sh` exposes the redesign as a Discobot preview service on port `3100` while starting the backend and agent watcher alongside it.

Until the migration is complete, treat `ui/` as a parallel frontend track rather than part of the current React runtime.

## Overview

The UI is a single-page application built with React 19 and React Router. It renders an IDE-style interface with resizable panels for workspace navigation, chat/terminal, and file diffs.

```
┌─────────────────────────────────────────────────────────────────┐
│                      Header (logo, controls)                     │
├─────────────────────────────────────────────────────────────────┤
│ Left Sidebar  │              Main Content                        │
│ ┌───────────┐ │  ┌─────────────────────────────────────────┐    │
│ │ Workspace │ │  │           Diff Panel (tabs)             │    │
│ │   Tree    │ │  ├─────────────────────────────────────────┤    │
│ ├───────────┤ │  │        Bottom Panel                     │    │
│ │  Agents   │ │  │     (Chat or Terminal)                  │    │
│ │   Panel   │ │  └─────────────────────────────────────────┘    │
│ └───────────┘ │                                                  │
└─────────────────────────────────────────────────────────────────┘
```

## Directory Structure

```
src/
├── main.tsx           # Vite entry point with BrowserRouter
├── App.tsx            # Root component with Routes and providers
├── globals.css        # Theme tokens and Tailwind config
└── pages/
    └── HomePage.tsx   # Main IDE page orchestration

components/
├── ai-elements/       # Vercel AI SDK UI wrappers
├── ide/              # IDE-specific components
│   ├── layout/       # Panel layout components
│   └── *.tsx         # Feature components
└── ui/               # shadcn/ui base components

lib/
├── api-client.ts     # REST API client
├── api-types.ts      # TypeScript interfaces
├── api-config.ts     # API configuration
├── hooks/            # Custom React hooks
└── plugins/          # Auth provider plugins
```

## Module Documentation

- [Layout Module](./design/layout.md) - Panel system and page composition
- [Chat Module](./design/chat.md) - AI chat integration with Vercel AI SDK
- [Data Layer](./design/data-layer.md) - SWR hooks and API client
- [Components Module](./design/components.md) - UI component organization
- [Theming Module](./design/theming.md) - Theme system and design tokens

## Key Architectural Decisions

### 1. Single-Page Application with React Router

The application uses React Router 7 with a single main route (`/`) that renders the `HomePage` component. Panel content is driven by React state and context (`MainPanelProvider`) rather than URL routes. This provides a desktop-like IDE experience.

### 2. SWR for Server State

All server data is managed through SWR hooks in `lib/hooks/`. This provides:
- Automatic caching and revalidation
- Optimistic updates via mutations
- Built-in loading and error states

### 3. Vite Dev Server Proxy to Go Backend

API calls go to `/api/*` which Vite's dev server proxies to the Go backend at `localhost:3001`. This allows the frontend to use relative URLs while the backend handles business logic.

### 4. Server-Sent Events for Real-time Updates

Real-time updates flow through a layered architecture:
1. `useProjectEvents` hook manages the SSE connection and calls callbacks
2. `ProjectEventsProvider` uses the hook and routes events to cache mutations
3. Cache mutation functions (`invalidateSession`, `invalidateWorkspaces`, etc.) are exported from hooks

This design keeps SWR key knowledge in the hooks that define them, while the provider handles event routing.

### 5. Vercel AI SDK for Chat

Chat uses the `useChat` hook from `@ai-sdk/react`. Messages stream via SSE and support custom UI parts for tool invocations and reasoning.

## Data Flow

### User Initiates Chat

```
1. User types message in ChatPanel
2. useChat sends POST /api/chat (proxied to Go server)
3. Go server creates session, starts container
4. Go server proxies to container's /chat endpoint
5. Container streams SSE response
6. useChat updates messages state
7. React re-renders with new content
```

### Real-time Updates

```
1. Backend emits SSE event (session status changed)
2. useProjectEvents receives event, calls onSessionUpdated callback
3. ProjectEventsProvider calls invalidateSession() or removeSessionFromCache()
4. SWR refetches from API (or removes from cache for deletions)
5. React re-renders with fresh data
```

### Panel State Persistence

```
1. User resizes/collapses panels
2. ResizeHandle callback updates state
3. usePersistedState syncs to localStorage
4. On page reload, state restored from localStorage
```

## Dependencies

| Package | Purpose |
|---------|---------|
| `vite` | Build tool and dev server |
| `react-router` | Client-side routing |
| `react` | UI library |
| `ai`, `@ai-sdk/react` | Vercel AI SDK for chat |
| `swr` | Data fetching and caching |
| `@radix-ui/*` | Accessible UI primitives |
| `tailwindcss` | Utility-first CSS |
| `next-themes` | Theme switching |
| `@xterm/xterm` | Terminal emulator |
| `lucide-react` | Icons |
