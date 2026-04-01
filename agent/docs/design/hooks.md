# Session Hooks (Agent Init)

Session hooks are scripts in `.discobot/hooks/` with `type: session` that run once during container startup. They are executed by the Go agent init process (PID 1) before the agent-api starts.

## Execution Point

Session hooks run after the workspace and filesystem are fully set up (step 5 — workspace symlink created) but before the agent-api process is forked (step 9). This means:

- The workspace is cloned and available at `/home/discobot/workspace`
- The overlay filesystem is mounted
- Cache directories are mounted
- The proxy may or may not be started yet (hooks run before proxy/Docker startup)

## Implementation

### Hook Discovery

The Go agent scans `/home/discobot/workspace/.discobot/hooks/` for files that:
1. Are regular files (not directories, not hidden)
2. Have the executable bit set
3. Have a shebang line (`#!`)
4. Have front matter with `type: session`

### Front Matter Parsing

Minimal Go implementation of the same YAML front matter parser used by the TypeScript services module. Supports `#---` delimiters with `key: value` pairs:

```go
type HookConfig struct {
    Name        string // Display name
    Type        string // "session", "file", "pre-commit"
    Description string // Human-readable description
    RunAs       string // "root" or "user" (default: "user")
}
```

### Execution

Hooks are sorted alphabetically by filename and executed sequentially:

```go
func runSessionHooks(workspaceDir string, userInfo *userInfo) error
```

For each hook:
- If `run_as: root` → execute as root (no credential switching)
- If `run_as: user` (default) → execute as discobot user via `syscall.Credential`
- Working directory: `/home/discobot/workspace`
- Timeout: 5 minutes per hook
- stdout/stderr captured and logged
- **On failure: log error and continue** (don't block session startup)

### Environment Variables

Hooks receive the agent's environment plus:

| Variable | Description |
|----------|-------------|
| `DISCOBOT_SESSION_ID` | Current session ID |
| `DISCOBOT_WORKSPACE` | Workspace path (`/home/discobot/workspace`) |
| `DISCOBOT_HOOK_TYPE` | Always `session` |

### Error Handling

Session hook failures are logged but do not prevent the agent-api from starting. This ensures that a broken hook doesn't make the session permanently unusable.

Runtime hook state is persisted under `~/.discobot/threads/{sessionId}/hooks/`, including `status.json` and per-hook output logs in the `output/` subdirectory.

## Example

```bash
#!/bin/bash
#---
# name: Install system deps
# type: session
# run_as: root
#---
apt-get update && apt-get install -y postgresql-client
```

```bash
#!/bin/bash
#---
# name: Setup dev environment
# type: session
#---
pnpm install
cp .env.example .env
```
