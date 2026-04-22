# Electron Port Investigation and Dual-Runtime Plan

_Last reviewed: 2026-04-18_

## Electron release baseline

This investigation assumes the current stable Electron target is **v41.2.1** (published 2026-04-16), which ships with **Chromium 146.0.7680.188** and **Node.js 24.14.1**. Electron's documented support policy is to support the **latest three stable major versions**, which currently means the 41.x, 40.x, and 39.x lines.

For Discobot, that means Electron itself is not the limiting factor. The bigger porting work is in the desktop shell layer: sidecar bootstrapping, window/tray behavior, updater/signing, and macOS entitlements.

## Executive summary

Discobot is not deeply coupled to Tauri at the product-logic level. Most of the application lives in:

- the Go server
- the Svelte UI
- the shared API contracts between them

The Tauri-specific surface area is real, but it is concentrated in a relatively small set of places:

1. the Rust desktop shell under `src-tauri/`
2. a handful of UI adapters that import `@tauri-apps/*`
3. startup/auth plumbing for the bundled local Go server
4. system tray, window management, and updater behavior
5. build/release/signing infrastructure

That makes an in-place dual-runtime approach feasible.

The recommended approach is:

- keep the current Tauri app working
- add a Discobot-owned desktop runtime abstraction
- implement that abstraction for **Tauri**, **Electron**, and **browser**
- move renderer code off direct `@tauri-apps/*` imports
- add Electron as a second shell implementation instead of replacing Tauri in one step

## Current Tauri-specific inventory

### 1. Tooling, scripts, and release pipeline

| Area                 | Current Tauri-specific detail                                                                            | Evidence                                                                                           |
| -------------------- | -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| Root scripts         | `pnpm dev` and `pnpm build` assume the desktop shell is Tauri today.                                     | `package.json:7-10`, `package.json:23-26`, `package.json:55-59`                                    |
| Root dependency      | The repo depends on the Tauri CLI.                                                                       | `package.json:58-64`                                                                               |
| UI dependencies      | The renderer imports `@tauri-apps/api` plus clipboard, dialog, opener, process, and updater plugins.     | `ui/package.json:26-31`                                                                            |
| Server build staging | The Go server build script emits binaries into `src-tauri/binaries` and defaults Sentry dist to `tauri`. | `scripts/build-server.mjs:12`, `scripts/build-server.mjs:42-49`, `scripts/build-server.mjs:57-145` |
| VZ asset staging     | The VZ extraction script writes resources into `src-tauri/resources` for bundling into the macOS app.    | `scripts/extract-vz-image.mjs:3-8`, `scripts/extract-vz-image.mjs:21-30`                           |
| Release workflow     | Releases are built with `tauri-apps/tauri-action`, include updater JSON, and use Tauri signing secrets.  | `.github/workflows/release.yml:122-247`                                                            |
| Release metadata     | Release builds set `PUBLIC_SENTRY_DIST=tauri`.                                                           | `.github/workflows/release.yml:217-224`, `scripts/build-server.mjs:46`                             |

### 2. Tauri shell runtime under `src-tauri/`

| Area                     | Current Tauri-specific detail                                                                                          | Evidence                                                   |
| ------------------------ | ---------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------- |
| Rust shell entrypoint    | The desktop app is a Rust/Tauri program with a Windows release subsystem tweak.                                        | `src-tauri/src/main.rs:1-5`                                |
| Tauri builder            | The app is assembled through `tauri::Builder` with setup hooks, window events, managed state, and invoke handlers.     | `src-tauri/src/lib.rs:16-52`                               |
| Build integration        | Rust build integration is handled by `tauri-build`.                                                                    | `src-tauri/build.rs:1-2`, `src-tauri/Cargo.toml:12-17`     |
| Tauri plugins            | The shell registers clipboard, dialog, opener, os, shell, process, updater, single-instance, and window-state plugins. | `src-tauri/src/lib.rs:18-33`, `src-tauri/Cargo.toml:15-27` |
| Window state persistence | Window state is persisted through `tauri-plugin-window-state`, excluding decorations.                                  | `src-tauri/src/lib.rs:11-14`, `src-tauri/src/lib.rs:29-33` |
| Mobile entrypoint        | The Rust shell still carries a Tauri mobile entrypoint attribute.                                                      | `src-tauri/src/lib.rs:16`                                  |
| Capability model         | Tauri capabilities define which windows, URLs, and plugin permissions are allowed.                                     | `src-tauri/capabilities/default.json:1-52`                 |

### 3. App configuration, packaging, and shell resources

| Area                      | Current Tauri-specific detail                                                                  | Evidence                                                              |
| ------------------------- | ---------------------------------------------------------------------------------------------- | --------------------------------------------------------------------- |
| Base app config           | The app uses `tauri.conf.json` as its primary shell config.                                    | `src-tauri/tauri.conf.json:1-53`                                      |
| Frameless desktop window  | The default main window disables decorations and relies on custom chrome.                      | `src-tauri/tauri.conf.json:12-25`                                     |
| macOS overlay titlebar    | macOS overrides use decorated windows with overlay traffic lights and bundled VZ resources.    | `src-tauri/tauri.macos.conf.json:1-20`                                |
| Sidecar packaging         | The Go server is bundled as a Tauri `externalBin`.                                             | `src-tauri/tauri.conf.json:27-43`                                     |
| Updater artifacts         | Tauri is configured to produce updater artifacts and consume `latest.json`.                    | `src-tauri/tauri.conf.json:27-52`                                     |
| App icons                 | Tauri-specific app, tray, Windows, Android, and iOS icon assets live under `src-tauri/icons/`. | `src-tauri/icons/*`                                                   |
| Tray icon asset           | The tray uses the dedicated packaged icon.                                                     | `src-tauri/src/tray.rs:65-68`                                         |
| macOS Info.plist          | Tauri packaging injects macOS transport policy overrides.                                      | `src-tauri/Info.plist:1-13`                                           |
| macOS entitlements        | Tauri packaging carries the macOS JIT/network/file/automation/virtualization entitlements.     | `src-tauri/entitlements.plist:1-22`                                   |
| Existing entitlements doc | Current signing documentation is explicitly written around the Tauri wrapper app.              | `docs/MACOS_ENTITLEMENTS.md:7-17`, `docs/MACOS_ENTITLEMENTS.md:36-73` |

### 4. Bundled Go server bootstrap and desktop auth

This is the most important Tauri-specific integration because it is the part Electron must reproduce.

| Area                         | Current Tauri-specific detail                                                                                               | Evidence                                                                                                                   |
| ---------------------------- | --------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| Sidecar startup              | In production, Tauri picks a random local port, generates a shared secret, and starts the bundled Go server as a sidecar.   | `src-tauri/src/server.rs:17-56`, `src-tauri/src/server.rs:74-130`, `src-tauri/src/server.rs:141-176`                       |
| Dev/prod split               | In debug builds, the Tauri shell does not start the sidecar and instead assumes the backend is already on `localhost:3001`. | `src-tauri/src/server.rs:133-156`                                                                                          |
| Tauri-specific env vars      | The sidecar receives `TAURI=true`, `DISCOBOT_SECRET`, `STDIN_KEEPALIVE`, and a Tauri-specific CORS origin list.             | `src-tauri/src/server.rs:87-94`                                                                                            |
| macOS VZ resource handoff    | The shell looks up packaged `vz/` resources and passes them to the server with env vars.                                    | `src-tauri/src/server.rs:96-120`                                                                                           |
| Renderer startup bridge      | The UI asks Tauri for the server port and secret through custom commands before app startup continues.                      | `src-tauri/src/commands.rs:7-15`, `ui/src/lib/api-config.ts:18-47`, `ui/src/lib/components/app/StartupGate.svelte:353-377` |
| Renderer auth token handling | The UI appends the secret to fetch/WebSocket URLs when running in Tauri.                                                    | `ui/src/lib/api-config.ts:123-141`, `ui/src/lib/api-client.ts:120-139`                                                     |
| Server config                | The Go server has an explicit Tauri mode and Tauri secret.                                                                  | `server/internal/config/config.go:284-289`                                                                                 |
| Server middleware            | The whole HTTP server installs `TauriAuth`, which checks the secret from `?token=` or a cookie.                             | `server/cmd/server/main.go:407-409`, `server/internal/middleware/auth.go:26-82`                                            |
| Diagnostic exposure          | Support info and API types surface whether the server is in `tauri_mode`.                                                   | `server/internal/handler/status.go:72-85`, `server/internal/handler/status.go:134-147`, `ui/src/lib/api-types.ts:778-786`  |

### 5. Native desktop capabilities used by the renderer

The current UI does not use Tauri everywhere. It routes desktop-only behavior through a small number of adapter points. That is good news for an Electron port.

| Capability                      | Current implementation                                                                                                | Evidence                                                                                                                                                                                               |
| ------------------------------- | --------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Save file directly to Downloads | Custom Rust command `save_file_to_downloads`, exposed through the runtime-neutral shell facade and the Tauri adapter. | `src-tauri/src/commands.rs:17-26`, `ui/src/lib/shell.ts:1-21`, `ui/src/lib/desktop/tauri-adapter.ts:35-43`                                                                                             |
| Read/write clipboard            | Tauri clipboard plugin behind the shell facade.                                                                       | `ui/src/lib/shell.ts:1-21`, `ui/src/lib/desktop/tauri-adapter.ts:46-54`                                                                                                                                |
| Open external URLs / IDE links  | Tauri opener plugin behind the shell facade.                                                                          | `ui/src/lib/shell.ts:1-21`, `ui/src/lib/desktop/tauri-adapter.ts:56-59`                                                                                                                                |
| Pick a local directory          | Tauri dialog plugin behind the shell facade.                                                                          | `ui/src/lib/shell.ts:1-21`, `ui/src/lib/desktop/tauri-adapter.ts:61-67`                                                                                                                                |
| Relaunch after update           | Tauri process plugin behind the shell facade.                                                                         | `ui/src/lib/shell.ts:1-21`, `ui/src/lib/desktop/tauri-adapter.ts:124-127`, `ui/src/lib/app/domains/app-updates.svelte.ts:253-279`                                                                      |
| Window operations               | Window methods flow through the shell facade and into the Tauri adapter.                                              | `ui/src/lib/shell.ts:1-21`, `ui/src/lib/components/app/parts/RightWindowControls.svelte:1-25`, `ui/src/lib/components/app/AppMacWindowSpacer.svelte:1-30`, `ui/src/lib/desktop/tauri-adapter.ts:70-75` |

Representative renderer consumers:

- clipboard: `ui/src/lib/components/app/parts/DesktopPanel.svelte:168-185`, `ui/src/lib/components/app/parts/TerminalPanel.svelte:325-335`, `ui/src/lib/components/app/CredentialsManager.svelte:815-830`
- URL opening: `ui/src/lib/components/app/SessionToolbar.svelte:132-135`, `ui/src/lib/components/app/CredentialsManager.svelte:847-850`, `ui/src/lib/components/app/parts/TerminalPanel.svelte:381-383`, `ui/src/lib/components/app/parts/ServicePanel.svelte:231-233`, `ui/src/lib/components/ai/link-safety-modal/LinkSafetyModal.svelte:29-32`, `ui/src/lib/components/ai/open-in-chat/OpenInProviderItem.svelte:20-22`
- native downloads: `ui/src/lib/markdown/render-dom.ts:402-406`, `ui/src/lib/components/app/SupportInfoDialog.svelte:52-56`, `ui/src/lib/components/app/ConversationHooksPanel.svelte:124-128`, `ui/src/lib/components/app/parts/FilesPanel.svelte:272-279`, `ui/src/lib/components/ai/image-attachment/ImageAttachment.svelte:51-55`
- directory picking: `ui/src/lib/components/app/ConversationWorkspaceSelector.svelte:97-99`, `ui/src/lib/components/app/ConversationWorkspaceSelector.svelte:313-330`, `ui/src/lib/components/app/ConversationWorkspaceSelector.svelte:705-717`

### 6. Window chrome, drag regions, tray, and lifecycle

| Area                        | Current Tauri-specific detail                                                                               | Evidence                                                                                                                                |
| --------------------------- | ----------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| Custom drag regions         | The app uses `data-tauri-drag-region`, `.tauri-drag-region`, and `.tauri-no-drag`.                          | `ui/src/app.css:763-773`, `ui/src/lib/components/app/AppHeader.svelte:40-46`, `ui/src/lib/components/app/SessionToolbar.svelte:252-258` |
| Conditional custom controls | The header only renders the custom right-side window buttons when the app is in Tauri on non-mac platforms. | `ui/src/lib/components/app/AppHeader.svelte:31-37`, `ui/src/lib/components/app/AppHeader.svelte:120-126`                                |
| macOS fullscreen spacer     | The macOS window spacer is only active in Tauri and listens to Tauri resize/fullscreen state.               | `ui/src/lib/components/app/AppMacWindowSpacer.svelte:9-39`                                                                              |
| Tray menu                   | The Rust shell creates a tray icon with Show/Quit and toggles the main window on click.                     | `src-tauri/src/tray.rs:60-90`                                                                                                           |
| Close-to-tray               | Closing the main window prevents app exit and hides to tray instead.                                        | `src-tauri/src/tray.rs:93-97`                                                                                                           |
| Single-instance             | A second app launch focuses the existing window.                                                            | `src-tauri/src/lib.rs:25-28`                                                                                                            |
| macOS activation policy     | The app flips between `Regular` and `Accessory` activation policies as the window is shown/hidden.          | `src-tauri/src/tray.rs:7-29`, `src-tauri/src/tray.rs:44-57`                                                                             |

### 7. Updater integration

| Area                       | Current Tauri-specific detail                                                                 | Evidence                                                                                                                                                                                                                               |
| -------------------------- | --------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Updater config             | The Tauri config points at GitHub `latest.json` and includes the updater public key.          | `src-tauri/tauri.conf.json:45-52`                                                                                                                                                                                                      |
| Custom Rust updater bridge | The Rust shell wraps `tauri-plugin-updater` behind custom commands and a progress channel.    | `src-tauri/src/app_updater.rs:1-154`                                                                                                                                                                                                   |
| Renderer updater domain    | The UI's `AppUpdates` domain is written around Tauri commands plus `plugin-process` relaunch. | `ui/src/lib/app/domains/app-updates.svelte.ts:5-337`                                                                                                                                                                                   |
| Tauri-only settings UI     | The Update tab is only visible when `environment.runtime === "tauri"`.                        | `ui/src/lib/components/app/SettingsDialog.svelte:116`, `ui/src/lib/components/app/SettingsDialog.svelte:149-161`, `ui/src/lib/components/app/SettingsDialog.svelte:202-206`, `ui/src/lib/components/app/SettingsDialog.svelte:427-550` |
| Release outputs            | The release workflow signs Tauri updater artifacts and publishes `latest.json`.               | `.github/workflows/release.yml:224-243`                                                                                                                                                                                                |

### 8. Tauri-only UI/runtime behavior switches

| Area                   | Current Tauri-specific detail                                                                                            | Evidence                                                                                                                                         |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| Runtime detection      | The renderer now detects Tauri in the dedicated Tauri adapter and exposes runtime state through the shared shell facade. | `ui/src/lib/desktop/tauri-adapter.ts:11-31`, `ui/src/lib/shell.ts:1-21`, `ui/src/lib/api-config.ts:1-253`                                        |
| Environment shape      | App environment stores `runtime` plus window control placement.                                                          | `ui/src/lib/app/app-helpers.ts:48-54`, `ui/src/lib/app/domains/app-environment.ts:14-20`, `ui/src/lib/app/app-context.types.ts:138-143`          |
| Local directory picker | The workspace input only exposes a native directory chooser in Tauri.                                                    | `ui/src/lib/components/app/ConversationWorkspaceSelector.svelte:97-99`, `ui/src/lib/components/app/ConversationWorkspaceSelector.svelte:705-717` |
| Desktop update UI      | The settings update tab is suppressed outside Tauri.                                                                     | `ui/src/lib/components/app/SettingsDialog.svelte:116`, `ui/src/lib/components/app/SettingsDialog.svelte:427-550`                                 |
| Startup flow copy      | Startup gallery documentation explicitly describes Tauri config initialization.                                          | `ui/src/routes/gallery/startup/+page.svelte:47-61`                                                                                               |
| Runtime indicator      | The gallery shows “Tauri” vs “Browser”.                                                                                  | `ui/src/routes/gallery/+page.svelte:208-210`                                                                                                     |

### 9. Low-value Tauri-specific items that can probably be cleaned up during the port

| Item                                   | Observation                                                                                                        | Evidence                                                                                                                             |
| -------------------------------------- | ------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------ |
| `tauri-plugin-os`                      | Registered and permitted, but there are no obvious renderer consumers today.                                       | `src-tauri/Cargo.toml:20`, `src-tauri/src/lib.rs:22`, `src-tauri/capabilities/default.json:34`                                       |
| `@tauri-apps/plugin-updater` in the UI | Present as a dependency, but the renderer update flow uses custom Rust commands instead of the JS updater package. | `ui/package.json:31`, `ui/src/lib/app/domains/app-updates.svelte.ts:183-235`, `ui/src/lib/app/domains/app-updates.svelte.ts:302-308` |

## What Electron has to replace

An Electron port does **not** need to reimplement Discobot itself. It needs to replace the current Tauri shell responsibilities:

1. launch and supervise the bundled Go server
2. expose the server port + shared secret to the renderer
3. enforce a narrow desktop bridge for window/system features
4. reproduce tray, close-to-tray, and single-instance behavior
5. reproduce update/relaunch behavior
6. reproduce macOS signing + entitlements for the server binary and shell app

If those are reproduced, most of the existing UI and server can stay as-is.

## Recommended target architecture

### Goal

Support three runtimes with one UI codebase:

- `browser`
- `tauri`
- `electron`

### Core design decision

**Do not make Electron pretend to be Tauri.**

Instead, introduce a Discobot-owned desktop shell API and have both Tauri and Electron implement it.

That keeps the renderer independent from either vendor runtime and avoids baking Tauri command names or plugin imports into the Electron path.

### Proposed renderer-side structure

```text
ui/src/lib/desktop/
  types.ts
  runtime.ts           # runtime detection + singleton access
  browser-adapter.ts   # no-op / web fallback
  tauri-adapter.ts     # wraps current @tauri-apps usage
  electron-adapter.ts  # speaks to window.__DISCOBOT_DESKTOP__
```

Representative interface shape:

```ts
export type DesktopRuntimeKind = "browser" | "tauri" | "electron";

export type DesktopBridge = {
  kind: DesktopRuntimeKind;
  initServer(): Promise<{ port: number; secret: string } | null>;
  window: {
    minimize(): Promise<void>;
    maximize(): Promise<void>;
    unmaximize(): Promise<void>;
    isMaximized(): Promise<boolean>;
    close(): Promise<void>;
    isFullscreen(): Promise<boolean>;
    onResized(listener: () => void): Promise<() => void>;
  };
  system: {
    saveFileToDownloads(filename: string, bytes: Uint8Array): Promise<void>;
    readClipboardText(): Promise<string>;
    writeClipboardText(text: string): Promise<void>;
    openExternal(url: string): Promise<void>;
    pickDirectory(): Promise<string | null>;
    relaunch(): Promise<void>;
  };
  updates: {
    check(options?: { prerelease?: boolean }): Promise<DesktopUpdate | null>;
    download(
      updateId: string,
      onProgress: (event: DownloadEvent) => void,
    ): Promise<void>;
    install(updateId: string): Promise<void>;
    close(updateId: string): Promise<void>;
  };
};
```

The existing `ui/src/lib/shell.ts`, `ui/src/lib/api-config.ts`, and `ui/src/lib/desktop/tauri-adapter.ts` now hold the renderer-side Tauri bridge instead of scattering that logic through feature code.

### Proposed Electron-side structure

```text
electron/
  main.ts              # app lifecycle, BrowserWindow, single instance
  preload.ts           # safe renderer bridge
  server.ts            # sidecar spawn + auth secret
  tray.ts              # tray, close-to-tray, show/hide
  updater.ts           # auto-update bridge
  window-state.ts      # persist bounds/fullscreen/maximized state
```

Security expectations for Electron:

- `contextIsolation: true`
- `nodeIntegration: false`
- `sandbox: true` if compatible with the preload bridge
- all renderer access goes through a narrow preload API such as `window.__DISCOBOT_DESKTOP__`
- the preload surface should only expose the capabilities Discobot already uses in Tauri

### Server/auth generalization

The server logic should stop using Tauri-specific naming even if Tauri remains supported.

Recommended migration path:

- introduce generic config such as `DISCOBOT_DESKTOP_RUNTIME=tauri|electron` and `DISCOBOT_DESKTOP_SECRET`
- keep `TAURI=true` and `DISCOBOT_SECRET` as backward-compatible aliases during migration
- rename `TauriAuth` to something runtime-neutral such as `DesktopShellAuth`
- keep the existing token-on-query-string behavior for WebSocket/SSE because both Tauri and Electron can use it

That lets both shells share one auth model.

### Runtime-specific origin strategy

In development:

- both Tauri and Electron should load the Svelte Vite dev server on `http://localhost:3100`
- both should talk to the dev Go server on `http://localhost:3001`
- neither should spawn the bundled sidecar in dev

In production:

- both should start the bundled Go server sidecar
- both should pass a secret to the renderer
- Electron should use a stable packaged renderer origin (prefer a custom secure app protocol over `file://` so CORS stays explicit)
- the sidecar should allow the Tauri and Electron production origins via `CORS_ORIGINS`

## Progress snapshot

_Current implementation status as of 2026-04-18:_

- Phase 1: substantially complete
  - renderer desktop APIs moved behind `ui/src/lib/desktop/`
  - feature code no longer imports `@tauri-apps/*` directly
  - Tauri imports are centralized in the Tauri adapter
- Phase 2: substantially complete
  - server config/auth naming is desktop-shell-neutral
  - backward-compatible Tauri aliases remain in place
  - runtime-neutral desktop bootstrap fields exist in server/UI contracts
- Phase 3: substantially complete
  - Electron renderer adapter exists in the UI desktop layer
  - `electron/main.ts`, `electron/preload.ts`, `electron/server.ts`, and `electron/tray.ts` now boot a single-instance shell with a secure preload bridge
  - Electron window state now persists across launches
  - packaged Electron builds now force a fresh frontend build before bundling
  - added opt-in `pnpm dev:app:electron`, `pnpm build:app:electron`, and `pnpm dist:app:electron` scripts
- Phase 4: substantially complete
  - renderer-native downloads, clipboard access, external URL opening, directory picker, relaunch, and window/fullscreen events are wired through Electron IPC
  - Electron downloads now match Tauri's direct-to-Downloads behavior instead of prompting with a save dialog
  - validated the native local-directory picker in a live Electron desktop session on Linux
- Phase 5: substantially complete
  - Electron updater IPC now lives in the main process via `electron-updater`
  - prerelease endpoint resolution now supports Electron updater metadata across macOS and Linux (`latest-mac.yml`, `latest-linux.yml`, and `latest.yml` fallback)
  - Electron download progress now listens to the full `download-progress` stream instead of only the first progress event
  - the release workflow now builds and publishes Electron macOS arm64 artifacts, including Electron updater metadata, alongside the existing Tauri release job
- Phase 6+: not started beyond local packaging configuration

## Port plan

### Phase 1: Extract a runtime-neutral renderer bridge

**Goal:** stop importing `@tauri-apps/*` directly from feature code.

Work:

- create `ui/src/lib/desktop/` and move the current Tauri wrapper logic there
- replace `isTauriShell()` with something like `getDesktopRuntimeKind()` / `isDesktopShell()`
- move `initTauriConfig()` into a generic `initDesktopConfig()` flow
- replace direct Tauri window imports in `RightWindowControls.svelte` and `AppMacWindowSpacer.svelte` with the shared bridge
- keep a Tauri implementation under the new abstraction so behavior does not change

Acceptance criteria:

- the browser build still works
- the Tauri app still works
- no app feature code imports `@tauri-apps/*` except the Tauri adapter itself

### Phase 2: Make desktop server bootstrap runtime-neutral

**Goal:** support both Tauri and Electron shells with the same backend contract.

Work:

- rename Tauri-only config/auth concepts to desktop-shell concepts
- keep backward-compatible aliases so the current Tauri app does not break
- generalize sidecar origin handling beyond `tauri.localhost` / `tauri://localhost`
- return runtime-neutral server bootstrap data to the renderer
- add a neutral server status field such as `desktop_runtime` while keeping `tauri_mode` during transition if needed

Acceptance criteria:

- Tauri still boots exactly as before
- Electron can reuse the same port/secret/token contract
- the server no longer assumes the only desktop shell is Tauri

### Phase 3: Add the Electron shell with parity-focused scope

**Goal:** stand up Electron without changing product behavior.

Work:

- add `electron/main.ts`, `electron/preload.ts`, `electron/server.ts`, and `electron/tray.ts`
- create a frameless/non-frameless window configuration that matches the current Tauri shell UX
- implement minimize/maximize/unmaximize/close/fullscreen bridge methods
- implement single-instance behavior
- implement tray behavior and close-to-tray behavior
- persist window state across launches
- in dev, point Electron at the Vite dev server
- in prod, spawn the bundled Go server and expose port/secret through preload

Recommended scope control:

- target **macOS arm64 first**, because that is the only platform currently built in the Tauri release workflow (`.github/workflows/release.yml:126-135`)
- add Windows/Linux parity after the macOS path is solid

Acceptance criteria:

- Electron dev can open the app and talk to the dev server
- Electron prod can launch the bundled Go server and load the full UI
- window chrome/tray behavior is close enough that end-user flows are unchanged

### Phase 4: Port native capability bridges

**Goal:** match the current Tauri feature set used by the renderer.

Work:

- downloads to Downloads folder
- clipboard read/write
- external URL and IDE opener
- native local-directory picker
- relaunch support
- fullscreen/resize events used by the macOS spacer

Acceptance criteria:

- `ui/src/lib/desktop/` supports all currently used capabilities
- the same UI features that work in Tauri also work in Electron
- browser fallback behavior remains unchanged where it already exists

### Phase 5: Port updater/release behavior

**Goal:** support desktop auto-updates in Electron without regressing the Tauri flow.

Recommended implementation:

- use Electron's main process updater stack (for example, `electron-builder` + `electron-updater`)
- keep the UI update domain runtime-neutral and runtime-agnostic
- attach Electron release assets to the same GitHub releases used by Tauri
- keep **separate updater metadata formats** for each shell:
  - Tauri: `latest.json`
  - Electron: `latest.yml` / `latest-mac.yml` or the provider's equivalent
- set Sentry dist by runtime (`tauri` vs `electron`)

Work:

- create an Electron updater bridge that exposes the same high-level operations as the current Tauri update domain
- preserve the current pre-release toggle semantics
- add Electron release jobs alongside Tauri release jobs instead of replacing them immediately

Acceptance criteria:

- Tauri releases continue unchanged
- Electron releases produce signed installable artifacts and their own updater metadata
- the UI's update settings work in both desktop shells

### Phase 6: Port signing, entitlements, and bundled resources

**Goal:** get Electron over the macOS distribution hump, which is the highest-risk part of the port.

Work:

- port the current Tauri macOS entitlements into Electron packaging/signing configuration
- explicitly validate the entitlements on the **Electron app bundle and the bundled Go server binary**
- preserve virtualization access for the server binary
- carry over the VZ resource bundling path used today on macOS
- update `docs/MACOS_ENTITLEMENTS.md` once the Electron path is real

Important note:

The current documentation assumes the Tauri wrapper app is the thing being signed. Electron will need its own signing/notarization path, and the bundled Go server binary should be validated independently instead of assuming Tauri's bundler behavior applies.

Acceptance criteria:

- macOS Electron builds run the VZ-backed flows successfully
- the bundled Go server can still use the virtualization entitlement
- notarized Electron app builds launch cleanly on macOS

### Phase 7: Test, compare, and roll out

**Goal:** ship Electron as an additional supported shell instead of a flag day replacement.

Work:

- add unit tests for the desktop runtime abstraction
- add smoke tests for Electron preload/main-process bridge behavior where practical
- add manual parity checklists for:
  - startup and login
  - tray/show/hide/quit
  - window controls and fullscreen behavior
  - downloads, clipboard, opener, local directory selection
  - updater check/download/install/relaunch
  - VZ/macOS flows
- keep Tauri as the default `pnpm dev` / `pnpm build` target until Electron parity is proven
- add explicit opt-in scripts such as:
  - `pnpm dev:app:tauri`
  - `pnpm dev:app:electron`
  - `pnpm build:app:tauri`
  - `pnpm build:app:electron`

Acceptance criteria:

- both shells can be developed locally
- both shells can be packaged from CI
- the UI codebase stays single-source
- Tauri remains available while Electron stabilizes

## Suggested file/layout changes

A realistic in-place migration would likely look like this:

```text
src-tauri/                     # keep existing Tauri shell
  ...

electron/                      # add Electron shell
  main.ts
  preload.ts
  server.ts
  tray.ts
  updater.ts
  window-state.ts

ui/src/lib/desktop/            # shared runtime abstraction
  types.ts
  runtime.ts
  browser-adapter.ts
  tauri-adapter.ts
  electron-adapter.ts
```

And in the UI:

- `ui/src/lib/shell.ts` is the public facade over `ui/src/lib/desktop/...`
- `ui/src/lib/api-config.ts` becomes runtime-neutral
- `environment.runtime` becomes `"tauri" | "electron" | "browser"`
- `tauri-*` CSS/data attribute names can be aliased first, then renamed to shell-neutral names once both runtimes exist

## Highest-risk items

| Risk                             | Why it matters                                                            | Mitigation                                                                                  |
| -------------------------------- | ------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| macOS virtualization entitlement | The bundled Go server must keep working under Electron packaging/signing. | Validate this early with a tiny Electron shell spike before doing large renderer refactors. |
| Updater divergence               | Tauri and Electron use different updater metadata and signing flows.      | Keep the UI updater API runtime-neutral but use separate runtime-specific release assets.   |
| Renderer security                | Electron makes it easy to overexpose Node/IPC to the renderer.            | Use a tiny preload API, `contextIsolation`, and no direct renderer Node access.             |
| CORS/origin handling             | Tauri currently hardcodes Tauri-specific origins for the local server.    | Define a runtime-neutral origin model before writing Electron production boot.              |
| Shell-specific drift             | Direct feature work could end up duplicated in Tauri and Electron.        | Finish the renderer abstraction before shipping the Electron shell.                         |
| Scope explosion                  | Trying to port all platforms at once will slow the work down.             | Land macOS arm64 parity first, then expand.                                                 |

## Recommended next steps

If this port is approved, the lowest-risk sequence is:

1. build the runtime abstraction with **no behavior change**
2. rename the backend auth/bootstrap concepts from Tauri-specific to desktop-shell-specific
3. create a minimal Electron shell that boots the existing UI in dev
4. add production sidecar boot + tray/window parity
5. add updater + signing + macOS entitlement work last

That sequencing keeps Tauri working throughout and gives the project a clean path to long-term dual support.
