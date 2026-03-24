#!/bin/bash
# Wrapper script for air: writes .zed/debug.json, then execs the server binary so air signals the real process
set -e

BINARY="./build/discobot"
DEBUG_JSON="../.zed/debug.json"
PID=$$

mkdir -p "$(dirname "$DEBUG_JSON")"
cat > "$DEBUG_JSON" <<EOF
[
    {
        "adapter": "Delve",
        "label": "Attach to Discobot Server (Delve)",
        "request": "attach",
        "mode": "local",
        "processId": $PID,
        "cwd": "\${ZED_WORKTREE_ROOT}/server",
        "stopOnEntry": false
    }
]
EOF

exec "$BINARY" "$@"
