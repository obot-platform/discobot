# UI Architecture

This document describes the architecture of the Discobot frontend, a React application built with Vite and React Router 7 that provides an IDE-like chat interface for AI coding agents.

## In-progress Svelte redesign

The repository now also contains an **isolated Svelte 5 redesign workspace** under `ui/`.

- `ui/` is currently a standalone SvelteKit SPA scaffold for the upcoming UI rewrite.
- `/` is now the redesign home/shell wireframe, while `/gallery` is the component exploration route.
- `/ai-gallery` remains the AI component gallery, and `/ai-gallery/conversation-pane` is the dedicated mocked `ConversationPane` sandbox for iterating on message-interaction UX without a live session.
- The Svelte shell now uses layered context state (`AppContext` → `SessionContext`) where AppContext owns user-accessible sessions and SessionContext owns active sandbox + thread-aware panel/chat state, including thread-level credential assignment for agent-visible secrets.
- The redesign dock terminal panel is now a live `ghostty-web` terminal that attaches to the existing session PTY WebSocket endpoint, while keeping session-aware state in the `app/` context consumers and passing only props into `app/parts/` components.
- The redesign files panel now uses raw Monaco integration with session-owned open tabs, models, buffers, and view state so navigating away from the editor and back restores the same file state within the current session, while syncing Monaco to the active app color scheme, exposing explorer context-menu rename/delete actions for files, supporting an internally resizable explorer width with an invisible drag handle, prompting before closing a dirty tab whose unsaved draft will be restored on reopen, rendering inline image previews with fullscreen/download support, rendering PDFs inline with native browser preview, and giving Markdown files preview, split, and editor modes.
- The redesign diff review panel now renders real per-file session diffs with `@pierre/diffs`, including split/unified mode, worker-pool syntax highlighting, path/language-scoped diff input cache keys to avoid worker-side highlight reuse across files, lazy per-file diff loading, virtualized rendering for large inline diffs, patch-hash approval persistence, and deleted-file snapshot reconstruction with a base-file fallback when needed.
- The shell chrome (header + toolbar + sidebar) now uses a shared Discobot brand component and shadcn Svelte controls for consistent sizing/interaction styles.
- App-wide document metadata for the Svelte redesign lives in `ui/src/app.html`; favicon assets are served from `ui/static/` and are copied from the canonical Tauri icon set in `src-tauri/icons/` so desktop and web surfaces stay in sync.
- The redesign conversation pane now renders session-scoped conversation fixtures (`SessionData.conversation`) instead of hardcoded transcript text, so thread/session state changes are reflected in the timeline.
- The Svelte conversation pane groups messages into consistent turn shells (`1..N` user messages followed by `0..1` assistant message) and snapshots reserved min-height only when a new turn is submitted, avoiding the old live bottom-spacer resize loop while keeping the active turn anchored near the bottom of the viewport.
- The Svelte redesign app context now owns a project-events SSE subscription that reloads only the affected session/workspace objects for update events, while still doing full refreshes on reconnect and tracking startup tasks for a global startup-status banner during backend initialization work.
- The Svelte redesign store layer now coalesces list and item reloads into at most one active request plus one queued follow-up per resource key, so reconnect/SSE bursts avoid duplicate fetches while later callers still receive data from their request time or newer.
- The Svelte redesign now includes a thread chat stream reducer (`ui/src/lib/session/runtime/chat-stream-state.ts`) that buffers `history-start`/`history-message`/`history-end` replay events, applies explicit `chunk`/`done` SSE events to materialize AI SDK `UIMessage[]`, and surfaces mode/model/reasoning metadata updates for thread state.
- The Svelte redesign now includes a native markdown engine under `ui/src/lib/markdown/` that preprocesses streaming content with `remend`, splits streamed markdown into incremental blocks, parses markdown with unified/remark/rehype, and renders AI message + reasoning content without a React island.
- Markdown link safety in that renderer stays Svelte-native via the existing Tauri-aware link safety modal and URL opening behavior.
- The Svelte AI component barrel now includes parity ports for `agent`, `code-block`, `inline-citation`, `sandbox`, `file-tree`, `prompt-input`, `speech-input`, `audio-player`, `canvas`, `edge`, `image-attachment`, and `link-safety-modal`.
- `AskUserQuestion` tool rendering now includes an interactive wizard flow in Svelte with step navigation, multi-select/other answers, and question fetch/submit endpoints when session context is available.
- Plan-mode tools (`EnterPlanMode`, `ExitPlanMode`) now have dedicated Svelte renderers, and the composer exposes the latest plan in a dialog control beside the todo/hooks affordances.
- Thread context now owns per-thread composer mode/model/reasoning state plus staged `next*` submit values, so reloading a thread restores its current settings while in-progress composer changes remain local until submit. Reasoning is level-based and sourced from each model's advertised `reasoningLevels` / `defaultReasoning` metadata instead of a simple thinking on/off toggle.
- Dynamic tool cards in the Svelte conversation pane now render inside a shared collapsible shell with an in-card raw/optimized toggle; specialized renderers own the parsed presentation, while `ToolOutput` is reserved for generic or forced-raw fallbacks.
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
