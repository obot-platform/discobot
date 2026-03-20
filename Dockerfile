# Stage 1: Build the proxy binary from source
FROM golang:1.26 AS proxy-builder

WORKDIR /build

# Copy module files first for better caching
# modelsdev/go.mod is needed by the replace directive in the root go.mod
COPY modelsdev/go.mod ./modelsdev/
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy proxy source
COPY proxy/ ./proxy/

# Build the proxy binary
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /proxy ./proxy/cmd/proxy

# Stage 1b: Build the agent init process from source
FROM golang:1.26 AS agent-builder

WORKDIR /build

# Copy module files first for better caching
# modelsdev/go.mod is needed by the replace directive in the root go.mod
COPY modelsdev/go.mod ./modelsdev/
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy agent source (including embedded proxy config)
COPY agent/ ./agent/

# Build the agent binary (static for portability)
# The go:embed directive will include agent/internal/proxy/default-config.yaml
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /discobot-agent ./agent/cmd/agent

# Stage 2: Build agent-go as the discobot-agent-api binary
FROM golang:1.26 AS agent-go-builder

WORKDIR /build

# Copy modelsdev module files first — needed by the replace directive in agent-go/go.mod
# (replace ../modelsdev resolves to /modelsdev relative to WORKDIR /build)
COPY modelsdev/go.mod /modelsdev/

# Copy module files first for better layer caching
COPY agent-go/go.mod agent-go/go.sum ./

# Download dependencies
RUN go mod download

# Copy modelsdev source (required for compilation, not just module resolution)
COPY modelsdev/ /modelsdev/

# Copy agent-go source
COPY agent-go/ ./

# Build the agent-go binary as discobot-agent-api
# Use mcp_go_client_oauth build tag to enable OAuth support for MCP tools
RUN CGO_ENABLED=0 go build -tags mcp_go_client_oauth -ldflags="-s -w" -o /discobot-agent-api ./cmd/agent-api

# Stage 3: Shared Ubuntu runtime base
FROM ubuntu:24.04 AS runtime-base

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
# iptables is needed by dockerd for network management
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && sed -i 's|http://|https://|g' /etc/apt/sources.list.d/ubuntu.sources \
    && apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    curl \
    dbus \
    docker-buildx \
    docker.io \
    git \
    iptables \
    jq \
    less \
    openssh-client \
    openssh-sftp-server \
    psmisc \
    poppler-utils \
    ripgrep \
    shellcheck \
    python3 \
    python-is-python3 \
    python3-pip \
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

# Explicitly deny sudo access for discobot user
RUN echo 'discobot ALL=(ALL) !ALL' > /etc/sudoers.d/discobot-deny \
    && chmod 440 /etc/sudoers.d/discobot-deny

# Install rustup for discobot user (Rust toolchain manager)
# Must be done after user creation so rust tools are owned by discobot
# Install rustup without any toolchains (users can install toolchains on demand with rustup install)
RUN su - discobot -c 'curl --proto "=https" --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain none'

# Configure npm global directory in /home/discobot/.npm-global
# This allows npm install -g to work without root for the discobot user
# Environment is set system-wide via /etc/profile.d so both root and discobot can use it
RUN mkdir -p /home/discobot/.npm-global/bin \
    && chown -R discobot:discobot /home/discobot/.npm-global \
    && printf '%s\n' \
    '# npm global packages directory' \
    'export NPM_CONFIG_PREFIX="/home/discobot/.npm-global"' \
    'export PATH="/home/discobot/.npm-global/bin:$PATH"' \
    > /etc/profile.d/npm-global.sh \
    && chmod 644 /etc/profile.d/npm-global.sh

# Create directory structure per filesystem design
# /.data      - persistent storage (Docker volume or VZ disk)
# /.workspace - base workspace (read-only)
# /workspace  - project root (writable)
RUN mkdir -p /.data /.workspace /opt/discobot/bin \
    && chown discobot:discobot /.data

# Add discobot binaries and npm global bin to PATH
# Also set NPM_CONFIG_PREFIX for non-login shell contexts
# Set PNPM_HOME to use persistent storage for pnpm cache/store
# Add Rust cargo bin for rustc and cargo
# Claude CLI is installed to /usr/local/bin (already in default PATH)
ENV NPM_CONFIG_PREFIX="/home/discobot/.npm-global"
ENV PNPM_HOME="/.data/pnpm"
ENV PATH="/home/discobot/.cargo/bin:/usr/local/go/bin:/home/discobot/.npm-global/bin:/opt/discobot/bin:${PATH}"
ENV WORKSPACE_PATH=/home/discobot/workspace

WORKDIR /workspace

EXPOSE 3002

# systemd as PID 1 — manages discobot services (setup, proxy, dockerd, agent-api)
# SIGRTMIN+3 tells systemd to shut down cleanly (used by docker stop)
STOPSIGNAL SIGRTMIN+3
CMD ["/sbin/init"]

# Stage 3b: Shared graphical runtime base
FROM runtime-base AS runtime-gui-base

# Install graphical packages: virtual X11 display, VNC, window manager, browser
RUN apt-get update && apt-get install -y --no-install-recommends \
    menu \
    openbox \
    pcmanfm \
    python3-xdg \
    python3-websockify \
    scrot \
    software-properties-common \
    x11vnc \
    xdotool \
    xterm \
    xvfb \
    && add-apt-repository -y ppa:xtradeb/apps \
    && apt-get update && apt-get install -y --no-install-recommends chromium \
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

# Stage 3c: Runtime overlay with frequently-changing binaries and container assets
FROM scratch AS runtime-overlay

# Copy binaries to /opt/discobot/bin
COPY --from=agent-go-builder --chmod=755 /discobot-agent-api /opt/discobot/bin/discobot-agent-api
COPY --from=proxy-builder --chmod=755 /proxy /opt/discobot/bin/proxy
COPY --from=agent-builder --chmod=755 /discobot-agent /opt/discobot/bin/discobot-agent

# Docker wrapper: injects --output type=docker for build commands so remote
# buildx builders always load images into the local daemon.
COPY --chmod=755 container-assets/docker-wrapper.sh /usr/local/bin/docker

# Copy systemd service files for container service management
COPY container-assets/systemd/ /etc/systemd/system/

# Copy container-specific agent configuration (Claude Code commands, etc.)
# These are placed in /home/discobot/.claude/ for user-level availability
COPY --chown=1000:1000 container-assets/claude /home/discobot/.claude
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
    systemd-logind.service \
    && systemctl disable docker.service containerd.service \
    && systemctl enable \
    discobot-setup.service \
    discobot-proxy.service \
    docker.socket \
    discobot-agent-api.service

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
    x11-display.socket \
    x11vnc.socket \
    websockify-proxy.socket

# Stage 4: VZ root filesystem builder with systemd and Docker
# Build with: docker build --target vz-image --output type=local,dest=. .
# This creates a minimal systemd-based system with Docker daemon for macOS Virtualization.framework
# This stage is completely independent from the runtime image
FROM ubuntu:24.04 AS vz-rootfs-builder

# Docker image to preload into the VM at build time (pulled via crane as OCI tarball)
# Defaults to the main tag of the discobot runtime image
ARG PRELOAD_IMAGE=ghcr.io/obot-platform/discobot:main

# Prevent interactive prompts during package installation
ENV DEBIAN_FRONTEND=noninteractive

# Install kernel, systemd, Docker, and minimal tools
# Use a specific stable kernel version with virtio drivers built-in
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && sed -i 's|http://|https://|g' /etc/apt/sources.list.d/ubuntu.sources \
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

# Copy VM assets (systemd units, scripts, network config, fstab)
COPY vm-assets/fstab /etc/fstab
COPY vm-assets/systemd/docker-vsock-proxy.service /etc/systemd/system/
COPY vm-assets/systemd/init-var.service /etc/systemd/system/
COPY vm-assets/systemd/mount-home.service /etc/systemd/system/
COPY vm-assets/systemd/preload-image.service /etc/systemd/system/
COPY vm-assets/systemd/docker.service.d/ /etc/systemd/system/docker.service.d/
COPY vm-assets/systemd/containerd.service.d/ /etc/systemd/system/containerd.service.d/
COPY vm-assets/network/20-dhcp.network /etc/systemd/network/
COPY --chmod=755 vm-assets/scripts/init-var.sh /usr/local/bin/
COPY --chmod=755 vm-assets/scripts/mount-home.sh /usr/local/bin/
COPY --chmod=755 vm-assets/scripts/preload-image.sh /usr/local/bin/

# Configure systemd for VM environment
RUN set -ex \
    # Disable unnecessary systemd services (but keep network services)
    && systemctl mask \
    getty@.service \
    serial-getty@.service \
    # Enable network services for connectivity
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

# Install tools for image creation and kernel extraction
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && sed -i 's|http://|https://|g' /etc/apt/sources.list.d/ubuntu.sources \
    && apt-get update && apt-get install -y --no-install-recommends \
    squashfs-tools \
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
    # Configure systemd-resolved: symlink resolv.conf to stub resolver
    # This routes DNS queries through resolved's stub listener at 127.0.0.53
    && rm -f /rootfs/etc/resolv.conf \
    && ln -s /run/systemd/resolve/stub-resolv.conf /rootfs/etc/resolv.conf \
    # Clean up /boot to save space (kernel/initrd already extracted)
    && rm -rf /rootfs/boot/*

# Create SquashFS image with zstd compression
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
    -info \
    && SQUASHFS_SIZE_MB=$(du -m /rootfs.squashfs | cut -f1) \
    && RATIO=$((100 - (SQUASHFS_SIZE_MB * 100 / ROOTFS_SIZE_MB))) \
    && echo "SquashFS image: ${SQUASHFS_SIZE_MB}MB (${RATIO}% reduction)"

# Stage 6: Output stage with kernel and SquashFS root filesystem (no initrd needed)
FROM scratch AS vz-image
COPY --from=vz-image-builder /vmlinuz /vmlinuz
COPY --from=vz-image-builder /kernel-version /kernel-version
COPY --from=vz-image-builder /rootfs.squashfs /discobot-rootfs.squashfs

# Default target: runtime image
FROM runtime
