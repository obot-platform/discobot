# syntax=docker/dockerfile:1.7

ARG UBUNTU_MIRROR=
ARG UBUNTU_PORTS_MIRROR=

# Stage 0: Download shared Go module dependencies for root-module binaries
FROM golang:1.26 AS root-go-deps

WORKDIR /build

# Copy module files first for better caching
# modelsdev/go.mod is needed by the replace directive in the root go.mod
COPY modelsdev/go.mod ./modelsdev/
COPY go.mod go.sum ./

# Download dependencies
RUN --mount=type=cache,id=discobot-gomodcache,target=/go/pkg/mod \
    --mount=type=cache,id=discobot-gobuildcache,target=/root/.cache/go-build \
    go mod download

# Stage 1: Build the proxy binary from source
FROM root-go-deps AS proxy-builder

# Copy proxy source
COPY proxy/ ./proxy/

# Build the proxy binary
RUN --mount=type=cache,id=discobot-gomodcache,target=/go/pkg/mod \
    --mount=type=cache,id=discobot-gobuildcache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /proxy ./proxy/cmd/proxy

# Stage 1b: Build the agent init process from source
FROM root-go-deps AS agent-builder

# Copy agent source (including embedded proxy config)
COPY agent/ ./agent/

# Build the agent binary (static for portability)
# The go:embed directive will include agent/internal/proxy/default-config.yaml
RUN --mount=type=cache,id=discobot-gomodcache,target=/go/pkg/mod \
    --mount=type=cache,id=discobot-gobuildcache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o /discobot-agent ./agent/cmd/agent

# Stage 2: Download shared Go module dependencies for agent-go
FROM golang:1.26 AS agent-go-deps

WORKDIR /build

# Copy modelsdev module files first — needed by the replace directive in agent-go/go.mod
# (replace ../modelsdev resolves to /modelsdev relative to WORKDIR /build)
COPY modelsdev/go.mod /modelsdev/

# Copy module files first for better layer caching
COPY agent-go/go.mod agent-go/go.sum ./

# Download dependencies
RUN --mount=type=cache,id=discobot-gomodcache,target=/go/pkg/mod \
    --mount=type=cache,id=discobot-gobuildcache,target=/root/.cache/go-build \
    go mod download

# Stage 2b: Build agent-go as the discobot-agent-api binary
FROM agent-go-deps AS agent-go-builder

# Copy modelsdev source (required for compilation, not just module resolution)
COPY modelsdev/ /modelsdev/

# Copy agent-go source
COPY agent-go/ ./

# Build the agent-go binary as discobot-agent-api and the sudo policy gate.
# Use mcp_go_client_oauth build tag to enable OAuth support for MCP tools
RUN --mount=type=cache,id=discobot-gomodcache,target=/go/pkg/mod \
    --mount=type=cache,id=discobot-gobuildcache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -tags mcp_go_client_oauth -ldflags="-s -w" -o /discobot-agent-api ./cmd/agent-api \
    && CGO_ENABLED=0 go build -ldflags="-s -w" -o /discobot-sudo-gate ./cmd/sudo-gate

# Stage 3: Shared Ubuntu runtime base
FROM ubuntu:24.04 AS runtime-base

ARG UBUNTU_MIRROR
ARG UBUNTU_PORTS_MIRROR

# Label for image identification and cleanup
LABEL io.discobot.sandbox-image=true

# Tell systemd it's running inside a container
ENV container=docker

# Install shared apt packages first for better layer caching
# Keep repo COPY steps in later stages so source changes do not invalidate this layer
# systemd + dbus: init system for managing services (PID 1)
# git is needed for workspace cloning
# socat is needed for vsock forwarding in VZ VMs
# nodejs is needed for claude-code-acp
# pnpm is needed for package management
# docker.io provides dockerd daemon and docker CLI (runs inside container with privileged mode)
# docker-buildx is needed for multi-arch builds and advanced build features
# docker-compose-v2 provides the Docker Compose v2 CLI plugin
# iptables and iproute2 are needed by dockerd and runtime diagnostics for network management
RUN if [ -n "${UBUNTU_MIRROR}" ]; then \
        sed -i \
            -e "s|http://archive.ubuntu.com/ubuntu|${UBUNTU_MIRROR}|g" \
            -e "s|http://security.ubuntu.com/ubuntu|${UBUNTU_MIRROR}|g" \
            /etc/apt/sources.list.d/ubuntu.sources; \
    fi \
    && if [ -n "${UBUNTU_PORTS_MIRROR}" ]; then \
        sed -i \
            -e "s|http://ports.ubuntu.com/ubuntu-ports|${UBUNTU_PORTS_MIRROR}|g" \
            /etc/apt/sources.list.d/ubuntu.sources; \
    fi \
    && apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    curl \
    dbus \
    docker-buildx \
    docker-compose-v2 \
    docker.io \
    git \
    iproute2 \
    iptables \
    jq \
    less \
    libnss3-tools \
    openssh-client \
    openssh-sftp-server \
    psmisc \
    poppler-utils \
    ripgrep \
    shellcheck \
    python3 \
    python-is-python3 \
    python3-pip \
    python3-requests \
    python3-venv \
    socat \
    sqlite3 \
    sudo \
    systemd \
    systemd-sysv \
    unzip \
    vim \
    && curl -fsSL https://deb.nodesource.com/setup_25.x | bash - \
    && sed -i 's|http://|https://|g' /etc/apt/sources.list.d/nodesource.list 2>/dev/null || true \
    && mkdir -p /etc/apt/keyrings \
    && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg -o /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" > /etc/apt/sources.list.d/github-cli.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends gh nodejs \
    # Install code-server
    && curl -fsSL https://code-server.dev/install.sh | sh \
    # Seed bundled code-server extensions
    && mkdir -p /opt/discobot/code-server-defaults/extensions \
    && code-server --install-extension vscodevim.vim --extensions-dir /opt/discobot/code-server-defaults/extensions \
    && rm -f /opt/discobot/code-server-defaults/extensions/extensions.json \
    # Install Claude Code CLI and OpenCode CLI
    && npm install -g @anthropic-ai/claude-code @zed-industries/claude-code-acp pnpm opencode-ai \
    # Install latest stable Go
    && GO_VERSION=$(curl -fsSL 'https://go.dev/VERSION?m=text' | head -1) \
    && curl -fsSL "https://go.dev/dl/${GO_VERSION}.linux-$(dpkg --print-architecture).tar.gz" | tar -C /usr/local -xz \
    # Install uv (Python package installer) to /usr/local/bin
    && curl -LsSf https://astral.sh/uv/install.sh | env UV_INSTALL_DIR=/usr/local/bin sh \
    # Install Bun runtime to /usr/local
    && curl -fsSL https://bun.sh/install | BUN_INSTALL=/usr/local bash \
    && rm -rf /var/lib/apt/lists/* /root/.npm \
    # Disable Docker's apt auto-clean so downloaded .deb files persist in /var/cache/apt/archives.
    # This allows apt package downloads to be cached across sessions via cache volume mounts.
    # All image-time apt installs are already complete, so this only affects runtime installs.
    && rm -f /etc/apt/apt.conf.d/docker-clean

# Create discobot user (UID 1000)
# Handle case where UID 1000 might already be taken by another user
# Pre-create /nix so discobot can perform a single-user Nix install without root.
RUN (useradd -m -s /bin/bash -u 1000 discobot 2>/dev/null \
    || (userdel -r $(getent passwd 1000 | cut -d: -f1) 2>/dev/null; useradd -m -s /bin/bash -u 1000 discobot) \
    || useradd -m -s /bin/bash discobot) \
    && usermod -aG systemd-journal discobot \
    && mkdir -m 0755 /nix \
    && chown discobot:discobot /nix

# Install the Discobot sudo gate. The real sudo binary is kept in a
# root-only path; /usr/bin/sudo becomes a setuid Discobot gate that calls the
# local agent API before exec'ing the real sudo binary.
COPY --from=agent-go-builder --chmod=4755 /discobot-sudo-gate /tmp/discobot-sudo-gate
RUN mkdir -p /usr/lib/discobot /etc/discobot \
    && dpkg-divert --rename --add /usr/bin/sudo \
    && mv /usr/bin/sudo.distrib /usr/lib/discobot/sudo.real \
    && chown root:root /usr/lib/discobot/sudo.real \
    && install -m 4755 /tmp/discobot-sudo-gate /usr/bin/sudo \
    && rm -f /tmp/discobot-sudo-gate \
    && chmod 4700 /usr/lib/discobot/sudo.real \
    && printf '%s\n' '{"realSudo":"/usr/lib/discobot/sudo.real","agentAPIURL":"http://127.0.0.1:3002/sudo/authorize"}' > /etc/discobot/sudo-gate.json \
    && chown root:root /etc/discobot/sudo-gate.json \
    && chmod 400 /etc/discobot/sudo-gate.json \
    && printf '%s\n' \
        'Defaults env_keep += "DISCOBOT_SUDO_RUNTIME DISCOBOT_SUDO_TOKEN DISCOBOT_SUDO_CREDENTIAL_ID DISCOBOT_SUDO_USE_ID DISCOBOT_SUDO_TOOL_CALL_ID DISCOBOT_SUDO_COMMAND DISCOBOT_SECRET"' \
        'discobot ALL=(ALL) NOPASSWD:SETENV: ALL' \
        > /etc/sudoers.d/discobot-gated \
    && chmod 440 /etc/sudoers.d/discobot-gated

# Install rustup for discobot user (Rust toolchain manager)
# Must be done after user creation so rust tools are owned by discobot
# Install rustup without any toolchains (users can install toolchains on demand with rustup install)
RUN su - discobot -c 'curl --proto "=https" --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain none'

# Configure npm global directory in /home/discobot/.npm-global
# This allows npm install -g to work without root for the discobot user
# Also add ~/.local/bin so uv-installed Python executables are on PATH by default
# Environment is set system-wide via /etc/profile.d so both root and discobot can use it
RUN mkdir -p /home/discobot/.npm-global/bin /home/discobot/.local/bin \
    && chown -R discobot:discobot /home/discobot/.npm-global /home/discobot/.local \
    && printf '%s\n' \
    '# User-local executables and npm global packages' \
    'export NPM_CONFIG_PREFIX="/home/discobot/.npm-global"' \
    'export PATH="/home/discobot/.local/bin:/home/discobot/.npm-global/bin:$PATH"' \
    > /etc/profile.d/npm-global.sh \
    && chmod 644 /etc/profile.d/npm-global.sh


# Create directory structure per filesystem design
# /.data      - persistent storage (Docker volume or VZ disk)
# /.workspace - base workspace (read-only)
# /workspace  - project root (writable)
RUN mkdir -p /.data /.workspace /opt/discobot/bin \
    && chown discobot:discobot /.data

# Add discobot binaries, user-local bin, and npm global bin to PATH
# Also set NPM_CONFIG_PREFIX for non-login shell contexts
# Set PNPM_HOME to use persistent storage for pnpm cache/store
# Add Rust cargo bin for rustc and cargo
# Claude CLI is installed to /usr/local/bin (already in default PATH)
ENV NPM_CONFIG_PREFIX="/home/discobot/.npm-global"
ENV PNPM_HOME="/.data/pnpm"
ENV PATH="/home/discobot/.cargo/bin:/usr/local/go/bin:/home/discobot/.local/bin:/home/discobot/.npm-global/bin:/opt/discobot/bin:${PATH}"
ENV WORKSPACE_PATH=/home/discobot/workspace

WORKDIR /workspace

EXPOSE 3002

# systemd as PID 1 — manages discobot services (setup, proxy, dockerd, agent-api)
# SIGRTMIN+3 tells systemd to shut down cleanly (used by docker stop)
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]

# Stage 3b: Shared graphical runtime base
FROM runtime-base AS runtime-gui-base

# Install graphical packages: virtual X11 display, VNC, window manager, browser.
# Ubuntu's chromium package is a Snap stub and the Launchpad-hosted Chromium
# PPAs are slow/unreliable. Debian publishes real chromium .deb packages for
# both amd64 and arm64, so use Debian only for chromium and keep Ubuntu as the
# source for all other packages.
RUN apt-get update && apt-get install -y --no-install-recommends \
    debian-archive-keyring \
    menu \
    openbox \
    pcmanfm \
    python3-xdg \
    python3-websockify \
    scrot \
    x11vnc \
    xdotool \
    xterm \
    xvfb \
    && printf '%s\n' \
    'Types: deb' \
    'URIs: https://deb.debian.org/debian' \
    'Suites: bookworm' \
    'Components: main' \
    'Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg' \
    '' \
    'Types: deb' \
    'URIs: https://security.debian.org/debian-security' \
    'Suites: bookworm-security' \
    'Components: main' \
    'Signed-By: /usr/share/keyrings/debian-archive-keyring.gpg' \
    > /etc/apt/sources.list.d/debian-chromium.sources \
    && printf '%s\n' \
    'Package: *' \
    'Pin: release n=bookworm' \
    'Pin-Priority: 100' \
    '' \
    'Package: *' \
    'Pin: release n=bookworm-security' \
    'Pin-Priority: 100' \
    '' \
    'Package: chromium chromium-common chromium-sandbox' \
    'Pin: release n=bookworm' \
    'Pin-Priority: 990' \
    '' \
    'Package: chromium chromium-common chromium-sandbox' \
    'Pin: release n=bookworm-security' \
    'Pin-Priority: 990' \
    > /etc/apt/preferences.d/debian-chromium \
    && apt-get update \
    && apt-get install -y --no-install-recommends chromium \
    && rm -rf /var/lib/apt/lists/* /var/cache/apt/archives/*.deb /var/cache/apt/archives/partial/*.deb

# Configure Openbox to autostart PCManFM in desktop mode (renders desktop icons)
# Configure libfm to launch executable .desktop files without the "Execute File" prompt
RUN mkdir -p /home/discobot/.config/openbox /home/discobot/.config/libfm \
    && printf '%s\n' \
    '# Launch PCManFM in desktop mode to render desktop icons' \
    'pcmanfm --desktop &' \
    > /home/discobot/.config/openbox/autostart \
    && printf '%s\n' \
    '[config]' \
    'single_click=0' \
    'use_trash=1' \
    'confirm_del=1' \
    'confirm_trash=1' \
    'quick_exec=1' \
    > /home/discobot/.config/libfm/libfm.conf \
    && chown -R discobot:discobot /home/discobot/.config

# Create desktop shortcuts for Chromium and XTerm
RUN mkdir -p /home/discobot/Desktop \
    && printf '%s\n' \
    '[Desktop Entry]' \
    'Type=Application' \
    'Name=Chromium' \
    'Exec=chromium' \
    'Icon=chromium' \
    'Terminal=false' \
    'Categories=Network;WebBrowser;' \
    > /home/discobot/Desktop/chromium.desktop \
    && printf '%s\n' \
    '[Desktop Entry]' \
    'Type=Application' \
    'Name=XTerm' \
    'Exec=xterm' \
    'Icon=xterm-color' \
    'Terminal=false' \
    'Categories=System;TerminalEmulator;' \
    > /home/discobot/Desktop/xterm.desktop \
    && chmod 755 /home/discobot/Desktop/*.desktop \
    && chown -R discobot:discobot /home/discobot/Desktop

ENV DISPLAY=:0

EXPOSE 5900

# Stage 3b: Package browser-harness under its upstream command name
FROM runtime-base AS browser-harness-builder

ARG BROWSER_HARNESS_REPO=https://github.com/browser-use/browser-harness.git
ARG BROWSER_HARNESS_REF=main

RUN git clone --depth 1 --branch "${BROWSER_HARNESS_REF}" "${BROWSER_HARNESS_REPO}" /tmp/browser-harness \
    && mkdir -p /opt/browser-harness-skills/browser-harness \
    && cp -a /tmp/browser-harness/. /opt/browser-harness-skills/browser-harness/ \
    && uv venv /opt/browser-harness \
    && uv pip install --python /opt/browser-harness/bin/python /tmp/browser-harness \
    && ln -s /opt/browser-harness/bin/browser-harness /usr/local/bin/browser-harness \
    && rm -rf /tmp/browser-harness /root/.cache/uv

# Stage 3c: Runtime overlay with frequently-changing binaries and container assets
FROM scratch AS runtime-overlay

# Copy binaries to /opt/discobot/bin
COPY --from=agent-go-builder --chmod=755 /discobot-agent-api /opt/discobot/bin/discobot-agent-api
COPY --from=proxy-builder --chmod=755 /proxy /opt/discobot/bin/proxy
COPY --from=agent-builder --chmod=755 /discobot-agent /opt/discobot/bin/discobot-agent

# Copy browser-harness runtime and expose it at /usr/local/bin/browser-harness
COPY --from=browser-harness-builder /opt/browser-harness /opt/browser-harness
COPY --from=browser-harness-builder /usr/local/bin/browser-harness /usr/local/bin/browser-harness
COPY --from=browser-harness-builder /opt/browser-harness-skills/ /opt/discobot/skills/

# Docker wrapper: injects --output type=docker for build commands so remote
# buildx builders always load images into the local daemon.
COPY --chmod=755 container-assets/docker-wrapper.sh /usr/local/bin/docker
COPY --chmod=755 container-assets/discobot-session-env.sh /usr/local/bin/discobot-session-env

# Copy systemd service files for container service management
COPY container-assets/systemd/ /etc/systemd/system/

# Copy code-server default profile templates
COPY --chown=1000:1000 container-assets/code-server/ /opt/discobot/code-server-defaults/

# Copy container-specific agent configuration (Discobot scripts, docs, etc.)
# into system-level Discobot directories for session config discovery.
COPY container-assets/discobot/scripts/ /opt/discobot/scripts/
COPY --chown=1000:1000 container-assets/docs.txt /discobot/docs.txt

# Stage 3d: Minimal runtime without graphical tools
FROM runtime-base AS runtime-shell

COPY --from=runtime-overlay / /

# Configure systemd for container environment
# Disable docker.service so it only starts via docker.socket activation
# (the Ubuntu docker.io package preset enables it by default)
RUN ln -s /opt/discobot/bin/discobot-agent-api /opt/discobot/bin/disco \
    && systemctl mask \
    console-getty.service \
    getty@.service \
    serial-getty@.service \
    && systemctl disable docker.service containerd.service \
    && systemctl enable \
    discobot-setup.service \
    discobot-proxy.service \
    docker.socket \
    discobot-agent-api.service \
    discobot-vscode.socket

# Stage 3e: Full runtime with graphical desktop tools (X11, VNC, browser)
FROM runtime-gui-base AS runtime

COPY --from=runtime-overlay / /

# Configure systemd for container environment
# Disable docker.service so it only starts via docker.socket activation
# (the Ubuntu docker.io package preset enables it by default)
RUN ln -s /opt/discobot/bin/discobot-agent-api /opt/discobot/bin/disco \
    && systemctl mask \
    console-getty.service \
    getty@.service \
    serial-getty@.service \
    systemd-logind.service \
    && systemctl disable docker.service containerd.service \
    && systemctl enable \
    discobot-setup.service \
    discobot-proxy.service \
    docker.socket \
    discobot-agent-api.service \
    discobot-vscode.socket \
    x11-display.socket \
    x11vnc.socket \
    websockify-proxy.socket

# Stage 4: VZ root filesystem builder with systemd and Docker
# Build with: docker build --target vz-image -t discobot-vz .
# Then extract /vmlinuz and /discobot-rootfs.squashfs with docker cp from a
# temporary container. The watcher uses this flow so local Windows/WSL builds
# do not rely on docker build --output extraction.
# This creates a minimal systemd-based system with Docker daemon for macOS Virtualization.framework
# This stage is completely independent from the runtime image
FROM ubuntu:24.04 AS vz-rootfs-builder

ARG UBUNTU_MIRROR
ARG UBUNTU_PORTS_MIRROR

# Docker image to preload into the VM at build time (pulled via crane as OCI tarball)
# Defaults to the main tag of the discobot runtime image
ARG PRELOAD_IMAGE=ghcr.io/obot-platform/discobot:main

# Prevent interactive prompts during package installation
ENV DEBIAN_FRONTEND=noninteractive

# Install kernel, systemd, Docker, and minimal tools
# Use a specific stable kernel version with virtio drivers built-in
RUN if [ -n "${UBUNTU_MIRROR}" ]; then \
        sed -i \
            -e "s|http://archive.ubuntu.com/ubuntu|${UBUNTU_MIRROR}|g" \
            -e "s|http://security.ubuntu.com/ubuntu|${UBUNTU_MIRROR}|g" \
            /etc/apt/sources.list.d/ubuntu.sources; \
    fi \
    && if [ -n "${UBUNTU_PORTS_MIRROR}" ]; then \
        sed -i \
            -e "s|http://ports.ubuntu.com/ubuntu-ports|${UBUNTU_PORTS_MIRROR}|g" \
            /etc/apt/sources.list.d/ubuntu.sources; \
    fi \
    && apt-get update && apt-get install -y --no-install-recommends \
    # Kernel with virtio support built-in (no modules needed)
    # Using specific version to avoid metapackage dependency issues
    linux-image-6.8.0-31-generic \
    linux-modules-6.8.0-31-generic \
    # systemd as init system with network support
    systemd \
    systemd-sysv \
    systemd-resolved \
    systemd-timesyncd \
    # Docker daemon and dependencies
    docker.io \
    iptables \
    # Minimal essential tools
    ca-certificates \
    curl \
    socat \
    # e2fsprogs for mkfs.ext4 to format data disk
    e2fsprogs \
    # udev for device enumeration
    udev \
    && rm -rf /var/lib/apt/lists/*

# Pull the preload image as an OCI tarball using crane
# crane is a standalone tool from go-containerregistry that doesn't need Docker daemon
# TARGETARCH is automatically set by Docker buildx (amd64 or arm64)
ARG TARGETARCH
RUN set -ex \
    # Install crane from go-containerregistry releases with checksum verification
    && CRANE_VERSION="v0.20.7" \
    # Map Docker TARGETARCH to crane release filename arch
    && if [ "${TARGETARCH}" = "amd64" ]; then \
    CRANE_ARCH="x86_64"; \
    CRANE_SHA256="8ef3564d264e6b5ca93f7b7f5652704c4dd29d33935aff6947dd5adefd05953e"; \
    else \
    CRANE_ARCH="${TARGETARCH}"; \
    CRANE_SHA256="b04ee6e4904d9219c76383f5b73521a63f69ecc93c0b1840846eebfd071a6355"; \
    fi \
    && curl -fsSL -o /tmp/crane.tar.gz \
    "https://github.com/google/go-containerregistry/releases/download/${CRANE_VERSION}/go-containerregistry_Linux_${CRANE_ARCH}.tar.gz" \
    && echo "${CRANE_SHA256}  /tmp/crane.tar.gz" | sha256sum -c - \
    && tar -xzf /tmp/crane.tar.gz -C /usr/local/bin crane \
    && chmod +x /usr/local/bin/crane \
    && rm -f /tmp/crane.tar.gz \
    # Pull the image as an OCI tarball for the target architecture
    && echo "Pulling ${PRELOAD_IMAGE} for linux/${TARGETARCH}..." \
    && crane pull --platform "linux/${TARGETARCH}" "${PRELOAD_IMAGE}" /preload-image.tar \
    && echo "Preload image saved to /preload-image.tar" \
    # Save the image reference for the boot-time load script
    && echo "${PRELOAD_IMAGE}" > /preload-image-tag \
    # Clean up crane binary (not needed at runtime)
    && rm -f /usr/local/bin/crane

# Create /var skeleton for first-boot initialization
# This is copied to /var after the data disk is mounted
RUN cp -a /var /var.skel

# Copy shared guest assets (systemd units, scripts, network config, fstab, WSL config)
COPY vm-assets/fstab /etc/fstab
COPY vm-assets/wsl/wsl.conf /etc/wsl.conf
COPY vm-assets/systemd/docker-vsock-proxy.service /etc/systemd/system/
COPY vm-assets/systemd/init-var.service /etc/systemd/system/
COPY vm-assets/systemd/mount-home.service /etc/systemd/system/
COPY vm-assets/systemd/preload-image.service /etc/systemd/system/
COPY vm-assets/systemd/docker.service.d/ /etc/systemd/system/docker.service.d/
COPY vm-assets/systemd/containerd.service.d/ /etc/systemd/system/containerd.service.d/
COPY vm-assets/systemd/systemd-networkd.service.d/ /etc/systemd/system/systemd-networkd.service.d/
COPY vm-assets/systemd/systemd-networkd-wait-online.service.d/ /etc/systemd/system/systemd-networkd-wait-online.service.d/
COPY vm-assets/systemd/systemd-timesyncd.service.d/ /etc/systemd/system/systemd-timesyncd.service.d/
COPY vm-assets/systemd/systemd-resolved.service.d/ /etc/systemd/system/systemd-resolved.service.d/
COPY vm-assets/network/20-dhcp.network /etc/systemd/network/
COPY --chmod=755 vm-assets/scripts/check-wsl-role.sh /usr/local/bin/
COPY --chmod=755 vm-assets/scripts/init-var.sh /usr/local/bin/
COPY --chmod=755 vm-assets/scripts/mount-home.sh /usr/local/bin/
COPY --chmod=755 vm-assets/scripts/preload-image.sh /usr/local/bin/

# Configure systemd for VM environment
RUN set -ex \
    # Disable unnecessary systemd services.
    && systemctl mask \
    getty@.service \
    serial-getty@.service \
    # Enable the network stack in the shared image; WSL role-aware drop-ins
    # skip these units there while VZ keeps using them.
    && systemctl enable \
    systemd-networkd \
    systemd-resolved \
    systemd-timesyncd \
    fstrim.timer \
    # Enable /var initialization and home mount services
    && systemctl enable init-var.service \
    && systemctl enable mount-home.service \
    # Enable Docker service, vsock proxy, and preloaded image loader
    && systemctl enable docker \
    && systemctl enable docker-vsock-proxy \
    && systemctl enable preload-image

# Create discobot user (UID 1000)
RUN useradd -m -s /bin/bash -u 1000 discobot || \
    (userdel -r $(getent passwd 1000 | cut -d: -f1) 2>/dev/null; useradd -m -s /bin/bash -u 1000 discobot)

# Create minimal directory structure for VM
# /Users is for macOS host home directory VirtioFS mounts (root is read-only squashfs)
RUN mkdir -p /.data /.workspace /workspace /Users \
    && chown discobot:discobot /.data /workspace

# Stage 5: Extract kernel and initrd, create root filesystem image
FROM ubuntu:24.04 AS vz-image-builder

ARG UBUNTU_MIRROR
ARG UBUNTU_PORTS_MIRROR

# Install tools for image creation and kernel extraction
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && if [ -n "${UBUNTU_MIRROR}" ]; then \
        sed -i \
            -e "s|http://archive.ubuntu.com/ubuntu|${UBUNTU_MIRROR}|g" \
            -e "s|http://security.ubuntu.com/ubuntu|${UBUNTU_MIRROR}|g" \
            /etc/apt/sources.list.d/ubuntu.sources; \
    fi \
    && if [ -n "${UBUNTU_PORTS_MIRROR}" ]; then \
        sed -i \
            -e "s|http://ports.ubuntu.com/ubuntu-ports|${UBUNTU_PORTS_MIRROR}|g" \
            /etc/apt/sources.list.d/ubuntu.sources; \
    fi \
    && apt-get update && apt-get install -y --no-install-recommends \
    squashfs-tools \
    zstd \
    && rm -rf /var/lib/apt/lists/*

# Copy the rootfs from builder
COPY --from=vz-rootfs-builder / /rootfs

# Extract kernel from /rootfs/boot (no initrd needed)
RUN set -ex \
    && cd /rootfs/boot \
    # Find the kernel (vmlinuz-*)
    && KERNEL=$(ls -1 vmlinuz-* | head -1) \
    && KERNEL_VERSION=$(echo $KERNEL | sed 's/vmlinuz-//') \
    && echo "Found kernel: $KERNEL (version: $KERNEL_VERSION)" \
    # Copy kernel to root for extraction
    && cp "$KERNEL" /vmlinuz \
    # Save kernel version
    && echo "$KERNEL_VERSION" > /kernel-version

# Prepare rootfs for VM use
RUN set -ex \
    # Create essential mount points
    && mkdir -p /rootfs/proc /rootfs/sys /rootfs/dev /rootfs/run /rootfs/tmp \
    # VZ uses systemd-resolved's stub listener here. WSL rewrites
    # /etc/resolv.conf to /mnt/wsl/resolv.conf at runtime.
    && rm -f /rootfs/etc/resolv.conf \
    && ln -s /run/systemd/resolve/stub-resolv.conf /rootfs/etc/resolv.conf \
    # Clean up /boot to save space (kernel/initrd already extracted)
    && rm -rf /rootfs/boot/*

# Create SquashFS image with zstd compression for Apple VZ and a tar.zst
# archive for managed WSL imports.
# SquashFS is built into the kernel - no initrd needed!
# Boot with: root=/dev/vda rootfstype=squashfs ro
RUN set -ex \
    && ROOTFS_SIZE_MB=$(du -sm /rootfs | cut -f1) \
    && echo "Rootfs size: ${ROOTFS_SIZE_MB}MB" \
    && echo "Creating SquashFS image with zstd compression..." \
    && mksquashfs /rootfs /rootfs.squashfs \
    -comp zstd \
    -Xcompression-level 19 \
    -noappend \
    && SQUASHFS_SIZE_MB=$(du -m /rootfs.squashfs | cut -f1) \
    && RATIO=$((100 - (SQUASHFS_SIZE_MB * 100 / ROOTFS_SIZE_MB))) \
    && echo "SquashFS image: ${SQUASHFS_SIZE_MB}MB (${RATIO}% reduction)" \
    && echo "Creating WSL rootfs archive with zstd compression..." \
    && tar --numeric-owner -C /rootfs -cf - . | zstd -T0 -19 -o /discobot-rootfs.tar.zst \
    && ROOTFS_TAR_SIZE_MB=$(du -m /discobot-rootfs.tar.zst | cut -f1) \
    && TAR_RATIO=$((100 - (ROOTFS_TAR_SIZE_MB * 100 / ROOTFS_SIZE_MB))) \
    && echo "WSL rootfs archive: ${ROOTFS_TAR_SIZE_MB}MB (${TAR_RATIO}% reduction)"

# Stage 6: Output stage with kernel and SquashFS root filesystem (no initrd needed)
# This target is published as the macOS VZ guest image.
FROM scratch AS vz-image
COPY --from=vz-image-builder /vmlinuz /vmlinuz
COPY --from=vz-image-builder /kernel-version /kernel-version
COPY --from=vz-image-builder /rootfs.squashfs /discobot-rootfs.squashfs

# Stage 7: Output stage with WSL rootfs archive
# This target is published as the Windows WSL guest image.
FROM scratch AS wsl-image
COPY --from=vz-image-builder /discobot-rootfs.tar.zst /discobot-rootfs.tar.zst

# Default target: runtime image
FROM runtime
