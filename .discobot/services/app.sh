#!/bin/bash
#---
# name: Tauri App
# description: Discobot desktop application (Tauri)
#---

set -e

cleanup_stale_apt_locks() {
    local lock_files=(
        "/var/lib/dpkg/lock"
        "/var/lib/dpkg/lock-frontend"
        "/var/cache/apt/archives/lock"
    )
    local removed_lock=false

    if pgrep -x apt >/dev/null 2>&1 || \
       pgrep -x apt-get >/dev/null 2>&1 || \
       pgrep -x apt-cache >/dev/null 2>&1 || \
       pgrep -x dpkg >/dev/null 2>&1 || \
       pgrep -x unattended-upgrade >/dev/null 2>&1; then
        return
    fi

    for lock_file in "${lock_files[@]}"; do
        if [ -e "$lock_file" ] && [ -w "$lock_file" ]; then
            rm -f "$lock_file"
            removed_lock=true
        fi
    done

    if [ "$removed_lock" = true ] && command -v dpkg >/dev/null 2>&1; then
        if ! dpkg --configure -a; then
            printf '%s\n' "Warning: dpkg --configure -a failed after clearing stale apt locks"
        fi
    fi
}

# Ensure rustup has a default toolchain installed
if ! rustup show active-toolchain &>/dev/null; then
    echo "No active rustup toolchain found, installing stable..."
    rustup default stable
fi

cleanup_stale_apt_locks

pnpm dev:app

