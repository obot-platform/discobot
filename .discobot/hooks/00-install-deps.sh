#!/bin/bash
#---
# name: Install dependencies
# type: session
#---
# Install Node.js dependencies (needed for biome, tsc, and other tools)
pnpm install --frozen-lockfile 2>&1 || pnpm install 2>&1

# Download Go module dependencies
cd server && go mod download 2>&1 &
cd proxy && go mod download 2>&1 &
cd agent && go mod download 2>&1 &
wait
