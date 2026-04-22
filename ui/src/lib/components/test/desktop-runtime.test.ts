import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";

const RIGHT_WINDOW_CONTROLS_COMPONENT = path.resolve(
	import.meta.dirname,
	"../app/parts/RightWindowControls.svelte",
);
const WORKSPACE_SELECTOR_COMPONENT = path.resolve(
	import.meta.dirname,
	"../app/ConversationWorkspaceSelector.svelte",
);
const APP_UPDATES_MODULE = path.resolve(
	import.meta.dirname,
	"../../app/domains/app-updates.svelte.ts",
);
const API_CONFIG_MODULE = path.resolve(
	import.meta.dirname,
	"../../api-config.ts",
);
const SHELL_MODULE = path.resolve(import.meta.dirname, "../../shell.ts");
const LEGACY_TAURI_HELPERS_MODULE = path.resolve(
	import.meta.dirname,
	"../../tauri.ts",
);
const LEGACY_ENVIRONMENT_MODULE = path.resolve(
	import.meta.dirname,
	"../../environment.ts",
);
const TAURI_ADAPTER_MODULE = path.resolve(
	import.meta.dirname,
	"../../desktop/tauri-adapter.ts",
);
const ELECTRON_ADAPTER_MODULE = path.resolve(
	import.meta.dirname,
	"../../desktop/electron-adapter.ts",
);

function readSource(filePath: string) {
	return readFileSync(filePath, "utf-8");
}

test("right window controls use the shared desktop window bridge", () => {
	const source = readSource(RIGHT_WINDOW_CONTROLS_COMPONENT);

	assert.match(
		source,
		/import \{ withCurrentDesktopWindow \} from "\$lib\/shell";/,
	);
	assert.match(source, /void withCurrentDesktopWindow\(async \(window\) => \{/);
	assert.doesNotMatch(source, /@tauri-apps\/api\/window/);
});

test("workspace selector uses desktop shell capability for the local directory picker", () => {
	const source = readSource(WORKSPACE_SELECTOR_COMPONENT);

	assert.match(
		source,
		/import \{ isDesktopShell, pickDirectory \} from "\$lib\/shell";/,
	);
	assert.match(
		source,
		/const showLocalDirectoryPicker = \$derived\.by\([\s\S]*isDesktopShell\(\) && workspaceSourceType === "local"/,
	);
	assert.doesNotMatch(source, /getDesktopRuntimeKind\(\) === "tauri"/);
});

test("app updates use the shared desktop runtime helpers", () => {
	const source = readSource(APP_UPDATES_MODULE);

	assert.match(
		source,
		/import \{[\s\S]*checkForAppUpdate,[\s\S]*downloadAppUpdate,[\s\S]*supportsAppUpdates,[\s\S]*installAppUpdate,[\s\S]*relaunchApp,[\s\S]*\} from "\$lib\/shell";/,
	);
	assert.match(
		source,
		/await checkForAppUpdate\(await resolveUpdateEndpoint\(\)\)/,
	);
	assert.match(source, /await downloadAppUpdate\(/);
	assert.match(source, /await installAppUpdate\(/);
	assert.match(source, /await relaunchApp\(\)/);
	assert.match(source, /if \(!supportsAppUpdates\(\)\) \{/);
	assert.match(source, /latest-linux\.yml/);
	assert.match(source, /latest-mac\.yml/);
	assert.doesNotMatch(source, /@tauri-apps/);
	assert.doesNotMatch(source, /getDesktopRuntimeKind\(\) !== "tauri"/);
});

test("api config reads desktop bootstrap state from the shared runtime", () => {
	const source = readSource(API_CONFIG_MODULE);

	assert.match(
		source,
		/import \{[\s\S]*getDesktopAuthToken,[\s\S]*getDesktopServerConfig,[\s\S]*isDesktopShell,[\s\S]*\} from "\$lib\/shell";/,
	);
	assert.match(
		source,
		/const desktopServerConfig = getDesktopServerConfig\(\);/,
	);
	assert.doesNotMatch(source, /get_server_port/);
	assert.doesNotMatch(source, /get_server_secret/);
});

test("shell exports the shared runtime surface", () => {
	const source = readSource(SHELL_MODULE);

	assert.match(
		source,
		/export \{[\s\S]*downloadFile,[\s\S]*openUrl,[\s\S]*pickDirectory,[\s\S]*readClipboardText,[\s\S]*supportsAppUpdates,[\s\S]*supportsNativeWindowControls,[\s\S]*writeClipboardText,[\s\S]*\} from "\$lib\/desktop\/runtime";/,
	);
});

test("runtime supports Tauri, Electron, and browser detection", () => {
	const source = readSource(
		path.resolve(import.meta.dirname, "../../desktop/runtime.ts"),
	);

	assert.match(source, /detectTauriRuntime/);
	assert.match(source, /detectElectronRuntime/);
	assert.match(source, /supportsNativeWindowControls\(\)/);
	assert.match(source, /supportsAppUpdates\(\)/);
	assert.match(source, /return "tauri"/);
	assert.match(source, /return "electron"/);
	assert.match(source, /return "browser"/);
});

test("runtime keeps the download flow consistent across desktop shells", () => {
	const source = readSource(
		path.resolve(import.meta.dirname, "../../desktop/runtime.ts"),
	);

	assert.match(source, /await saveFileToDownloads\(filename, bytes\)/);
	assert.match(source, /await downloadElectronFile\(filename, bytes\)/);
	assert.match(source, /toast\.success\(`\$\{filename\} saved to Downloads`\)/);
});

test("tauri-specific imports are centralized in the tauri adapter", () => {
	const source = readSource(TAURI_ADAPTER_MODULE);

	assert.match(source, /get_desktop_server_port/);
	assert.match(source, /get_desktop_server_secret/);
	assert.match(source, /isTopLevelWindowContext/);
	assert.match(source, /hasInjectedStandaloneConfig/);
	assert.match(source, /!hasInjectedStandaloneConfig\(\)/);
	assert.match(source, /@tauri-apps\/api\/core/);
	assert.match(source, /@tauri-apps\/api\/window/);
	assert.match(source, /@tauri-apps\/plugin-clipboard-manager/);
	assert.match(source, /@tauri-apps\/plugin-dialog/);
	assert.match(source, /@tauri-apps\/plugin-opener/);
	assert.match(source, /@tauri-apps\/plugin-process/);
});

test("electron-specific bridge access is centralized in the electron adapter", () => {
	const source = readSource(ELECTRON_ADAPTER_MODULE);

	assert.match(source, /window\.__DISCOBOT_DESKTOP__/);
	assert.match(source, /requireBridgeMethod/);
	assert.match(source, /kind === "electron"/);
	assert.match(source, /navigator\.userAgent/);
	assert.match(source, /Electron\\\//);
	assert.doesNotMatch(source, /@tauri-apps/);
});

test("legacy tauri and environment helper modules are removed", () => {
	assert.equal(existsSync(LEGACY_TAURI_HELPERS_MODULE), false);
	assert.equal(existsSync(LEGACY_ENVIRONMENT_MODULE), false);
});
