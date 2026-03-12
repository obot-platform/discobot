# Agent Architecture

This document describes the architecture of the `discobot-agent` init process.

## Overview

The `discobot-agent` binary serves as the container's PID 1 process, providing:

1. Home directory initialization (copy from template)
2. Workspace initialization (git clone)
3. AgentFS setup (copy-on-write filesystem)
4. HTTP proxy with Docker registry caching
5. CA certificate generation and system trust installation
6. Docker daemon startup (if available, with proxy configuration)
7. Process reaping for zombie collection
8. Privilege separation (root → discobot user)
9. Signal handling and forwarding
10. Graceful shutdown coordination

## Design Goals

### Minimal and Secure

- Statically compiled Go binary with no runtime dependencies
- Drops root privileges immediately after setup completes
- Uses `pdeathsig` to ensure child processes terminate with parent

### Container-Native

- Designed specifically for Docker/OCI container environments
- Handles PID 1 responsibilities that the Linux kernel delegates to init
- Works with both Docker runtime and VZ (Virtualization.framework) VMs

### Copy-on-Write Isolation

- Uses AgentFS for efficient storage with snapshot/restore capability
- Base layer is read-only (home directory + cloned repository)
- Agent sees a writable overlay that captures all changes
- Changes are stored efficiently in SQLite database

## Startup Sequence

```
┌─────────────────────────────────────────────────────────────┐
│                    Container Start                          │
│                    (running as root)                        │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  Step 1: Base Home Setup                                    │
│  ───────────────────────                                    │
│  • Check if /.data/discobot exists                           │
│  • If not, copy /home/discobot to /.data/discobot             │
│  • Preserve all permissions and ownership                   │
│  • Set ownership to discobot user                            │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  Step 2: Start Workspace Clone (Background)                 │
│  ───────────────────────────────────────────                │
│  • Start git clone in goroutine (slowest operation)         │
│  • Runs in parallel with filesystem/proxy/Docker setup      │
│  • Check if /.data/discobot/workspace exists                 │
│  • If not, clone WORKSPACE_ORIGIN_PATH to staging directory │
│  • Checkout WORKSPACE_COMMIT if specified                   │
│  • Change ownership to discobot user                         │
│  • Atomically rename staging → workspace                    │
│  • Agent waits for completion before starting API           │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  Step 3: AgentFS Directory                                  │
│  ─────────────────────────                                  │
│  • Create /.data/.agentfs directory                         │
│  • Set ownership to discobot user                            │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  Step 4: AgentFS Init                                       │
│  ───────────────────                                        │
│  • Check if /.data/.agentfs/{SESSION_ID}.db exists          │
│  • If not, run: agentfs init --base /.data/discobot {id}     │
│  • Creates SQLite database with base layer reference        │
│  • Runs as discobot user                                     │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  Step 5: AgentFS Mount                                      │
│  ──────────────────                                         │
│  • Run as discobot user:                                     │
│    agentfs mount -a --allow-root {id} /home/discobot         │
│  • -a: auto-unmount on exit                                 │
│  • --allow-root: allow root access to FUSE mount            │
│  • Mounts COW filesystem directly over /home/discobot        │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  Step 5.5: Mount Cache Directories                          │
│  ──────────────────────────────                             │
│  • Check if /.data/cache volume exists                      │
│  • Load cache config from user workspace (optional)         │
│    Location: /home/discobot/workspace/.discobot/cache.json   │
│  • Bind-mount cache dirs from /.data/cache to overlay       │
│  • Mounts sit on top of /home/discobot overlay               │
│  • Prevents cache writes to overlay layer                   │
│  See: docs/design/cache-config.md                           │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  Step 6: Create Workspace Symlink                           │
│  ───────────────────────────────                            │
│  • Create /workspace -> /home/discobot/workspace symlink     │
│  • Provides convenient access to project directory          │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  Step 7: Setup Proxy Configuration                          │
│  ────────────────────────────────                           │
│  • Use embedded default config only (security: no workspace │
│    config reading before sandbox is ready)                  │
│  • Write config to /.data/proxy/config.yaml (session)       │
│  • Proxy data (certs, cache) at /.data/cache/proxy (project)│
│  • Default enables Docker registry caching (20GB)           │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  Step 7: Generate CA Certificate & Install System Trust     │
│  ─────────────────────────────────────────────────────────  │
│  • Check if /.data/proxy/certs/ca.crt exists                │
│  • If not, generate CA cert using Go crypto/x509            │
│  • Install in /usr/local/share/ca-certificates/ (Debian)    │
│  • Or /etc/pki/ca-trust/source/anchors/ (RHEL)              │
│  • Run update-ca-certificates or update-ca-trust            │
│  • Enables transparent HTTPS MITM without warnings          │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  Step 8: Start Proxy Daemon                                 │
│  ───────────────────────                                    │
│  • Start proxy on port 17080 (HTTP/HTTPS/SOCKS5)            │
│  • API endpoint on port 17081                               │
│  • Wait for health check: http://localhost:17081/health     │
│  • Write settings to /etc/profile.d/discobot-proxy.sh        │
│  • Logs to stdout (visible in container logs)               │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  Step 9: Start Docker Daemon (Optional)                     │
│  ───────────────────────────────────────                    │
│  • Check if dockerd is on PATH                              │
│  • If found, start Docker daemon in background              │
│  • Configure data root at /.data/docker                     │
│  • Set proxy environment (HTTP_PROXY, HTTPS_PROXY, etc.)    │
│  • Wait for /var/run/docker.sock to become available        │
│  • Set socket permissions to 0666 (world-readable/writable) │
│  • Requires container to run in privileged mode             │
│  • Docker pulls now use proxy cache automatically           │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  Step 10: Wait for Workspace Clone Completion               │
│  ─────────────────────────────────────────────              │
│  • Block until background workspace clone finishes          │
│  • Ensures workspace is ready before agent-api starts       │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  Step 11: Run Agent API                                     │
│  ──────────────────────                                     │
│  • Fork child process                                       │
│  • Switch to discobot user (setuid/setgid)                   │
│  • Set HOME, USER, LOGNAME environment                      │
│  • Set proxy environment (HTTP_PROXY, NODE_EXTRA_CA_CERTS)  │
│  • Set working directory to /home/discobot/workspace         │
│  • Configure pdeathsig for cleanup                          │
│  • Enter event loop for signal handling                     │
└─────────────────────────────────────────────────────────────┘
```

## Component Design

```
┌─────────────────────────────────────────────────────────────┐
│                     discobot-agent (PID 1)                       │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐   │
│  │   Home       │  │   Workspace  │  │      AgentFS     │   │
│  │   Manager    │  │   Manager    │  │      Manager     │   │
│  └──────────────┘  └──────────────┘  └──────────────────┘   │
│                                                             │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────┐   │
│  │   Proxy      │  │   Docker     │  │     Signal       │   │
│  │   Manager    │  │   Manager    │  │     Handler      │   │
│  └──────────────┘  └──────────────┘  └──────────────────┘   │
│                                                             │
│  ┌──────────────┐  ┌──────────────────────────────────────┐ │
│  │   Process    │  │         Child Manager                │ │
│  │   Reaper     │  │                                      │ │
│  └──────────────┘  └──────────────────────────────────────┘ │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### Home Manager

Handles initial home directory setup:

- Copies /home/discobot template to /.data/discobot
- Preserves file permissions and ownership
- Recursive copy with symlink support
- Only runs on first container start

### Workspace Manager

Handles git operations for workspace initialization:

- Clones to staging directory first for atomicity
- Supports specific commit checkout
- Skips clone if workspace already exists
- Creates empty workspace if no WORKSPACE_ORIGIN_PATH

### AgentFS Manager

Integrates with the AgentFS copy-on-write filesystem:

- Initializes database with base layer reference
- Mounts FUSE filesystem directly over /home/discobot
- Uses `-a` flag for auto-unmount on exit
- Uses `--allow-root` for root access (docker exec)
- Provides efficient storage for session changes

### Proxy Manager

Manages the HTTP/HTTPS/SOCKS5 proxy with Docker registry caching:

- Uses embedded default configuration only (security: never reads untrusted workspace config)
- Generates CA certificate using Go crypto/x509 and installs in system trust store
- Starts proxy daemon on port 17080 (proxy) and 17081 (API)
- Waits for health check before proceeding
- Writes proxy environment variables to `/etc/profile.d/discobot-proxy.sh`
- Sets `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY`, and `NODE_EXTRA_CA_CERTS`
- Tracks proxy process for cleanup on shutdown
- Enables Docker registry caching (5-10x faster repeated pulls)
- **Cache location**: `/.data/cache/proxy/` (project-scoped, shared across sessions)
- **Config location**: `/.data/proxy/config.yaml` (session-scoped)
- **Performance**: Starts early in parallel with workspace cloning

See [design/proxy-integration.md](design/proxy-integration.md) for implementation details.

### Docker Manager

Optionally starts the Docker daemon inside the container:

- Checks if `dockerd` is available on PATH
- Starts Docker daemon with persistent storage at `/.data/docker`
- Waits for `/var/run/docker.sock` to become available
- Sets socket permissions to 0666 for all users
- Tracks dockerd process for cleanup on shutdown
- Requires container to run in privileged mode

This allows agents to run Docker commands (build, run, etc.) inside the sandbox without requiring a separate DinD sidecar container.

### Signal Handler

Handles incoming signals and forwards them appropriately:

| Signal | Action |
|--------|--------|
| SIGTERM | Forward to child, start shutdown timer |
| SIGINT | Forward to child, start shutdown timer |
| SIGQUIT | Forward to child, start shutdown timer |
| SIGHUP | Forward to child (config reload) |
| SIGCHLD | Trigger process reaping |

### Process Reaper

Collects zombie processes using `wait4()` with `WNOHANG`. This is essential for PID 1 because:

- Orphaned processes are re-parented to PID 1
- Only PID 1 can reap these orphans
- Without reaping, zombies accumulate and consume process table entries

### Child Manager

Responsible for:

1. **User Lookup**: Resolves username to UID/GID
2. **Environment Setup**: Sets HOME, USER, LOGNAME
3. **Process Creation**: Forks with credential switching
4. **Pdeathsig Setup**: Configures child to receive SIGTERM on parent death
5. **Exit Handling**: Captures and propagates child exit code

## Filesystem Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     What Agent Sees                          │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  /home/discobot  ─────► AgentFS FUSE mount (COW layer)       │
│  /workspace     ─────► symlink to /home/discobot/workspace   │
│                                                             │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│                  Actual Filesystem Layout                    │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  /.data/                                                    │
│  ├── discobot/                   (base layer, read-only)     │
│  │   ├── .bashrc                (shell config)              │
│  │   ├── .profile               (user profile)              │
│  │   └── workspace/             (cloned repository)         │
│  ├── .agentfs/                  (AgentFS databases)         │
│  │   └── {SESSION_ID}.db        (SQLite with changes)       │
│  ├── .overlayfs/                (OverlayFS layers)          │
│  │   └── {SESSION_ID}/          (upper + work dirs)         │
│  ├── proxy/                     (session-scoped)            │
│  │   └── config.yaml            (proxy configuration)       │
│  └── cache/                     (project-scoped volume)     │
│      └── proxy/                 (proxy cache & certs)       │
│          ├── certs/             (CA certificates)           │
│          └── cache/             (Docker registry cache)     │
│                                                             │
│  /home/discobot                  (AgentFS/OverlayFS mount)   │
│  ├── .config/discobot/           (agent-api persistence)     │
│  │   ├── agent-session.json     (session metadata)          │
│  │   └── agent-messages.json    (message history)           │
│  ├── .cache/                    (bind mount from cache vol) │
│  ├── .npm/                      (bind mount from cache vol) │
│  └── workspace/                 (COW of /.data/discobot/ws)  │
│                                                             │
│  /nix                            (writable Nix store root)   │
│                                                             │
│  /workspace -> /home/discobot/workspace (symlink)            │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

## Security Considerations

### Privilege Separation

The init process runs as root to:
- Copy home directory template
- Clone git repositories
- Create directories with correct ownership
- Perform mount operations
- Initialize AgentFS database

The child process runs as the `discobot` user with:
- No root access
- Standard user filesystem permissions
- Working directory set to /home/discobot/workspace

### FUSE Mount Access

The `--allow-root` flag on the AgentFS mount allows:
- Root to access the FUSE filesystem (needed for docker exec)
- The discobot user to read/write normally
- Requires `user_allow_other` in /etc/fuse.conf

### Pdeathsig

The `PR_SET_PDEATHSIG` option ensures the child receives SIGTERM if the parent dies unexpectedly. This prevents orphaned agent processes from continuing to run.

## Design Documents

- [Init Process Design](./design/init.md) - Detailed init process implementation
