# macOS Entitlements Setup

This document explains how code signing and entitlements work for Discobot on macOS.

The current release workflow still packages the Tauri app, but the repository now also has a local Electron packaging path. That Electron path reuses the same `src-tauri/entitlements.plist` file for the wrapper app while the broader CI/notarization work is still pending.

## Overview

Discobot requires special entitlements on macOS because:
1. The **Go server** uses Apple's Virtualization framework (requires `com.apple.security.virtualization`)
2. The **Tauri app** needs permissions for JIT compilation, networking, and file access

## Approach

We add the `com.apple.security.virtualization` entitlement to the **Tauri app's** entitlements file. This allows both:
- The Tauri wrapper app to have the entitlement
- The bundled Go server binary to inherit it (transitively)

This is simpler than signing binaries separately and should work for development builds.

## Architecture

```
┌─────────────────────────────────────────────────┐
│  Discobot.app (Tauri wrapper)                   │
│  Signed with: entitlements.plist                │
│  • com.apple.security.virtualization            │
│                                                  │
│  ┌────────────────────────────────────────────┐ │
│  │  discobot-server (Go binary)               │ │
│  │  Inherits entitlements from parent app     │ │
│  └────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────┘
```

## Entitlement File

**Location:** `src-tauri/entitlements.plist`

```xml
<!-- JIT and code execution -->
<key>com.apple.security.cs.allow-jit</key>
<key>com.apple.security.cs.allow-unsigned-executable-memory</key>
<key>com.apple.security.cs.disable-library-validation</key>

<!-- Virtualization (for Go server) -->
<key>com.apple.security.virtualization</key>

<!-- Network access -->
<key>com.apple.security.network.client</key>
<key>com.apple.security.network.server</key>

<!-- File access -->
<key>com.apple.security.files.user-selected.read-write</key>

<!-- Apple Events -->
<key>com.apple.security.automation.apple-events</key>
```

**Critical:** The `com.apple.security.virtualization` entitlement allows the bundled Go server to use Apple's Virtualization framework.

## Build Process

### Standard Build

```bash
pnpm tauri build
```

This runs:
1. `pnpm build:server` - Builds the Go server
2. `pnpm build` - Builds the frontend
3. Tauri bundles everything and signs the app with entitlements

The Go server binary is bundled into the app and inherits the app's entitlements.

## Development vs Production Signing

### Development (Ad-hoc Signing)

For local development, we use **ad-hoc signing** with `-s -`:

```bash
codesign --entitlements file.entitlements --force --sign - binary
```

This is free and works for local testing.

### Production (Developer Certificate)

For distribution (App Store, notarization), you need:
1. Apple Developer account
2. Developer ID Application certificate
3. Replace `-` with your certificate identity:

```bash
codesign --entitlements file.entitlements --sign "Developer ID Application: Your Name" binary
```

## Troubleshooting

### Error: "Invalid virtual machine configuration. The process doesn't have the virtualization entitlement."

**Cause:** The Tauri app doesn't have the virtualization entitlement.

**Solution:**
Verify `src-tauri/entitlements.plist` includes:
```xml
<key>com.apple.security.virtualization</key>
<true/>
```

Then rebuild:
```bash
pnpm tauri build
```

### Verify Entitlements

Check that the built app has the virtualization entitlement:

```bash
# Check the app bundle
codesign -d --entitlements - src-tauri/target/release/bundle/macos/Discobot.app

# Should include:
<key>com.apple.security.virtualization</key>
<true/>
```

## File Locations

```
discobot/
├── src-tauri/
│   ├── entitlements.plist    # App entitlements (includes virtualization)
│   ├── tauri.conf.json       # References entitlements.plist
│   └── binaries/
│       └── discobot-server   # Go server binary (generated during build)
└── server/
    └── cmd/server/main.go    # Go server source
```

## CI/CD Considerations

For automated builds:

1. **Development builds:** Use ad-hoc signing (current setup)
2. **Release builds:**
   - Import Apple Developer certificate
   - Update signing identity in build script
   - Notarize the app bundle

Example for GitHub Actions:
```yaml
- name: Import signing certificate
  env:
    CERTIFICATE_P12: ${{ secrets.APPLE_CERTIFICATE }}
    CERTIFICATE_PASSWORD: ${{ secrets.CERTIFICATE_PASSWORD }}
  run: |
    # Import cert to keychain
    # Update build script to use certificate
```

## References

- [Apple Virtualization Framework Entitlement](https://developer.apple.com/documentation/bundleresources/entitlements/com_apple_security_virtualization)
- [Tauri Code Signing](https://v2.tauri.app/distribute/sign/macos/)
- [macOS Code Signing Guide](https://developer.apple.com/documentation/security/code_signing_services)
