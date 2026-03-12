# Session Display Name

## Overview

Sessions have two name fields to support both automatic naming and user customization:
- **`name`**: The original, automatically-derived name from the first message (preserved forever)
- **`displayName`**: An optional user-provided custom name (can be set, updated, or cleared)

This design follows the same pattern as workspace naming (`path` vs `displayName`).

## Motivation

When a session is created, its `name` is automatically derived from the first user message (truncated to 50 characters). This provides a meaningful default, but users may want to:
1. Shorten the name to something more concise (e.g., "Fix authentication bug" → "Auth Fix")
2. Clarify the purpose after the session evolves
3. Organize sessions with consistent naming conventions

The challenge is: if we allow renaming the `name` field directly, users lose the original context from the first message. By adding a separate `displayName` field, we:
- **Preserve** the original name (important historical context)
- **Allow** users to customize what they see in the UI
- **Enable** reverting to the original name by clearing `displayName`

## Data Model

### Database Schema (Go)

```go
type Session struct {
    ID          string    `gorm:"primaryKey;type:text"`
    ProjectID   string    `gorm:"column:project_id;not null;type:text;index"`
    WorkspaceID string    `gorm:"column:workspace_id;not null;type:text;index"`
    AgentID     *string   `gorm:"column:agent_id;type:text;index"`
    Name        string    `gorm:"not null;type:text"`
    DisplayName *string   `gorm:"column:display_name;type:text"`  // NEW
    Description *string   `gorm:"type:text"`
    Status      string    `gorm:"not null;type:text;default:initializing"`
    // ... other fields
}
```

### TypeScript Interface

```typescript
export interface Session {
  id: string;
  name: string;              // Original name (always present)
  displayName?: string;      // Optional custom name
  description: string;
  // ... other fields
}

export interface UpdateSessionRequest {
  name?: string;
  displayName?: string | null;  // null = clear display name
  status?: SessionStatus;
}
```

## API Behavior

### Creating a Session

Sessions are created implicitly via the `/api/projects/{projectId}/chat` endpoint. If the request includes no prompt (`messages: []`, `null`, or omitted), the session is still created but its derived `name` remains empty until the first real user message:

```json
POST /api/projects/{projectId}/chat
{
  "id": "session-123",
  "messages": [
    {
      "role": "user",
      "parts": [{"type": "text", "text": "Help me fix the authentication bug in my app"}]
    }
  ],
  "workspaceId": "ws-1",
  "agentId": "agent-1"
}
```

**Result:**
- `name`: `"Help me fix the authentication bug in my app"` (derived from message)
- `displayName`: `null` (not set)

### Setting a Display Name

```json
PATCH /api/projects/{projectId}/sessions/session-123
{
  "displayName": "Auth Bug Fix"
}
```

**Result:**
- `name`: `"Help me fix the authentication bug in my app"` (unchanged)
- `displayName`: `"Auth Bug Fix"`

### Clearing a Display Name

```json
PATCH /api/projects/{projectId}/sessions/session-123
{
  "displayName": null
}
```

**Result:**
- `name`: `"Help me fix the authentication bug in my app"` (unchanged)
- `displayName`: `null`

Empty string is also treated as clearing:

```json
PATCH /api/projects/{projectId}/sessions/session-123
{
  "displayName": ""
}
```

## Frontend Implementation

### Centralized Helper (`session-name.tsx`)

Following the pattern of `workspace-path.tsx`, we created a centralized helper:

```typescript
// Get the display name to show in UI
export function getSessionDisplayName(session: Session): string {
  return session.displayName || session.name;
}

// Component with icon and tooltip support
export function SessionName({
  session,
  showIcon = false,
  iconClassName,
  className,
  textClassName
}: SessionNameProps): JSX.Element
```

### Usage Across Components

The helper is used in:
1. **Sidebar Tree** (`sidebar-tree.tsx`) - Session list with inline rename
2. **Session Dropdown** (`session-dropdown-item.tsx`) - Prompt history dropdown

### Inline Rename UI

The sidebar tree allows inline renaming (like workspace rename):

```typescript
const SessionNode = () => {
  const [isRenaming, setIsRenaming] = useState(false);
  const [editedName, setEditedName] = useState("");

  const startRename = () => {
    setEditedName(session.displayName || "");
    setIsRenaming(true);
  };

  const saveRename = async () => {
    await updateSession({
      displayName: editedName.trim() === "" ? null : editedName.trim()
    });
  };

  // Enter to save, Escape to cancel
  // Blur to save
  // Shows original name as placeholder
};
```

### Display Logic

| Scenario | Display | Tooltip |
|----------|---------|---------|
| No custom name | `name` | None |
| Custom name set | `displayName` | Shows both `displayName` and `name` |
| Custom name cleared | `name` | None |

## Testing

### Unit Tests

**Frontend** (`components/ide/session-name.test.tsx`):
- `getSessionDisplayName()` returns correct name
- `SessionName` component renders correctly
- Props (className, showIcon, etc.) work as expected

### Integration Tests

**Backend** (`server/internal/integration/session_displayname_test.go`):
- Setting displayName preserves original name
- Clearing displayName works (null and empty string)
- displayName persists across API calls
- displayName appears in session lists

### Test Coverage

```bash
# Frontend tests
node --import ./test/setup.js --import tsx --test components/ide/session-name.test.tsx

# Backend tests
go test ./internal/integration -run TestSessionDisplayName
```

## Database Migration

The schema change is handled automatically by GORM's `AutoMigrate()`:
- On first startup after upgrade, the `display_name` column is added to the `sessions` table
- Existing sessions have `display_name = NULL`
- No data migration needed - existing behavior is preserved

## Comparison with Workspace Naming

| Aspect | Workspace | Session |
|--------|-----------|---------|
| **Original field** | `path` (full path) | `name` (first message) |
| **Custom field** | `displayName` | `displayName` |
| **Source** | User-provided | Auto-derived |
| **Default display** | Shortened path | Full name |
| **Tooltip** | Full path (if shortened) | Both names (if custom) |

## Edge Cases

### Long Names
- Original names are already truncated to 50 chars on creation
- displayName has no enforced limit (database allows any length)
- UI handles long names with CSS truncation + tooltips

### Special Characters
- Names can contain any UTF-8 characters
- Frontend properly escapes for display
- No sanitization needed (not used in URLs)

### Empty Sessions
- If message parsing fails, name defaults to `"New Session"`
- displayName works the same regardless of how name was derived

## Future Considerations

### Potential Enhancements
1. **Search/Filter**: Index displayName for faster searching
2. **History**: Track displayName changes (audit log)
3. **Auto-suggest**: Suggest displayNames based on session content
4. **Validation**: Enforce displayName length limits (optional)

### Not Planned
- Renaming the `name` field directly (use displayName instead)
- Bulk rename operations (users can script via API if needed)
- Default displayName patterns (just use the auto-derived name)

## References

- Implementation: `server/internal/model/model.go`, `lib/api-types.ts`
- API Docs: `server/api.md` (Sessions section)
- UI Component: `components/ide/session-name.tsx`
- Similar Pattern: `components/ide/workspace-path.tsx`
