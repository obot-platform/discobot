import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";

const RIGHT_WINDOW_CONTROLS_COMPONENT = path.resolve(
	import.meta.dirname,
	"../app/parts/RightWindowControls.svelte",
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

test("app updates use the shared desktop runtime helpers", () => {
	const source = readSource(APP_UPDATES_MODULE);

	assert.match(
		source,
		/import \{[\s\S]*checkForAppUpdate,[\s\S]*downloadAppUpdate,[\s\S]*installAppUpdate,[\s\S]*relaunchApp,[\s\S]*\} from "\$lib\/shell";/,
	);
	assert.match(
		source,
		/await checkForAppUpdate\(await resolveUpdateEndpoint\(\)\)/,
	);
	assert.match(source, /await downloadAppUpdate\(/);
	assert.match(source, /await installAppUpdate\(/);
	assert.match(source, /await relaunchApp\(\)/);
	assert.doesNotMatch(source, /@tauri-apps/);
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
		/export \{[\s\S]*downloadFile,[\s\S]*openUrl,[\s\S]*pickDirectory,[\s\S]*readClipboardText,[\s\S]*writeClipboardText,[\s\S]*\} from "\$lib\/desktop\/runtime";/,
	);
});

test("tauri-specific imports are centralized in the tauri adapter", () => {
	const source = readSource(TAURI_ADAPTER_MODULE);

	assert.match(source, /get_desktop_server_port/);
	assert.match(source, /get_desktop_server_secret/);
	assert.match(source, /@tauri-apps\/api\/core/);
	assert.match(source, /@tauri-apps\/api\/window/);
	assert.match(source, /@tauri-apps\/plugin-clipboard-manager/);
	assert.match(source, /@tauri-apps\/plugin-dialog/);
	assert.match(source, /@tauri-apps\/plugin-opener/);
	assert.match(source, /@tauri-apps\/plugin-process/);
});

test("legacy tauri and environment helper modules are removed", () => {
	assert.equal(existsSync(LEGACY_TAURI_HELPERS_MODULE), false);
	assert.equal(existsSync(LEGACY_ENVIRONMENT_MODULE), false);
});
