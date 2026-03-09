#!/bin/bash
#---
# name: Discobot Svelte UI
# description: SvelteKit UI development server with Go backend
# http: 3100
# path: /
#---

set +x

SQL_DUMP="${WORKSPACE_ORIGIN_PATH:-.}/test.db.sql"
DB="/home/discobot/.local/share/discobot/discobot.db"
if [ ! -e "$DB" ] && [ -e "$SQL_DUMP" ]; then
    mkdir -p "$(dirname "$DB")"
    sqlite3 "$DB" < "$SQL_DUMP"
fi

ENV_FILE="./server/.env"
if [ ! -e "$ENV_FILE" ]; then
    touch "$ENV_FILE"
fi
if ! grep -q "^SANDBOX_IMAGE=" "$ENV_FILE" 2>/dev/null; then
    printf '%s\n' "SANDBOX_IMAGE=ghcr.io/obot-platform/discobot:nonexistent" >> "$ENV_FILE"
fi

pnpm install && exec pnpm ui:dev:backend
