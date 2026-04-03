# Proxy Integration with Agent

This document describes how the HTTP proxy with Docker registry caching has been integrated into the Discobot agent.

## Overview

The proxy is now automatically started when an agent container launches and configured to intercept all HTTP/HTTPS traffic from the agent-api and its child processes. This provides:

1. **Docker registry caching** - Dramatically speeds up repeated Docker pulls
2. **Traffic inspection** - All HTTP/HTTPS traffic flows through the proxy
3. **Header injection** - Can inject authentication headers for private registries
4. **Request filtering** - Can allowlist/blocklist specific domains

## Architecture

```
Container Start (PID 1: agent init)
    ↓
1. Base home setup
2. Workspace cloning
3. Filesystem setup (OverlayFS/AgentFS)
4. Create /workspace symlink
5. >>> Setup proxy config <<<
6. >>> Generate CA certificate & install in system trust <<<
7. >>> Start proxy daemon (NEW) <<<
8. Start Docker daemon (with proxy env vars)
9. Start agent-api (with proxy env vars)
    ↓
All HTTP/HTTPS traffic → Proxy (port 17080) → Upstream
```

## Port Configuration

The proxy uses **uncommon ports** to avoid conflicts:

- **Proxy port**: 17080 (HTTP/HTTPS + SOCKS5)
- **API port**: 17081 (REST API for stats/config)

These ports were chosen to be high enough to avoid common service conflicts but low enough to be memorable.

## Environment Variables

When the proxy is running, the following environment variables are automatically set for the agent-api and all child processes:

```bash
HTTP_PROXY=http://localhost:17080
HTTPS_PROXY=http://localhost:17080
http_proxy=http://localhost:17080
https_proxy=http://localhost:17080
```

This causes all HTTP/HTTPS clients (curl, wget, Docker, npm, pip, etc.) to automatically use the proxy.

## Proxy Data Directory

The proxy stores its data in `/.data/proxy/`:

```
/.data/proxy/
├── certs/          # CA certificate (generated on first run)
│   ├── ca.crt     # Public certificate (install in trust store if needed)
│   └── ca.key     # Private key
└── cache/          # Response cache (if caching enabled)
    ├── <hash>      # Cached response data
    └── <hash>.meta # Metadata (original cache key)
```

## Startup Flow

### 1. Workspace Cloning

The agent clones the workspace from git (if specified) or copies from the mount point.

### 2. Configuration Setup

**After workspace is available**, the agent sets up the proxy configuration:

1. Checks for workspace-specific config at `/home/discobot/workspace/.discobot/proxy/config.yaml`
2. If found: Copies workspace config to `/.data/proxy/config.yaml`
3. If not found: Writes built-in default config (with Docker caching enabled) to `/.data/proxy/config.yaml`

This happens **before** the proxy starts, ensuring configuration is always available.

### 3. CA Certificate Generation & System Trust

**Before starting the proxy**, the agent generates a CA certificate for HTTPS interception:

1. **Check for existing certificate** at `/.data/proxy/certs/ca.crt`
2. **Generate new certificate** if not found:
   - Uses Go's crypto/x509 library to create a 2048-bit RSA key pair
   - Creates a self-signed X.509 certificate valid for 10 years
   - Subject: `O=Discobot Proxy, CN=Discobot Proxy CA`
   - **SANs (Subject Alternative Names)**: `localhost`, `127.0.0.1`, `::1`
   - Stored at `/.data/proxy/certs/ca.crt` (public) and `ca.key` (private, mode 0600)

3. **Install in system trust store**:
   - **Debian/Ubuntu/Alpine**: Copies to `/usr/local/share/ca-certificates/` and runs `update-ca-certificates`
   - **Fedora/RHEL/CentOS**: Copies to `/etc/pki/ca-trust/source/anchors/` and runs `update-ca-trust extract`
   - **Other systems**: Warns if no update tool found but continues

This ensures that **all processes in the container trust the proxy's CA**, allowing transparent HTTPS interception without certificate errors.

### 4. Directory Setup

Creates and chowns proxy directories:
- `/.data/proxy/` (main data dir)
- `/.data/proxy/certs/` (certificate storage)
- `/.data/proxy/cache/` (response cache)
- `/.data/proxy/config.yaml` (configuration file)

### 5. Proxy Launch

Starts the proxy with the config file:

```bash
/opt/discobot/bin/proxy -config /.data/proxy/config.yaml
```

### 6. Health Check

Waits up to 10 seconds for the proxy API port (17081) to accept connections. If the proxy doesn't become ready, it's killed and startup continues without it.

**Important**: The Docker daemon is started **after** the proxy is ready, so Docker can use the proxy for image pulls from the very first pull.

### 7. Environment Variables

If proxy startup succeeded, sets proxy environment variables in multiple locations:

**A. Process Environment** (Docker daemon and agent-api):
- `HTTP_PROXY=http://localhost:17080`
- `HTTPS_PROXY=http://localhost:17080`
- `ALL_PROXY=http://localhost:17080` (for SOCKS5)
- `NO_PROXY=localhost,127.0.0.1,::1` (bypass proxy for localhost)
- `NODE_EXTRA_CA_CERTS=/.data/proxy/certs/ca.crt` (Node.js: trust proxy CA)
- Lowercase variants also set for all variables (except NODE_EXTRA_CA_CERTS)

**B. System Profile** (`/etc/profile.d/discobot-proxy.sh`):
- Same environment variables written to profile script
- Automatically sourced by login shells (bash, sh, zsh)
- Falls back to `/etc/profile` if `/etc/profile.d` doesn't exist

This ensures:
- ✅ Docker image pulls use the proxy cache
- ✅ Agent-api HTTP requests use the proxy cache
- ✅ Interactive shells (ssh, docker exec) automatically use the proxy
- ✅ Node.js processes trust the proxy CA for HTTPS
- ✅ All processes spawned from login shells inherit proxy settings

## Shutdown Flow

During container shutdown, the proxy is gracefully terminated:

1. Send SIGTERM to proxy process
2. Wait up to 5 seconds for graceful shutdown
3. If still running, send SIGKILL
4. Proceed with Docker daemon shutdown

The proxy is shut down **before** the Docker daemon to ensure any pending Docker operations complete through the proxy.

## Configuration

### Default Configuration

**Docker caching is now enabled by default!**

The agent includes a built-in default configuration with Docker registry caching enabled:
- ✅ **Docker caching enabled** - Caches blob layers and manifests
- ✅ **20GB cache size** - Adjustable via workspace config
- ❌ **No filtering** - All domains allowed
- ❌ **No header injection** - Pass-through mode
- ✅ **Request logging** - Basic request/response logs

### Configuration Priority

The agent looks for proxy configuration in this order:

1. **Workspace config** (highest priority): `.discobot/proxy/config.yaml` in your workspace
2. **Built-in default** (fallback): Embedded config with Docker caching enabled

### Workspace-Specific Configuration

To customize proxy settings for a specific workspace, create `.discobot/proxy/config.yaml` in your repository:

```yaml
# .discobot/proxy/config.yaml
proxy:
  port: 17080
  api_port: 17081

tls:
  cert_dir: /.data/proxy/certs

cache:
  enabled: true
  dir: /.data/proxy/cache
  max_size: 53687091200  # 50GB (increase for team usage)
  patterns:
    - "^/v2/.*/blobs/sha256:.*"                           # Docker blob layers
    - "^/v2/.*/manifests/sha256:.*"                       # Docker manifests by digest
    - "^/registry-v2/docker/registry/v2/blobs/sha256/"    # Local registry storage

logging:
  level: info
  format: text
```

The agent will automatically detect and use this file when the container starts.

### Advanced Configuration

For advanced use cases like header injection or domain filtering:

```yaml
proxy:
  port: 17080
  api_port: 17081

cache:
  enabled: true
  dir: /.data/proxy/cache
  max_size: 21474836480

headers:
  "registry.example.com":
    set:
      "Authorization": "Bearer YOUR_TOKEN"

allowlist:
  enabled: false  # Set to true to restrict domains
  domains: []
```

## Monitoring

### Cache Statistics

Get cache performance metrics via the API:

```bash
curl http://localhost:17081/api/cache/stats
```

Response:
```json
{
  "hits": 150,
  "misses": 25,
  "stores": 25,
  "evictions": 2,
  "errors": 0,
  "current_size": 8589934592,
  "hit_rate": 0.857
}
```

### Logs

The proxy logs to stdout/stderr (captured by container logs):

```
discobot-agent: found proxy at /opt/discobot/bin/proxy, starting HTTP proxy...
discobot-agent: proxy started (pid=42), waiting for health check...
discobot-agent: HTTP proxy ready on port 17080
```

During operation:
```
INFO  request    method=GET host=registry-1.docker.io path=/v2/library/ubuntu/blobs/sha256:abc123...
INFO  cache hit  path=/v2/library/ubuntu/blobs/sha256:abc123...
INFO  response   method=GET host=registry-1.docker.io status=200 duration=5ms
```

### Health Check

The proxy API provides a health endpoint:

```bash
curl http://localhost:17081/health
```

Response:
```json
{
  "status": "ok",
  "ca_cert": "/.data/proxy/certs/ca.crt"
}
```

## Docker Registry Caching

### How It Works

1. **First pull**: Docker requests an image layer → Proxy fetches from registry → Caches response → Returns to Docker
2. **Second pull**: Docker requests same layer → Proxy serves from cache (no network request)
3. **Result**: 5-10x faster pulls, reduced bandwidth

### Cache Key

Cache keys are generated from the URL path:
- Key: `host + path` (e.g., `registry-1.docker.io/v2/library/ubuntu/blobs/sha256:abc123...`)
- Storage: SHA256 hash of the key (filesystem-safe)

### What Gets Cached

- ✅ Blob layers (`/v2/.*/blobs/sha256:.*`) - Immutable, content-addressable (standard Docker Registry API v2)
- ✅ Manifests by digest (`/v2/.*/manifests/sha256:.*`) - Immutable (standard Docker Registry API v2)
- ✅ Registry filesystem storage (`/registry-v2/docker/registry/v2/blobs/sha256/`) - Local registry blobs (Docker registry filesystem layout)
- ❌ Manifests by tag (`/v2/.*/manifests/latest`) - Can change over time
- ❌ Failed responses (non-2xx status codes)
- ❌ Responses with `Cache-Control: no-store`

### LRU Eviction

When cache size exceeds `max_size`:
1. Least recently used (LRU) items are evicted
2. Eviction continues until size is below limit
3. Statistics track eviction count

### Cache Headers

Cached responses include:
- `X-Cache: HIT` - Response served from cache
- `X-Cache-Date` - Timestamp when response was cached

## Certificate Management

### CA Certificate Generation

The agent **automatically generates** a CA certificate during container startup (Step 6 of startup flow):

**Generation process:**
- Uses Go's `crypto/x509` and `crypto/rsa` packages to generate a 2048-bit RSA key pair
- Creates a self-signed X.509 certificate valid for 10 years
- Subject: `O=Discobot Proxy, CN=Discobot Proxy CA`
- **SANs (Subject Alternative Names)**: Includes `localhost`, `127.0.0.1`, and `::1` for proper proxy identification
- Location: `/.data/proxy/certs/ca.crt` (public) and `ca.key` (private, mode 0600)

**Certificate reuse:**
- If `/.data/proxy/certs/ca.crt` already exists, it's reused
- This allows certificate persistence across container restarts when `/.data` is mounted as a volume

### Automatic System Trust Installation

The agent **automatically installs** the CA certificate in the system trust store during startup:

**Supported distributions:**

1. **Debian/Ubuntu/Alpine**:
   - Copies certificate to `/usr/local/share/ca-certificates/discobot-proxy-ca.crt`
   - Runs `update-ca-certificates` to update trust store

2. **Fedora/RHEL/CentOS**:
   - Copies certificate to `/etc/pki/ca-trust/source/anchors/discobot-proxy-ca.crt`
   - Runs `update-ca-trust extract` to update trust store

3. **Other distributions**:
   - Logs a warning if no certificate update tool is found
   - Proxy continues to run but HTTPS interception may not work for all clients

**Result:**
- ✅ All processes in the container automatically trust the proxy's CA
- ✅ No certificate warnings or errors for HTTPS traffic
- ✅ Docker, curl, wget, npm, pip, etc. all work seamlessly through the proxy
- ✅ HTTPS interception works transparently

### Trust Store on Host (Optional)

For development, you may want to trust the certificate on the host machine to inspect traffic:

```bash
# Export CA cert from container
docker cp <container>:/.data/proxy/certs/ca.crt ./proxy-ca.crt

# Install on macOS
sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain ./proxy-ca.crt

# Install on Linux (Debian/Ubuntu)
sudo cp ./proxy-ca.crt /usr/local/share/ca-certificates/
sudo update-ca-certificates

# Install on Linux (Fedora/RHEL)
sudo cp ./proxy-ca.crt /etc/pki/ca-trust/source/anchors/
sudo update-ca-trust extract
```

## Troubleshooting

### Proxy Not Starting

Check logs for:
```
discobot-agent: Proxy daemon not started: proxy binary not found at /opt/discobot/bin/proxy
```

**Solution**: Verify the proxy binary is included in the container image at `/opt/discobot/bin/proxy`.

### Proxy Not Ready

```
discobot-agent: proxy did not become ready: timeout waiting for proxy health check
```

**Solution**: Check if port 17081 is available. Increase `proxyStartupTimeout` in agent code if needed.

### Docker Pulls Not Cached

Check cache stats:
```bash
curl http://localhost:17081/api/cache/stats
```

If `hits: 0` and `stores: 0`:
- Verify caching is enabled in config
- Check logs for "cache hit" / "cache miss" messages
- Verify patterns match Docker registry paths

### High Memory Usage

The cache is stored on disk, but active entries are indexed in memory. To reduce memory:
1. Reduce `max_size` in cache config
2. Clear cache: `curl -X DELETE http://localhost:17081/api/cache`
3. Restart container to reset LRU index

## Performance Impact

### Overhead

- **First request** (cache miss): ~5-10ms additional latency
- **Cached request** (cache hit): Disk I/O only (~1-2ms for large files)
- **Memory**: ~1KB per cached entry (for LRU index)
- **CPU**: Negligible (proxy is I/O bound)

### Benefits

- **Docker pulls**: 5-10x faster for cached layers
- **Bandwidth**: 70-90% reduction for repeated pulls
- **Development**: Faster iteration when rebuilding containers

## Implementation Files

### Agent Changes

- `agent/cmd/agent/main.go`:
  - Added proxy startup constants (ports, timeouts)
  - Added `startProxyDaemon()` function
  - Added `waitForProxyReady()` health check
  - Modified `run()` to start proxy before Docker
  - Modified `runAgent()` to accept `proxyCmd`
  - Modified `buildChildEnv()` to set proxy env vars
  - Modified `eventLoop()` to handle proxy cleanup

### Proxy Changes

- `proxy/internal/config/config.go`:
  - Changed default ports from 8080/8081 to 17080/17081
- `proxy/internal/config/config_test.go`:
  - Updated test expectations for new ports
- Documentation:
  - Updated all references to ports in README.md, DOCKER_CACHING.md
  - Updated example configs

## Future Enhancements

Potential improvements:

1. **Cache prewarming** - Pre-populate cache with common base images
2. **Shared cache** - Multiple containers sharing cache via volume mount
3. **npm/PyPI caching** - Add patterns for other package registries
4. **Metrics export** - Prometheus metrics endpoint
5. **Cache TTL** - Expire cached items after configurable duration
6. **Compression** - Compress cached responses to save disk space

## References

- [Proxy README](./proxy/README.md) - Main proxy documentation
- [Docker Caching Guide](./proxy/docs/DOCKER_CACHING.md) - Detailed caching guide
- [Agent Architecture](./agent/docs/ARCHITECTURE.md) - Agent design docs
- [Agent Init Process](./agent/docs/design/init.md) - Init process details
