#!/bin/bash
#---
# name: Electron App
# description: Discobot desktop application (Electron)
# order: 4
#---

set -euo pipefail

if [ ! -d node_modules ] || [ ! -x node_modules/.bin/electron ]; then
    pnpm install
fi

exec pnpm dev:app:electron
