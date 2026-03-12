# Cache Configuration

This document describes how users can configure cache directories in their workspace.

## Overview

Users can configure additional cache directories to be persisted across container rebuilds by creating a `.discobot/cache.json` file in their workspace repository.

## Configuration File

**Location:** `<workspace>/.discobot/cache.json` (inside the user's workspace/repository)

**Format:** JSON

**Example:**
```json
{
  "additionalPaths": [
    "/home/discobot/.cache/custom-tool",
    "/home/discobot/.local/share/my-app"
  ]
}
```

## Security Validation

The agent validates all paths from the user's configuration:

- **Must be within `/home/discobot/`** - Paths outside this directory are ignored
- **Must be absolute** - Relative paths are rejected
- **Must not contain `..`** - Path traversal attempts are blocked
- **Invalid paths are logged** - Users will see warnings in logs

This prevents malicious workspaces from:
- Mounting sensitive system directories (`/etc`, `/root`, `/var`, etc.)
- Path traversal attacks
- Privilege escalation attempts

## Permissions

All cache directories (both well-known and additional) are created with world-writable permissions (0777):

- **Allows all users/processes to write** - Necessary for tools that run as different users
- **Applied to entire directory tree** - Including intermediate directories like `.local/share/`
- **Set explicitly** - Uses `os.Chmod()` to override umask restrictions

## Well-Known Cache Paths

The agent automatically mounts these cache directories without configuration:

### Universal Cache Directory
- `/home/discobot/.cache` - Universal cache directory used by many tools

### Package Managers
- `/home/discobot/.npm` - npm cache
- `/home/discobot/.pnpm-store` - pnpm store
- `/home/discobot/.yarn` - yarn cache
- `/home/discobot/.bun/install/cache` - Bun install cache
- `/nix` - Nix store for single-user installs

### Language-Specific
- **Python**: `.local/share/uv` 
- **Go**: `go/pkg/mod`, `.cache/go-build`
- **Rust**: `.cargo/registry`, `.cargo/git`
- **Ruby**: `.bundle`, `.gem`
- **Java**: `.m2/repository`, `.gradle/caches`, `.gradle/wrapper`
- **.NET**: `.nuget/packages`
- **PHP**: `.composer/cache`

### Other Tools
- `.docker/buildx` - Docker buildx cache
- `.ccache` - ccache
- `.vscode-server` - VS Code Server
- `.cursor-server` - Cursor Server
- `.zed_server` - Zed Server

## Implementation Details

### Loading Configuration

The agent reads the configuration during container startup:

```go
func loadCacheConfig() *cacheConfig {
    configPath := filepath.Join(mountHome, "workspace", ".discobot", "cache.json")
    
    data, err := os.ReadFile(configPath)
    if err != nil {
        // No config file is not an error - return empty config
        return &cacheConfig{}
    }
    
    var cfg cacheConfig
    if err := json.Unmarshal(data, &cfg); err != nil {
        fmt.Printf("discobot-agent: warning: failed to parse cache config: %v\n", err)
        return &cacheConfig{}
    }
    
    return &cfg
}
```

### Path Validation

```go
func isValidCachePath(path string) bool {
    cleanPath := filepath.Clean(path)
    
    // Must be absolute
    if !filepath.IsAbs(cleanPath) {
        return false
    }
    
    // Must be within /home/discobot (not equal to it)
    homePrefix := "/home/discobot/"
    if !strings.HasPrefix(cleanPath+"/", homePrefix) {
        return false
    }
    
    // Must not contain ..
    if strings.Contains(cleanPath, "..") {
        return false
    }
    
    return true
}
```

### Mounting Process

1. Load configuration from workspace
2. Merge with well-known paths
3. Validate all additional paths
4. Create directories with 0777 permissions
5. Bind mount from cache volume

## User Documentation

Users should be instructed to:

1. Create `.discobot/cache.json` in their repository
2. Add paths within `/home/discobot/` only
3. Use absolute paths
4. Check agent logs for validation warnings

## See Also

- [Server Cache Documentation](../../../server/docs/design/cache.md) - Full technical details
- [Agent Architecture](../ARCHITECTURE.md) - Overall agent design
