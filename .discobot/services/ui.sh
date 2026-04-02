#!/bin/bash
#---
# name: Discobot UI
# description: Vite + React Router UI development server
# order: 1
# http: 3100
#---

set +x

SQL_DUMP="${WORKSPACE_ORIGIN_PATH}/test.db.sql"
DB="/home/discobot/.local/share/discobot/discobot.db"
if [ ! -e $DB ] && [ -e "${SQL_DUMP}" ]; then
    mkdir -p "$(dirname $DB)"
    sqlite3 $DB < "${SQL_DUMP}"
fi
ENV_FILE="./server/.env"
if ! grep -q "^SANDBOX_IMAGE=" "$ENV_FILE" 2>/dev/null; then
    echo "SANDBOX_IMAGE=ghcr.io/obot-platform/discobot:nonexistent" >> "$ENV_FILE"
fi
pnpm install && pnpm ui:dev:backend
