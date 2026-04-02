# Workspace Customization

Discobot supports per-workspace customization through the `.discobot/` directory at the root of your workspace. Two types of customizations are available:

- **Hooks** (`.discobot/hooks/`) — Automation scripts that run at specific lifecycle points
- **Services** (`.discobot/services/`) — Background processes and HTTP endpoints

Both use the same file format: executable scripts with YAML front matter.

```
.discobot/
├── hooks/
│   ├── install-deps.sh
│   ├── lint.sh
│   └── typecheck.sh
└── services/
    ├── ui.sh
    ├── api-server.sh
    └── database
```

## Front Matter Format

All `.discobot/` files use YAML front matter to declare configuration. Three delimiter styles are supported:

**Hash-prefixed (shell scripts):**
```bash
#!/bin/bash
#---
# name: My Script
# type: session
#---
echo "hello"
```

**Slash-prefixed (JavaScript/TypeScript):**
```javascript
#!/usr/bin/env node
//---
// name: Node Script
// http: 3000
//---
require('./server').start();
```

**Plain delimiters:**
```yaml
---
name: My Config
http: 8080
---
```

For comment-prefixed styles (`#---`, `//---`), all whitespace after the prefix is trimmed, so `#   name: Foo` and `# name: Foo` are equivalent.

---

## Hooks

Hooks are executable scripts in `.discobot/hooks/` that run at specific points during a session. They enable automated setup, validation, and enforcement of code quality standards.

### Hook Types

There are three hook types, set via the `type` field in front matter:

| Type | When it runs | On failure |
|------|-------------|------------|
| `session` | Once at container startup | Logged, does not block startup |
| `file` | After each LLM turn, when matching files changed | LLM is re-prompted to fix the issue |
| `pre-commit` | On `git commit` (installed as a git hook) | Commit is blocked |

### Common Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | No | Display name (defaults to filename) |
| `type` | string | **Yes** | `session`, `file`, or `pre-commit` |
| `description` | string | No | Human-readable description |

### File Requirements

Hook files must:

- Be in the `.discobot/hooks/` directory
- Be executable (`chmod +x`)
- Have a shebang line (`#!/bin/bash`, `#!/usr/bin/env python`, etc.)
- Have front matter with a valid `type` field

Files are sorted alphabetically by filename, so prefix with numbers to control execution order (e.g., `01-install.sh`, `02-lint.sh`).

### Session Hooks

Session hooks run once when the container starts, before the AI agent begins. They're ideal for installing dependencies, setting up the environment, or configuring tools.

**Additional fields:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `run_as` | `root` or `user` | `user` | Execute as root or as the discobot user |

**Behavior:**

- Run sequentially in alphabetical order
- 5-minute timeout per hook
- Working directory is `/home/discobot/workspace`
- Failures are logged but do not block the session from starting

**Environment variables:**

| Variable | Description |
|----------|-------------|
| `DISCOBOT_SESSION_ID` | Current session ID |
| `DISCOBOT_WORKSPACE` | Workspace path (`/home/discobot/workspace`) |
| `DISCOBOT_HOOK_TYPE` | `session` |

**Example — Install system packages (as root):**

```bash
#!/bin/bash
#---
# name: Install system deps
# type: session
# run_as: root
#---
apt-get update && apt-get install -y postgresql-client redis-tools
```

**Example — Install project dependencies (as user):**

```bash
#!/bin/bash
#---
# name: Install dependencies
# type: session
#---
pnpm install --frozen-lockfile 2>&1 || pnpm install 2>&1
```

### File Hooks

File hooks run after each LLM turn completes, checking whether files matching a glob pattern have changed. If a file hook fails, the LLM is automatically re-prompted with the failure output so it can fix the issue.

**Additional fields:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `pattern` | string | **Required** | Glob pattern for file matching (e.g., `"*.go"`, `"src/**/*.ts"`) |
| `notify_llm` | boolean | `true` | Whether to re-prompt the LLM on failure |

**Behavior:**

- Run after the LLM completion finishes and the SSE stream closes
- Only triggered when files matching the `pattern` have changed since the last evaluation
- On failure with `notify_llm: true`: the LLM receives the hook output and attempts to fix the issue (up to 3 retries per user message)
- On failure with `notify_llm: false`: the hook runs silently — useful for auto-fixers like formatters
- Hooks that fail block subsequent hooks from running until fixed
- If agent-go restarts while a file hook is running, Discobot resets that hook to pending and re-runs it on the next eligible evaluation

**Environment variables:**

| Variable | Description |
|----------|-------------|
| `DISCOBOT_CHANGED_FILES` | Space-separated list of changed file paths (relative to workspace) |
| `DISCOBOT_SESSION_ID` | Current session ID |
| `DISCOBOT_HOOK_TYPE` | `file` |

**Glob pattern syntax** uses [picomatch](https://github.com/micromatch/picomatch) patterns:

| Pattern | Matches |
|---------|---------|
| `"*.go"` | All `.go` files in any directory |
| `"src/**/*.ts"` | All `.ts` files under `src/` |
| `"*.{ts,tsx}"` | All `.ts` and `.tsx` files |
| `"**/*.go"` | All `.go` files recursively |
| `"{package.json,pnpm*.yaml}"` | `package.json` and any `pnpm*.yaml` files |

**Example — Lint Go files:**

```bash
#!/bin/bash
#---
# name: Go format check
# type: file
# pattern: "*.go"
#---
gofmt -l $DISCOBOT_CHANGED_FILES
```

**Example — Auto-fix TypeScript (silent):**

```bash
#!/bin/bash
#---
# name: ESLint autofix
# type: file
# pattern: "*.{ts,tsx}"
# notify_llm: false
#---
npx eslint --fix $DISCOBOT_CHANGED_FILES
```

**Example — Reinstall dependencies when lockfile changes:**

```bash
#!/bin/bash
#---
# name: Install dependencies
# type: file
# pattern: "{package.json,pnpm*.yaml}"
#---
pnpm install --frozen-lockfile 2>&1 || pnpm install 2>&1
```

**Example — Run `go mod tidy` when go.mod changes:**

```bash
#!/bin/bash
#---
# name: Go mod tidy
# type: file
# pattern: "**/go.mod"
#---
for f in $DISCOBOT_CHANGED_FILES; do
	dir=$(dirname "$f")
	(cd "$dir" && go mod tidy 2>&1)
done
```

### Pre-commit Hooks

Pre-commit hooks are installed as git pre-commit hooks. They run automatically when `git commit` is executed and block the commit if they fail.

**Additional fields:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `notify_llm` | boolean | `true` | Reserved for future use |

**Behavior:**

- On session startup, Discobot generates a `.git/hooks/pre-commit` script that chains all `type: pre-commit` hooks
- If a `.git/hooks/pre-commit` already exists and wasn't created by Discobot, it's preserved as `.git/hooks/pre-commit.original` and still runs
- When the LLM runs `git commit` and the hook fails, it sees the error output and can fix the issue and retry

**Example — Run CI checks before commit:**

```bash
#!/bin/bash
#---
# name: CI
# type: pre-commit
#---
pnpm run ci
```

**Example — Type check before commit:**

```bash
#!/bin/bash
#---
# name: Type check
# type: pre-commit
#---
pnpm typecheck
```

### How File Hooks Interact with the LLM

When a file hook fails with `notify_llm: true`, the LLM receives a message like:

```
[Discobot Hook Failed] "Go format check" (pattern: *.go)

Files: internal/server/handler.go, internal/server/service.go
Exit code: 1

Output:
internal/server/handler.go: formatting differs from gofmt
internal/server/service.go: formatting differs from gofmt

Please fix the issues and ensure the hook passes. (Attempt 1/3)
```

For large output (over 200 lines), the output is saved to a file and the LLM is told to read it:

```
[Discobot Hook Failed] "Go format check" (pattern: *.go)

Files: internal/server/handler.go, internal/server/service.go
Exit code: 1

Output is large (847 lines). Full output saved to:
  ~/.discobot/threads/abc123/hooks/output/go-format-check.log

Please read the file to see the full output and address the issues. (Attempt 1/3)
```

After 3 consecutive hook-triggered retries, Discobot stops re-prompting. The pending hooks will re-evaluate on the next user-initiated message.

### Hook Reloading

Hooks are automatically reloaded when files in `.discobot/hooks/` change. Adding, removing, or editing hook files takes effect on the next evaluation cycle without restarting the session.

---

## Services

Services are background processes defined in `.discobot/services/`. They allow you to run development servers, databases, or any long-running process alongside your AI agent session, with built-in HTTP proxying and output streaming.

### Service Types

There are two types of services:

| Type | Description | Requirements |
|------|-------------|--------------|
| **Executable** | Scripts started/stopped by Discobot | Executable file with shebang and script body |
| **Passive** | Declarations for externally-managed services | Front matter only, no script body |

### Configuration Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | No | Display name (defaults to filename) |
| `description` | string | No | Human-readable description |
| `order` | number | No | Optional UI sort order for service buttons and panels (lower numbers appear first) |
| `http` | number | No | HTTP port the service listens on |
| `https` | number | No | HTTPS port the service listens on |
| `path` | string | No | Default URL path for web preview (e.g., `"/app"`, `"/api/docs"`) |

### Service ID

The service ID is derived from the filename:

- Common script extensions are stripped (`.sh`, `.bash`, `.zsh`, `.py`, `.js`, `.ts`, `.rb`, `.pl`, `.php`)
- Remaining dots become hyphens
- Converted to lowercase
- Non-alphanumeric characters (except `-` and `_`) are removed

For example: `ui.sh` becomes `ui`, `api-server.sh` becomes `api-server`.

### Service Ordering

You can control the order of services in the Discobot UI with the optional `order` field.

- Lower numbers appear first
- Services with the same `order` fall back to alphabetical ordering by name
- Services without an `order` appear after all ordered services, also alphabetically

This applies to the services shown in the active UI, including the service panel buttons.

### Executable Services

Executable services are scripts that Discobot starts and stops as child processes. They must be executable files with a shebang line and a script body.

**Example — Vite development server:**

```bash
#!/bin/bash
#---
# name: Discobot UI
# description: Vite + React Router UI development server
# order: 10
# http: 3000
#---
pnpm install && pnpm dev
```

**Example — SQLite browser:**

```bash
#!/bin/bash
#---
# name: SQLite GUI
# description: sqlite-web browser for the Discobot database
# http: 8080
#---
DB="${HOME}/.local/share/discobot/discobot.db"

if [ ! -f "$DB" ]; then
    echo "Database not found at: $DB"
    exit 1
fi

exec uvx --from sqlite-web sqlite_web "$DB" --port 8080 --host 0.0.0.0 --no-browser --read-only
```

**Lifecycle:**

```
stopped → starting → running → stopping → stopped
                        ↓
                      error → stopped
```

- Started via the UI or API (`POST /services/:id/start`)
- Stopped with SIGTERM, then SIGKILL after 5 seconds if still running
- Output (stdout/stderr) is captured and available via SSE streaming
- Output files are stored at `~/.config/discobot/services/output/{id}.out` (max 1MB, auto-truncated)

### Passive Services

Passive services declare an HTTP endpoint without providing a script to run. They're useful when a service is managed externally (e.g., by `devcontainer.json` or `docker-compose`).

**Example — Externally-managed web app:**

```yaml
---
name: Web App
description: Frontend dev server (started by devcontainer)
order: 10
http: 3000
---
```

Passive service files:

- Must have front matter with `http` or `https` defined
- Must have an empty body (no script content after the closing delimiter)
- Do **not** need a shebang line or execute permissions

Passive services always show as `status: "stopped"` in the API. Start/stop/output calls return `400`. The HTTP proxy still works if the declared port is accessible.

### HTTP Proxy

Services with an `http` or `https` port get automatic HTTP proxying. The proxy is accessible through the Discobot UI as a web preview, or via subdomain-based URLs:

```
{session-id}-svc-{service-id}.{base-domain}
```

For example: `01HXYZ123456789ABCDEF-svc-ui.localhost:3000`

The proxy:

- Supports all HTTP methods and WebSocket connections
- Auto-starts non-passive executable services on first request
- Returns an auto-refreshing page if the service isn't ready yet
- Does not forward authentication credentials (services are considered public within the sandbox)

The `path` field in front matter sets the default URL path used by the web preview in the UI.

---

## Complete Example

Here's a full `.discobot/` configuration for a Go + React project:

```
.discobot/
├── hooks/
│   ├── 01-install-deps.sh      # Session: install all dependencies
│   ├── 02-go-mod-tidy.sh       # File: tidy go.mod when it changes
│   ├── 03-lint-frontend.sh     # File: lint TypeScript on change
│   ├── 04-lint-backend.sh      # File: lint Go on change
│   ├── 05-build.sh             # File: verify build on change
│   └── 06-ci.sh                # Pre-commit: full CI before commit
└── services/
    ├── ui.sh                   # Dev server on port 3000
    └── db.sh                   # SQLite browser on port 8080
```

**`.discobot/hooks/01-install-deps.sh`:**
```bash
#!/bin/bash
#---
# name: Install dependencies
# type: session
#---
pnpm install --frozen-lockfile 2>&1 || pnpm install 2>&1
cd server && go mod download 2>&1
```

**`.discobot/hooks/02-go-mod-tidy.sh`:**
```bash
#!/bin/bash
#---
# name: Go mod tidy
# type: file
# pattern: "**/go.mod"
#---
for f in $DISCOBOT_CHANGED_FILES; do
	dir=$(dirname "$f")
	(cd "$dir" && go mod tidy 2>&1)
done
```

**`.discobot/hooks/03-lint-frontend.sh`:**
```bash
#!/bin/bash
#---
# name: Frontend lint
# type: file
# pattern: "**/*.{ts,tsx,js,jsx,json}"
#---
pnpm check:frontend:fix
```

**`.discobot/hooks/04-lint-backend.sh`:**
```bash
#!/bin/bash
#---
# name: Backend lint
# type: file
# pattern: "**/*.go"
#---
pnpm check:backend:fix
```

**`.discobot/hooks/05-build.sh`:**
```bash
#!/bin/bash
#---
# name: Build
# type: file
# pattern: "**/*.{ts,tsx,js,jsx,go}"
#---
pnpm build
```

**`.discobot/hooks/06-ci.sh`:**
```bash
#!/bin/bash
#---
# name: CI
# type: pre-commit
#---
pnpm run ci
```

**`.discobot/services/ui.sh`:**
```bash
#!/bin/bash
#---
# name: Dev UI
# description: Vite development server
# order: 10
# http: 3000
#---
pnpm install && pnpm dev
```

**`.discobot/services/db.sh`:**
```bash
#!/bin/bash
#---
# name: SQLite GUI
# description: Database browser
# order: 20
# http: 8080
#---
exec uvx --from sqlite-web sqlite_web "$HOME/.local/share/app/app.db" \
    --port 8080 --host 0.0.0.0 --no-browser --read-only
```
