# Discobot Agent - Container Init Process

The `discobot-agent` binary is a minimal PID 1 init process for container environments. It handles workspace initialization, AgentFS setup, and process management for Discobot containers.

## Features

- **Home Directory Setup**: Copies `/home/discobot` to persistent storage on first run
- **Workspace Cloning**: Clones git repositories to persistent storage with atomic staging
- **AgentFS Integration**: Initializes and mounts copy-on-write filesystem directly over `/home/discobot`
- **PID 1 Process Reaping**: Collects zombie processes to prevent resource leaks
- **User Switching**: Drops privileges from root to the `discobot` user
- **Signal Forwarding**: Forwards SIGTERM, SIGINT, SIGQUIT, and SIGHUP to child processes
- **Pdeathsig Support**: Ensures child processes die when the init process terminates
- **Graceful Shutdown**: 10-second timeout for clean shutdown before force-killing children

## Startup Sequence

```
1. Copy /home/discobot to /.data/discobot (if not exists)
2. Clone workspace to /.data/discobot/workspace (if WORKSPACE_ORIGIN_PATH set)
3. Initialize AgentFS database (if not exists)
4. Mount AgentFS over /home/discobot with -a --allow-root
5. Create /workspace symlink to /home/discobot/workspace
6. Run discobot-agent-api as discobot user
```

## Usage

The agent is typically invoked as the container's CMD:

```bash
# Container starts with required environment variables
docker run -e SESSION_ID=abc123 -e WORKSPACE_ORIGIN_PATH=https://github.com/user/repo discobot
```

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `SESSION_ID` | Yes | - | Unique session identifier for AgentFS database |
| `WORKSPACE_ORIGIN_PATH` | No | - | Git URL or local path to clone |
| `WORKSPACE_COMMIT` | No | - | Specific commit SHA to checkout |
| `AGENT_BINARY` | No | `/opt/discobot/bin/discobot-agent-api` | Path to the agent API binary |
| `AGENT_USER` | No | `discobot` | Username to run the agent API as |

## Filesystem Layout

### Persistent Storage (/.data volume)

```
/.data/
├── discobot/                     # Base home directory (copied from /home/discobot)
│   ├── .bashrc                  # User shell config
│   ├── .profile                 # User profile
│   └── workspace/               # Cloned repository
└── .agentfs/
    └── {SESSION_ID}.db          # AgentFS SQLite database
```

### System Paths

After setup, the filesystem is configured as:

| System Path | Source | Description |
|-------------|--------|-------------|
| `/home/discobot` | AgentFS mount | COW overlay of `/.data/discobot` |
| `/nix` | Image directory | Writable Nix store root owned by `discobot` |
| `/workspace` | Symlink | Points to `/home/discobot/workspace` |

The AgentFS mount provides copy-on-write semantics - reads come from the base layer (`/.data/discobot`), writes are captured in the SQLite database.

## Building

The agent is built as part of the Docker multi-stage build:

```bash
# Build just the agent binary
go build -o discobot-agent ./agent/cmd/agent

# Or via Docker (as part of full build)
docker build -t discobot .
```

## Architecture

```
Container Start (root)
        │
        ▼
┌───────────────────┐
│   discobot-agent      │  ← PID 1 (runs as root)
│   (init process)  │
│                   │
│   1. Copy home    │
│   2. Clone repo   │
│   3. Init AgentFS │
│   4. Mount AgentFS│
│   5. Create symlink│
└─────────┬─────────┘
          │
          │  fork + setuid(discobot)
          ▼
┌───────────────────┐
│ discobot-agent-api    │  ← Child process (runs as discobot)
│ (agent API)       │
│                   │
│ Sees:             │
│ /home/discobot(COW)│
│ /workspace (link) │
└───────────────────┘
```

### Signal Flow

```
SIGTERM/SIGINT → discobot-agent → forwards to child process group
                      │
                      └→ Waits up to 10s for graceful shutdown
                      └→ Force-kills child if timeout exceeded
```

### Process Reaping

As PID 1, `discobot-agent` is responsible for calling `wait()` on orphaned processes. This prevents zombie process accumulation when child processes fork and their parents exit.

## AgentFS Mount Flags

The AgentFS mount uses special flags:

- `-a`: Auto-unmount when the process exits
- `--allow-root`: Allow root to access the FUSE mount (required for `docker exec` as root)

## Documentation

- [Architecture](./docs/ARCHITECTURE.md) - Technical architecture overview
- [Init Design](./docs/design/init.md) - Detailed design of the init process

## Related Components

- [Agent API](../agent-api/README.md) - The TypeScript/Bun API service that runs as the child process
- [Proxy](../proxy/README.md) - HTTP/SOCKS5 proxy for credential injection
