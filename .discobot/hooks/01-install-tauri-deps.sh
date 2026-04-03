#!/bin/bash
#---
# name: Install Tauri system dependencies
# type: session
# run_as: root
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

cleanup_stale_apt_locks

apt-get update -qq

apt-get install -y --no-install-recommends \
    build-essential \
    pkg-config \
    file \
    libssl-dev \
    libgtk-3-dev \
    libwebkit2gtk-4.1-dev \
    libayatana-appindicator3-dev \
    librsvg2-dev \
    patchelf \
    libglib2.0-dev \
    libsoup-3.0-dev \
    libjavascriptcoregtk-4.1-dev
