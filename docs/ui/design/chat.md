# Chat Module

The chat module handles AI conversations using the Vercel AI SDK v5, providing streaming responses and custom UI elements.

## Files

| File | Description |
|------|-------------|
| `components/ide/chat-panel.tsx` | Main chat interface component |
| `components/ai-elements/conversation.tsx` | Message list container |
| `components/ai-elements/message.tsx` | Individual message renderer |
| `components/ai-elements/prompt-input.tsx` | Chat input with attachments |
| `lib/hooks/use-messages.ts` | SWR hook for message history |

## Architecture

### Chat Flow

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   ChatPanel     │───▶│   Go Backend    │───▶│   Container     │
│   (useChat)     │    │   /api/chat     │    │   /chat         │
└─────────────────┘    └─────────────────┘    └─────────────────┘
        │                      │                      │
        │◀─────────── SSE Stream ◀────────────────────│
```

### Message Types

The chat uses Vercel AI SDK's `UIMessage` format:

```typescript
interface UIMessage {
  id: string
  role: 'user' | 'assistant' | 'system'
  content: string
  parts: UIPart[]
  createdAt?: Date
}

type UIPart =
  | TextUIPart
  | ReasoningUIPart
  | ToolInvocationUIPart
  | FileUIPart
```

## ChatPanel Component

The main chat interface at `components/ide/chat-panel.tsx`:

### Props

```typescript
interface ChatPanelProps {
  session: Session | null
  workspaceId: string | null
  agentId: string | null
  onSessionCreated?: (sessionId: string) => void
}
```

### useChat Integration

```typescript
const { messages, input, handleSubmit, isLoading, setInput } = useChat({
  api: '/api/chat',
  body: {
    workspaceId,
    agentId,
    sessionId: session?.id,
  },
  onFinish: (message) => {
    // Handle completion
  },
  onError: (error) => {
    // Handle error
  },
})
```

### Features

1. **Streaming Responses**: Messages stream in real-time via SSE
2. **Tool Invocations**: Tool calls displayed with status (pending, running, complete)
3. **Reasoning Display**: Model reasoning/thinking shown in collapsible sections
4. **File Attachments**: Support for attaching files to messages
5. **Mode/Model Selection**: Dropdown to select agent mode and model

## AI Elements

### Conversation Component

Renders the message list with auto-scroll:

```tsx
<Conversation
  messages={messages}
  isLoading={isLoading}
  renderMessage={(message) => <Message message={message} />}
/>
```

Features:
- Auto-scroll to bottom on new messages
- Loading indicator during streaming
- Empty state when no messages

### Message Component

Renders individual messages with parts:

```tsx
<Message message={message} />
```

Handles different part types:
- **Text**: Rendered as markdown
- **Reasoning**: Collapsible thinking section
- **Tool Invocation**: Tool name, input, output display
- **Files**: Linked file references

### PromptInput Component

Chat input with features:

```tsx
<PromptInput
  value={input}
  onChange={setInput}
  onSubmit={handleSubmit}
  isLoading={isLoading}
  disabled={!session}
/>
```

Features:
- Multi-line input with shift+enter
- Submit on enter when the textarea contains non-whitespace text
- File attachment button
- Disabled state when loading
- Prompt history with pin functionality

### Empty Session Creation

The composer has two different empty-input behaviors for brand-new sessions:

- **Blank + Enter**: ignored
- **Blank + submit button click**: creates an empty session only when the button is acting as the `+` affordance for a new session

That create-only path:

1. Uses the already-generated `sessionId`
2. POSTs to `/api/chat` with `sessionId`, `workspaceId`, `agentId`, and `messages: []`
3. Lets the backend create the session without sending anything to the sandbox
4. Does **not** insert a synthetic user message into the local message list
5. Does **not** add anything to prompt history

Once a session already exists or is being resumed, empty submits are blocked for both Enter and button clicks.

## Prompt History

The chat interface includes a prompt history dropdown that allows users to access and reuse previous prompts.

### Files

| File | Description |
|------|-------------|
| `lib/hooks/use-prompt-history.ts` | Hook managing prompt history state and keyboard navigation |
| `components/ide/prompt-history-dropdown.tsx` | Dropdown UI component |

### Features

#### History Management

- **Auto-save**: Prompts are automatically saved to localStorage after submission
- **Deduplication**: Duplicate prompts are not added to history
- **Size Limit**: History is limited to 100 most recent prompts
- **Visibility**: Only the 20 most recent prompts are shown in the dropdown

#### Pin Functionality

Users can pin frequently-used prompts to keep them easily accessible:

- **Pin/Unpin**: Hover over any prompt to reveal a pin icon
- **Pinned Section**: Pinned prompts appear in a separate section at the bottom of the dropdown (closest to input)
- **Persistent**: Pinned prompts are saved to user preferences API and synced across devices
- **Visual Indicator**: Pinned icons are filled, unpinned icons are outlined
- **No Limit**: Unlike history (100 items), pinned prompts have no size limit

#### Keyboard Navigation

- **Open**: Press ↑ arrow when cursor is at start of textarea
- **Navigate**: Use ↑/↓ arrows to move through prompts
- **Selection Flow**: Pinned prompts → Recent prompts
- **Select**: Press Enter to use the highlighted prompt
- **Close**: Press ↓ at first pinned prompt or Escape

#### Layout

```
┌─────────────────────────────────┐
│ Prompt history     ↑/↓ navigate │  ← Header
├─────────────────────────────────┤
│ Recent                          │  ← Recent label (if pinned exist)
│ ┌─────────────────────────────┐ │
│ │ Recent prompt 3             │ │  ← Scrollable section
│ │ Recent prompt 2             │ │
│ │ Recent prompt 1        [📌] │ │
│ └─────────────────────────────┘ │
├─────────────────────────────────┤
│ Pinned                          │  ← Pinned label
│   Pinned prompt 1          [📌] │  ← Fixed section (no scroll)
│   Pinned prompt 2          [📌] │     Reversed order (newest bottom)
└─────────────────────────────────┘
```

### Usage

```typescript
const {
  history,
  pinnedPrompts,
  historyIndex,
  isPinnedSelection,
  isHistoryOpen,
  setHistoryIndex,
  onSelectHistory,
  addToHistory,
  pinPrompt,
  unpinPrompt,
  isPinned,
  closeHistory,
  handleKeyDown,
} = usePromptHistory({
  textareaRef,
  sessionId: selectedSessionId,
})

// Add prompt after submission
handleSubmit(message)
addToHistory(message.text)

// Attach keyboard handler to textarea
<textarea onKeyDown={handleKeyDown} ref={textareaRef} />

// Render dropdown
<PromptHistoryDropdown
  history={history}
  pinnedPrompts={pinnedPrompts}
  historyIndex={historyIndex}
  isPinnedSelection={isPinnedSelection}
  isHistoryOpen={isHistoryOpen}
  setHistoryIndex={setHistoryIndex}
  onSelectHistory={onSelectHistory}
  pinPrompt={pinPrompt}
  unpinPrompt={unpinPrompt}
  isPinned={isPinned}
  textareaRef={textareaRef}
  closeHistory={closeHistory}
/>
```

### Implementation Details

The `usePromptHistory` hook integrates with `usePreferences` to persist pinned prompts:

- **On Mount**: Loads pinned prompts from the `prompts.pinned` preference
- **On Pin**: Saves updated array to preferences API via `setPreference()`
- **On Unpin**: Removes prompt and saves to preferences API
- **Sync**: Automatically syncs when preferences change (e.g., from another tab/device)

Pinned prompts are stored as a JSON-stringified array of strings in the user preferences database.

### Storage

#### LocalStorage
- `discobot:prompt-history` - Recent prompt history (max 100 items)
- `discobot-prompt-draft-{sessionId}` - Per-session draft persistence

#### User Preferences API
- `prompts.pinned` - Pinned prompts (stored as JSON array, unlimited size, synced across devices)

## Session Creation

When a user sends a message without an active session:

1. `ChatPanel` detects no session
2. POST `/api/chat` includes `workspaceId` and `agentId`
3. Go backend creates session record
4. Go backend starts container
5. Response includes `sessionId` in metadata
6. `onSessionCreated` callback updates parent state

## Message History

The `useMessages` hook fetches message history for existing sessions:

```typescript
const { messages, isLoading, error } = useMessages(sessionId)
```

Implementation:
- Calls `GET /api/projects/{projectId}/sessions/{sessionId}/messages`
- Returns cached messages from SWR
- Container returns stored messages

## Stream Resumption

The chat implements automatic stream resumption for reconnection scenarios:

### How It Works

1. **useChat Configuration**: ChatPanel enables resume mode when a session is selected:
   ```typescript
   const { messages, sendMessage, status } = useChat({
     id: sessionId,
     resume: !!selectedSessionId,
   })
   ```

2. **Client Behavior**: When resume is needed, the chat UI reconnects to the thread-scoped stream endpoint at `/api/projects/{projectId}/sessions/{sessionId}/threads/{threadId}/stream`. Hook-status updates can also request a reconnect, but ChatPanel now defers that reconnect until the current stream is no longer in `submitted`/`streaming` state so active completions are not replayed mid-flight.

3. **Server Response**: The backend checks for active streams:
   - **Active stream**: Returns 200 and continues streaming from current position
   - **No active stream**: Returns 204 (No Content), client proceeds normally

4. **Message Loss Prevention**: The server includes a critical fix to prevent message loss during channel checks (see `server/docs/design/handler.md`)

### Component Lifecycle

To prevent unnecessary stream checks, ChatPanel is **never unmounted** during normal operation:
- Always rendered in `BottomPanel` (not conditionally in `MainContent`)
- Uses CSS visibility to toggle between chat and terminal views
- Preserves component state across session switches
- Only remounts when explicitly reset via `chatResetTrigger`

### Error Handling

The AI SDK includes a patch to handle edge cases:
- **Patch Location**: `patches/ai@6.0.50.patch`
- **Fix**: Adds null check before accessing `activeResponse.state` in finally block
- **Reason**: Prevents crashes when resume finds no stream (204 response)

## Styling

Chat uses design tokens for theming:

```css
/* Message bubbles */
.bg-muted { background: var(--muted); }
.text-muted-foreground { color: var(--muted-foreground); }

/* Tool invocations */
.bg-accent { background: var(--accent); }
.border-border { border-color: var(--border); }
```

## Error Handling

Errors are displayed inline:
- Network errors: Toast notification
- Streaming errors: Error message in chat
- Validation errors: Input field validation

## Integration Points

- **Go Backend**: Proxies chat to container via `/api/chat`
- **Container Agent**: Handles message routing to AI provider
- **useProjectEvents**: Receives session status updates
- **useSessions**: Updates session metadata on creation
