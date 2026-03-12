# Cache Volume System

This document describes the project-scoped cache volume system for Docker containers.

## Overview

The cache volume system provides persistent, project-scoped cache storage that is shared across all containers in the same project. This improves build times and reduces network bandwidth by persisting common development tool caches.

## Architecture

### Cache Volume Scoping

- **One cache volume per project**: `discobot-cache-{projectID}`
- **Shared across all containers**: Every container in the same project mounts the same cache volume
- **Persistent across container rebuilds**: Cache survives container deletion and recreation

### Directory Structure

Each cache path gets its own subdirectory within the volume to prevent conflicts:

```
discobot-cache-abc123/
├── home/discobot/.npm/
├── home/discobot/.pnpm-store/
├── home/discobot/.cache/pip/
├── home/discobot/go/pkg/mod/
└── ... (other cache directories)
```

### Mount Strategy

The cache system uses a two-stage mounting approach:

1. **Docker Provider (Host)**: Mounts the cache volume at `/.data/cache` inside the container
2. **Agent (Container)**: After setting up the overlay filesystem for `/home/discobot`, bind-mounts individual cache directories from `/.data/cache` on top of the overlay

This approach ensures cache directories don't write to the container's overlay layer, improving performance and reducing disk usage.

## Configuration

### Environment Variable

Cache volumes can be globally disabled:

```bash
CACHE_ENABLED=false  # Default: true
```

### Workspace Configuration

Users can define additional cache paths by creating `.discobot/cache.json` in their workspace repository:

**Location:** `<user-workspace>/.discobot/cache.json` (inside the container at `/home/discobot/workspace/.discobot/cache.json`)

```json
{
  "additionalPaths": [
    "/home/discobot/.custom-cache",
    "/home/discobot/.local/share/my-tool"
  ]
}
```

The agent reads this file during container startup and merges the additional paths with well-known cache directories.

**Security Validation:**
- All paths must be within `/home/discobot/` (not equal to it)
- Paths must be absolute and not contain `..` components
- Invalid paths are logged and ignored
- This prevents malicious workspaces from mounting sensitive system directories

**Permissions:**
- Cache directories are created with mode `0777` (world-writable)
- This allows all users and processes to write to cache directories
- Necessary for tools that may run as different users or in different contexts

## Well-Known Cache Paths

The agent automatically mounts the following well-known cache directories:

### Universal Cache Directory
- `/home/discobot/.cache` - Universal cache directory (includes pip, yarn, go-build, pypoetry, uv, deno, bazel, sccache, git-lfs, and many others)

### Package Managers (not in .cache)
- `/home/discobot/.npm` - npm cache
- `/home/discobot/.pnpm-store` - pnpm store
- `/home/discobot/.yarn` - yarn cache
- `/nix` - Nix store for single-user installs

### Python
- `/home/discobot/.local/share/uv` - uv data directory

### Go
- `/home/discobot/go/pkg/mod` - Go module cache

### Rust / Cargo
- `/home/discobot/.cargo/registry` - Cargo registry
- `/home/discobot/.cargo/git` - Cargo git dependencies

### Ruby
- `/home/discobot/.bundle` - Bundler cache
- `/home/discobot/.gem` - Gem cache

### Java / JVM
- `/home/discobot/.m2/repository` - Maven repository
- `/home/discobot/.gradle/caches` - Gradle caches
- `/home/discobot/.gradle/wrapper` - Gradle wrapper

### .NET
- `/home/discobot/.nuget/packages` - NuGet packages

### PHP
- `/home/discobot/.composer/cache` - Composer cache

### Other Tools
- `/home/discobot/.bun/install/cache` - Bun cache
- `/home/discobot/.docker/buildx` - Docker buildx cache
- `/home/discobot/.ccache` - ccache
- `/home/discobot/.vscode-server` - VS Code Server
- `/home/discobot/.cursor-server` - Cursor Server
- `/home/discobot/.zed_server` - Zed Server

## API Endpoints

### List Cache Volumes

```http
GET /api/projects/{projectId}/cache
```

Returns all cache volumes for the project.

**Response:**
```json
{
  "volumes": [
    {
      "Name": "discobot-cache-abc123",
      "Driver": "local",
      "Mountpoint": "/var/lib/docker/volumes/discobot-cache-abc123/_data",
      "CreatedAt": "2024-01-15T10:30:00Z",
      "Labels": {
        "discobot.project.id": "abc123",
        "discobot.managed": "true",
        "discobot.type": "cache"
      }
    }
  ]
}
```

### Delete Cache Volume

```http
DELETE /api/projects/{projectId}/cache
```

Deletes the cache volume for the project, clearing all cached data.

**Note:** This requires admin or owner role.

## Implementation Details

### Volume Creation (Docker Provider)

Cache volumes are created lazily on first container creation:

1. When creating a container, check if project has a cache volume
2. If not, create volume with labels:
   - `discobot.project.id`: projectID
   - `discobot.managed`: "true"
   - `discobot.type`: "cache"
3. Mount the volume at `/.data/cache` inside the container

```go
// In server/internal/sandbox/docker/provider.go
hostConfig.Mounts = append(hostConfig.Mounts, mount.Mount{
    Type:   mount.TypeVolume,
    Source: "discobot-cache-abc123",
    Target: "/.data/cache",
})
```

### Mount Configuration (Agent)

After the overlay filesystem is mounted, the agent bind-mounts cache directories:

1. Load cache configuration from `/.home/discobot/workspace/.discobot/cache.json`
2. Get all cache paths (well-known + additional from config)
3. For each cache path:
   - Create subdirectory in `/.data/cache` (e.g., `/.data/cache/home/discobot/.npm`)
   - Create target directory in overlay (e.g., `/home/discobot/.npm`)
   - Bind mount source to target using `syscall.Mount()`

```go
// In agent/cmd/agent/main.go
source := filepath.Join("/.data/cache", subDir)
target := "/home/discobot/.npm"
syscall.Mount(source, target, "none", syscall.MS_BIND, "")
```

### Cleanup

Cache volumes are automatically deleted when a project is deleted:

1. `ProjectService.DeleteProject()` calls database deletion
2. After successful DB deletion, calls `provider.RemoveCacheVolume()`
3. Volume is force-removed even if still in use (containers being torn down)

## Performance Impact

### Benefits

- **Faster builds**: Package managers reuse cached downloads
- **Reduced network**: No need to re-download dependencies
- **Better disk usage**: Cache data not in container overlay layer

### Considerations

- **Disk space**: Cache volumes consume disk space (monitor with `docker system df`)
- **Mount overhead**: Each cache path adds one mount (60+ mounts per container)
- **Concurrency**: Multiple containers can safely access the same cache volume

## Future Enhancements

### Cache Size Limits

Consider adding volume size limits to prevent unbounded growth:

```json
{
  "additionalPaths": ["/home/discobot/.custom-cache"],
  "sizeLimit": "10GB"
}
```

### Cache TTL

Add automatic cleanup of unused cache entries:

```json
{
  "cacheTTL": "30d"  // Delete cache entries older than 30 days
}
```

### Per-Tool Configuration

Allow fine-grained control over specific tools:

```json
{
  "tools": {
    "npm": {
      "enabled": true,
      "path": "/home/discobot/.npm"
    },
    "pip": {
      "enabled": false
    }
  }
}
```

## Troubleshooting

### Cache Not Working

1. Check if cache is enabled: `echo $CACHE_ENABLED`
2. Verify volume exists: `docker volume ls | grep discobot-cache`
3. Inspect container mounts: `docker inspect <container-id> | jq '.[].Mounts'`

### Clearing Cache

To clear cache for a project:

```bash
# Via API
curl -X DELETE http://localhost:3001/api/projects/local/cache

# Or manually
docker volume rm discobot-cache-{projectId}
```

### Disk Space Issues

Check cache volume sizes:

```bash
docker system df -v | grep discobot-cache
```

Prune old volumes:

```bash
docker volume prune -f --filter "label=discobot.type=cache"
```
