# Frontend / Server Compatibility Changes

This document describes the changes needed in the discobot server and frontend to work with the `agent-go` backend. These changes are **not yet applied** — `agent-go` is currently standalone and does not affect the existing system.

## Route Changes

The agent-go API uses different route naming than the original TypeScript agent-api:

| Original (agent-api) | New (agent-go) | Notes |
|---|---|---|
| `GET /session/{id}/{agent}/chat` | `GET /threads/{id}/messages` | Renamed; JSON-only (no SSE via Accept header) |
| `GET /session/{id}/{agent}/chat` (SSE) | `GET /threads/{id}/chat/stream` | Separate SSE endpoint |
| `POST /session/{id}/{agent}/chat` | `POST /threads/{id}/chat` | Removed `{agent}` path param |
| `POST /session/{id}/{agent}/cancel` | `POST /threads/{id}/cancel` | Removed `{agent}` path param |
| `GET /session/{id}/{agent}/chat/status` | _(removed)_ | Server doesn't have equivalent |
| `GET /sessions` | `GET /threads` | Renamed |
| Question routes | `GET /threads/{id}/chat/question/{questionId}` | Path param instead of query |
| Answer routes | `POST /threads/{id}/chat/answer/{questionId}` | Path param instead of body |

### Key differences

1. **No `{agent}` path parameter** — routes are `/threads/{id}/...` not `/session/{id}/{agent}/...`
2. **`sessions` → `threads`** — a session can have multiple threads
3. **JSON and SSE are separate endpoints** — `GET /messages` for JSON history, `GET /chat/stream` for SSE
4. **Chat SSE is long-lived** — `GET /chat/stream` no longer closes after a single completion; it stays connected and emits `ping` events while idle, without a terminal `done` event
5. **Sessions stay `ready` while chat streams** — completion activity is no longer reflected as a session-level `running` state

## Server Changes Needed

### `server/internal/sandbox/sandboxapi/types.go`

Update sandbox API types to match the new route structure:
- Rename session-based types to thread-based types
- Update question/answer request/response types to use path params

### `server/internal/service/sandbox_client.go` and `sandbox_session_client.go`

Update HTTP client calls to use new routes:
- `/session/{id}/{agent}/chat` → `/threads/{id}/chat`
- `/session/{id}/{agent}/cancel` → `/threads/{id}/cancel`
- `/sessions` → `/threads`
- Question/answer routes to use path params

### `server/internal/handler/chat.go`

Update handler to proxy to new agent-go routes and adapt request/response formats.
The downstream proxying logic also needs to treat chat SSE as a persistent
connection with periodic `ping` events between completions, and it should not
expect a terminal `done` event from the stream itself.

### `server/internal/service/chat.go`

Update service layer to match new route structure.

## Frontend Changes Needed

### `lib/api-types.ts`

- Update types for question/answer to use `questionId` path param
- Thread-based naming where applicable

### `lib/api-client.ts`

- Update API client methods to call new routes
- Question/answer endpoints use path params instead of query/body
- Keep chat SSE subscriptions open across multiple completions and ignore
  `ping` events except for connection liveness

### `components/ai-elements/tool-renderers/ask-user-question-tool.tsx`

- Update to use new question ID-based routes

### `components/ide/ask-question-dialog.tsx`

- Update to use new answer submission route

## Message Format

The agent-go message format is compatible with the existing UI. Key points:

- Messages use the same `role` + `parts[]` structure
- Part types (`text`, `tool-call`, `tool-result`, `reasoning`, etc.) are the same
- UI projection (`ProjectUIMessages`) produces the same JSON format
- SSE streaming uses the same chunk format
- Chat SSE additionally emits `ping` events with `{}` payloads to keep the
  connection alive between completions
- Chat SSE may emit `data-thread-name` chunks before the assistant reply when
  the backend auto-generates a friendly thread title from the first user prompt

## System Prompt Delivery

Agent-go delivers the system prompt and user instructions differently than a simple concatenation:

1. **System prompt** (behavioral instructions) → injected as `role: "system"` root message
2. **User instructions** (CLAUDE.md, AGENTS.md, rules) → injected as `role: "user"` message with `<system-reminder>` tags, matching Claude Code's delivery format

This is transparent to the frontend — these messages appear in the thread history like any other messages.
