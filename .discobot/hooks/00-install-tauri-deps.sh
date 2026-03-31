#!/bin/bash
#---
# name: Install Tauri system dependencies
# type: session
# run_as: root
#---
set -e

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
