#!/bin/bash
#---
# name: Tauri App
# description: Discobot desktop application (Tauri)
#---

set -e

# Ensure rustup has a default toolchain installed
if ! rustup show active-toolchain &>/dev/null; then
    echo "No active rustup toolchain found, installing stable..."
    rustup default stable
fi

pnpm dev:app
