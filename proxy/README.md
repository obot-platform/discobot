# Discobot Proxy

A multi-protocol proxy server with HTTP interception, header injection, and dynamic configuration.

## Overview

The proxy provides:
- HTTP/HTTPS proxy with MITM for traffic inspection, header injection, and WebSocket upgrades
- SOCKS5 proxy for non-HTTP TCP tunneling
- Protocol auto-detection on a single port
- Domain-based header injection rules
- Dynamic configuration via file watching and REST API
- Request logging for all proxied traffic
- Response caching with LRU eviction (perfect for Docker registry pulls)

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Proxy Server                                 │
│                                                                      │
│  ┌────────────────────────────────────────────────────────────────┐ │
│  │                   Protocol Detector                             │ │
│  │              (first-byte sniffing on :17080)                     │ │
│  └────────────────────────────────────────────────────────────────┘ │
│                    │                          │                      │
│           HTTP (GET/POST/...)           SOCKS5 (0x05)               │
│                    │                          │                      │
│                    ▼                          ▼                      │
│  ┌─────────────────────────┐    ┌─────────────────────────────────┐ │
│  │     HTTP Proxy          │    │        SOCKS5 Proxy             │ │
│  │     (goproxy)           │    │     (things-go/go-socks5)       │ │
│  │                         │    │                                 │ │
│  │  ┌───────────────────┐  │    │  ┌───────────────────────────┐  │ │
│  │  │   MITM Handler    │  │    │  │   Rule-based Filtering    │  │ │
│  │  │  (TLS intercept)  │  │    │  │   (DNS/IP allowlist)      │  │ │
│  │  └───────────────────┘  │    │  └───────────────────────────┘  │ │
│  │           │             │    │               │                 │ │
│  │           ▼             │    │               ▼                 │ │
│  │  ┌───────────────────┐  │    │  ┌───────────────────────────┐  │ │
│  │  │  Header Injector  │  │    │  │   Connection Tunneling    │  │ │
│  │  │  (per-domain)     │  │    │  │   (TCP passthrough)       │  │ │
│  │  └───────────────────┘  │    │  └───────────────────────────┘  │ │
│  └─────────────────────────┘    └─────────────────────────────────┘ │
│                    │                          │                      │
│                    └──────────┬───────────────┘                      │
│                               ▼                                      │
│                    ┌───────────────────┐                            │
│                    │   Request Logger  │                            │
│                    └───────────────────┘                            │
└─────────────────────────────────────────────────────────────────────┘
                               │
         ┌─────────────────────┼─────────────────────┐
         ▼                     ▼                     ▼
┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐
│  Config Watcher │  │   REST API      │  │  Certificate    │
│  (YAML file)    │  │   (POST only)   │  │  Manager        │
└─────────────────┘  └─────────────────┘  └─────────────────┘
```

## Documentation

- [Architecture Overview](./docs/ARCHITECTURE.md) - System design and data flow
- [Docker Caching Guide](./docs/DOCKER_CACHING.md) - Complete guide to Docker registry caching
- [Config Module](./docs/design/config.md) - Configuration and file watching
- [Proxy Module](./docs/design/proxy.md) - HTTP and SOCKS5 proxy implementation
- [Injector Module](./docs/design/injector.md) - Header injection logic
- [API Module](./docs/design/api.md) - REST API for configuration

## Getting Started

### Prerequisites

- Go 1.23+

### Development

```bash
# Run with auto-reload
cd proxy
air

# Or run directly
go run cmd/proxy/main.go

# Run tests
go test ./...

# Run linter
golangci-lint run
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PROXY_PORT` | `17080` | Main proxy port (HTTP + SOCKS5) |
| `API_PORT` | `17081` | REST API port |
| `CONFIG_FILE` | `config.yaml` | Path to configuration file |
| `CERT_DIR` | `./certs` | Directory for CA certificate |
| `LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `LOG_FORMAT` | `text` | Log format (text, json) |

### Building

```bash
go build -o discobot-proxy ./cmd/proxy
```

### Configuration File

```yaml
# config.yaml
proxy:
  port: 17080
  api_port: 17081

# DNS/IP allowlist (empty = allow all)
allowlist:
  domains:
    - "*.github.com"
    - "api.anthropic.com"
    - "*.openai.com"
  ips:
    - "192.168.1.0/24"

# Header injection rules (domain -> header rules)
# Each rule has "set" (replace) and/or "append" sections
# Optional "conditions" restrict when headers are applied
headers:
  "api.anthropic.com":
    set:
      "X-Custom-Header": "value1"
  "*.openai.com":
    # Conditions: ALL must be true for headers to apply
    conditions:
      - header: "X-Environment"
        equals: "production"
    set:
      "X-Request-Source": "discobot-proxy"
    append:
      "X-Forwarded-For": "proxy.internal"

# Response caching (perfect for Docker registry pulls)
cache:
  enabled: true
  dir: ./cache
  max_size: 21474836480  # 20GB in bytes
  patterns:
    # Default patterns for Docker registry (if not specified):
    - "^/v2/.*/blobs/sha256:.*"      # Docker blob layers
    - "^/v2/.*/manifests/sha256:.*"  # Docker manifests by digest
    # Custom patterns can be added:
    # - "^/npm/@.*/-/.*\\.tgz$"       # npm packages

logging:
  level: info
  format: text
  file: ""  # Empty = stdout
```

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/config` | Overwrite entire running config |
| PATCH | `/api/config` | Merge partial config into running config |
| GET | `/api/cache/stats` | Get cache statistics |
| DELETE | `/api/cache` | Clear all cached content |
| GET | `/health` | Health check |

### POST /api/config - Overwrite

Completely replaces the running configuration:

```bash
curl -X POST http://localhost:17081/api/config \
  -H "Content-Type: application/json" \
  -d '{
    "allowlist": {
      "enabled": true,
      "domains": ["*.github.com", "api.anthropic.com"],
      "ips": ["10.0.0.0/8"]
    },
    "headers": {
      "api.anthropic.com": {
        "set": {"Authorization": "Bearer sk-ant-xxx"}
      },
      "*.github.com": {
        "conditions": [
          {"header": "X-Auth-Type", "equals": "enterprise"}
        ],
        "set": {"Authorization": "token ghp_xxx"},
        "append": {"X-Forwarded-For": "proxy.internal"}
      }
    }
  }'
```

### PATCH /api/config - Merge

Merges into existing config. Set a domain to `null` to delete:

```bash
# Add headers for a new domain (existing domains unchanged)
curl -X PATCH http://localhost:17081/api/config \
  -d '{"headers": {"api.openai.com": {"set": {"Authorization": "Bearer sk-xxx"}}}}'

# Add conditional headers (applied only when X-Environment equals "prod")
curl -X PATCH http://localhost:17081/api/config \
  -d '{"headers": {"api.example.com": {"conditions": [{"header": "X-Environment", "equals": "prod"}], "set": {"Authorization": "Bearer prod-token"}}}}'

# Add append-style headers
curl -X PATCH http://localhost:17081/api/config \
  -d '{"headers": {"*": {"append": {"Via": "1.1 discobot-proxy"}}}}'

# Delete a domain's headers
curl -X PATCH http://localhost:17081/api/config \
  -d '{"headers": {"api.openai.com": null}}'
```

Response:
```json
{"status": "ok"}
```

### GET /api/cache/stats - Cache Statistics

Returns current cache statistics:

```bash
curl http://localhost:17081/api/cache/stats
```

Response:
```json
{
  "hits": 42,
  "misses": 8,
  "stores": 8,
  "evictions": 0,
  "errors": 0,
  "current_size": 5368709120,
  "hit_rate": 0.84
}
```

### DELETE /api/cache - Clear Cache

Clears all cached content:

```bash
curl -X DELETE http://localhost:17081/api/cache
```

Response:
```json
{"status": "ok"}
```

## Project Structure

```
proxy/
├── cmd/proxy/
│   └── main.go              # Application entry point
├── internal/
│   ├── config/              # Configuration management
│   │   ├── config.go        # Config types and loading
│   │   └── watcher.go       # File watcher for hot reload
│   ├── proxy/               # Proxy implementations
│   │   ├── server.go        # Main server with protocol detection
│   │   ├── http.go          # HTTP/HTTPS proxy (goproxy)
│   │   ├── socks.go         # SOCKS5 proxy (go-socks5)
│   │   └── detector.go      # Protocol detection
│   ├── injector/            # Header injection
│   │   ├── injector.go      # Header injection logic
│   │   └── matcher.go       # Domain pattern matching
│   ├── cert/                # Certificate management
│   │   └── manager.go       # CA cert generation and storage
│   ├── api/                 # REST API
│   │   ├── server.go        # API server
│   │   └── handlers.go      # API handlers
│   ├── logger/              # Request logging
│   │   └── logger.go        # Structured logging
│   └── filter/              # Connection filtering
│       └── filter.go        # DNS/IP allowlist
├── docs/
│   ├── ARCHITECTURE.md
│   └── design/
│       ├── config.md
│       ├── proxy.md
│       ├── injector.md
│       └── api.md
├── go.mod
├── go.sum
└── config.yaml              # Example configuration
```

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/elazarl/goproxy` | HTTP/HTTPS proxy with MITM |
| `github.com/things-go/go-socks5` | SOCKS5 server |
| `github.com/fsnotify/fsnotify` | File watching for config |
| `github.com/go-chi/chi/v5` | HTTP routing for API |
| `gopkg.in/yaml.v3` | YAML configuration parsing |
| `go.uber.org/zap` | Structured logging |

## Certificate Installation

For HTTPS interception, the proxy generates a CA certificate on first run. Install it in your system/browser trust store:

```bash
# Certificate is saved to:
# ./certs/ca.crt (public cert - install this)
# ./certs/ca.key (private key - keep secure)

# macOS
sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain ./certs/ca.crt

# Linux (Ubuntu/Debian)
sudo cp ./certs/ca.crt /usr/local/share/ca-certificates/discobot-proxy.crt
sudo update-ca-certificates

# Windows (PowerShell as Admin)
Import-Certificate -FilePath .\certs\ca.crt -CertStoreLocation Cert:\LocalMachine\Root
```

## Usage

### As HTTP Proxy

```bash
# Set environment variables
export HTTP_PROXY=http://localhost:17080
export HTTPS_PROXY=http://localhost:17080

# Or per-command
curl --proxy http://localhost:17080 https://api.anthropic.com/v1/messages
```

### As SOCKS5 Proxy

```bash
# Set environment variable
export ALL_PROXY=socks5://localhost:17080

# Or per-command
curl --socks5 localhost:17080 https://example.com
```

### Docker Registry Caching

The proxy automatically caches Docker registry pulls when caching is enabled. This dramatically speeds up repeated pulls of the same images:

```bash
# Configure Docker to use the proxy
export HTTP_PROXY=http://localhost:17080
export HTTPS_PROXY=http://localhost:17080

# Or configure in Docker daemon.json
{
  "proxies": {
    "default": {
      "httpProxy": "http://localhost:17080",
      "httpsProxy": "http://localhost:17080"
    }
  }
}

# First pull - downloads from registry and caches
docker pull ubuntu:22.04
# Subsequent pulls - served from cache (much faster!)
docker pull ubuntu:22.04
```

Cache benefits:
- **Content-addressable**: Layers are cached by SHA256 digest (immutable)
- **Efficient storage**: Only unique layers are stored
- **LRU eviction**: Automatically manages cache size
- **Multi-image support**: Shared layers between images are cached once
- **Streaming cache misses**: First-time downloads are forwarded to the client immediately while they are written into the cache

## Testing

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run specific package
go test ./internal/injector/...

# Run with race detection
go test -race ./...
```

## License

MIT
