import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";

const ELECTRON_MAIN_MODULE = path.resolve(
	import.meta.dirname,
	"../../../../../electron/main.ts",
);
const ELECTRON_PRELOAD_MODULE = path.resolve(
	import.meta.dirname,
	"../../../../../electron/preload.ts",
);
const ELECTRON_SERVER_MODULE = path.resolve(
	import.meta.dirname,
	"../../../../../electron/server.ts",
);
const ELECTRON_TRAY_MODULE = path.resolve(
	import.meta.dirname,
	"../../../../../electron/tray.ts",
);
const ELECTRON_UPDATER_MODULE = path.resolve(
	import.meta.dirname,
	"../../../../../electron/updater.ts",
);
const ELECTRON_WINDOW_STATE_MODULE = path.resolve(
	import.meta.dirname,
	"../../../../../electron/window-state.ts",
);
const BUILD_ELECTRON_SCRIPT = path.resolve(
	import.meta.dirname,
	"../../../../../scripts/build-electron.mjs",
);
const ROOT_PACKAGE_JSON = path.resolve(
	import.meta.dirname,
	"../../../../../package.json",
);

function readSource(filePath: string) {
	return readFileSync(filePath, "utf-8");
}

test("electron main bootstraps a single-instance desktop shell", () => {
	const source = readSource(ELECTRON_MAIN_MODULE);

	assert.match(source, /app\.requestSingleInstanceLock\(\)/);
	assert.match(source, /contextIsolation: true/);
	assert.match(source, /nodeIntegration: false/);
	assert.match(source, /sandbox: true/);
	assert.match(source, /protocol\.handle\("app"/);
	assert.match(source, /desktop:init-server-config/);
	assert.match(source, /desktop:window-resized/);
	assert.match(source, /dialog\.showOpenDialog\(currentWindow\(event\), \{/);
	assert.match(
		source,
		/app\.isPackaged\s*\?\s*path\.resolve\(import\.meta\.dirname, "preload\.js"\)\s*:\s*path\.resolve\(import\.meta\.dirname, "\.\.", "\.electron-dist", "preload\.js"\)/,
	);
	assert.match(source, /setupDevContextMenu\(window\)/);
	assert.match(source, /label: "Reload"/);
	assert.match(source, /label: "Inspect Element"/);
	assert.match(source, /inspectElement\(params\.x, params\.y\)/);
	assert.match(source, /configureUpdater\(\)/);
	assert.match(source, /registerUpdaterHandlers\(\)/);
	assert.match(source, /path\.relative\(buildDir, filePath\)/);
	assert.match(source, /contentTypeForAppAsset/);
	assert.match(source, /const asset = await readFile\(filePath\)/);
	assert.match(source, /"content-type": contentTypeForAppAsset\(filePath\)/);
	assert.match(source, /new Response\("Not found", \{ status: 404 \}\)/);
	assert.match(source, /app\.getPath\("downloads"\)/);
	assert.match(source, /path\.basename\(filename\)/);
});

test("electron preload exposes the desktop bridge through the preload API", () => {
	const source = readSource(ELECTRON_PRELOAD_MODULE);

	assert.match(
		source,
		/contextBridge\.exposeInMainWorld\("__DISCOBOT_DESKTOP__"/,
	);
	assert.match(source, /kind: "electron"/);
	assert.match(source, /desktop:window-minimize/);
	assert.match(
		source,
		/windowMinimize: \(\) => ipcRenderer\.invoke\("desktop:window-minimize"\)/,
	);
	assert.match(
		source,
		/windowMaximize: \(\) => ipcRenderer\.invoke\("desktop:window-maximize"\)/,
	);
	assert.match(
		source,
		/windowClose: \(\) => ipcRenderer\.invoke\("desktop:window-close"\)/,
	);
	assert.match(source, /desktop:init-server-config/);
	assert.match(source, /desktop:update-download/);
});

test("electron server bootstrap mirrors the desktop sidecar contract", () => {
	const source = readSource(ELECTRON_SERVER_MODULE);

	assert.match(source, /DISCOBOT_DESKTOP_RUNTIME: "electron"/);
	assert.match(source, /DISCOBOT_DESKTOP_SECRET: state\.secret/);
	assert.match(source, /STDIN_KEEPALIVE: "true"/);
	assert.match(source, /app:\/\/discobot/);
	assert.match(source, /http:\/\/localhost:3100/);
});

test("electron tray scaffolding keeps close-to-tray behavior in one module", () => {
	const source = readSource(ELECTRON_TRAY_MODULE);

	assert.match(source, /new Tray/);
	assert.match(source, /"tray-icon\.png"/);
	assert.match(source, /icon\.resize\(\{ width: 18, height: 18 \}\)/);
	assert.match(source, /resized\.setTemplateImage\(true\)/);
	assert.match(source, /icon\.resize\(\{ width: 22, height: 22 \}\)/);
	assert.match(source, /Show/);
	assert.match(source, /Quit/);
	assert.match(source, /hideMainWindow/);
	assert.match(source, /showMainWindow/);
	assert.match(source, /app\.dock\.show\(\)/);
	assert.match(source, /app\.dock\.hide\(\)/);
});

test("electron updater wiring keeps update IPC in the main process", () => {
	const source = readSource(ELECTRON_UPDATER_MODULE);

	assert.match(source, /autoUpdater/);
	assert.match(source, /desktop:update-check/);
	assert.match(source, /desktop:update-download/);
	assert.match(source, /desktop:update-install/);
	assert.match(source, /desktop:update-close/);
	assert.match(source, /autoUpdater\.on\("download-progress", progress\)/);
});

test("electron window state helper persists and restores bounds", () => {
	const source = readSource(ELECTRON_WINDOW_STATE_MODULE);

	assert.match(source, /window-state\.json/);
	assert.match(source, /loadWindowState/);
	assert.match(source, /trackWindowState/);
	assert.match(source, /restoreWindowState/);
	assert.match(source, /isFullscreen\?: boolean/);
	assert.match(source, /window\.getNormalBounds\(\)/);
	assert.match(source, /window\.on\("enter-full-screen", persist\)/);
	assert.match(source, /window\.setFullScreen\(true\)/);
	assert.match(source, /window\.maximize\(\)/);
});

test("electron build script bundles main and preload entry points", () => {
	const source = readSource(BUILD_ELECTRON_SCRIPT);

	assert.match(source, /entryPoints/);
	assert.match(source, /"electron", "main\.ts"/);
	assert.match(source, /"electron", "preload\.ts"/);
	assert.match(source, /outdir: outputDir/);
	assert.match(source, /format: "esm"/);
	assert.match(source, /format: "cjs"/);
	assert.match(source, /external: \["electron", "electron-updater"\]/);
});

test("root package defines Electron build and distribution scripts", () => {
	const source = readSource(ROOT_PACKAGE_JSON);

	assert.match(source, /"build:app:electron"/);
	assert.match(source, /"dist:app:electron"/);
	assert.match(
		source,
		/"build:app:electron": "pnpm build:frontend && pnpm build:server && node scripts\/build-electron\.mjs && electron-builder --dir"/,
	);
	assert.match(
		source,
		/"dist:app:electron": "pnpm build:frontend && pnpm build:server && node scripts\/build-electron\.mjs && electron-builder"/,
	);
	assert.match(source, /"electron-builder"/);
	assert.match(source, /"electron-updater"/);
	assert.match(source, /"main": "\.electron-dist\/main\.js"/);
});
