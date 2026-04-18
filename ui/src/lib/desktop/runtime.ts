import {
	downloadFile as downloadBrowserFile,
	openExternalUrl as openBrowserExternalUrl,
	pickDirectory as pickBrowserDirectory,
	readClipboardText as readBrowserClipboardText,
	writeClipboardText as writeBrowserClipboardText,
} from "$lib/desktop/browser-adapter";
import {
	checkForAppUpdate as checkForTauriAppUpdate,
	closeAppUpdate as closeTauriAppUpdate,
	detectTauriRuntime,
	downloadAppUpdate as downloadTauriAppUpdate,
	getServerConfig as getTauriServerConfig,
	initServerConfig as initTauriServerConfig,
	installAppUpdate as installTauriAppUpdate,
	openExternalUrl as openTauriExternalUrl,
	pickDirectory as pickTauriDirectory,
	readClipboardText as readTauriClipboardText,
	relaunchApp as relaunchTauriApp,
	saveFileToDownloads,
	withCurrentWindow as withCurrentTauriWindow,
	writeClipboardText as writeTauriClipboardText,
} from "$lib/desktop/tauri-adapter";
import type {
	DesktopDownloadEvent,
	DesktopRuntimeKind,
	DesktopServerConfig,
	DesktopUpdateMetadata,
	DesktopWindowCallback,
	DownloadFileOptions,
} from "$lib/desktop/types";

export type {
	DesktopDownloadEvent,
	DesktopRuntimeKind,
	DesktopServerConfig,
	DesktopUpdateMetadata,
	DesktopWindow,
	DownloadFileOptions,
} from "$lib/desktop/types";

function toUint8Array(content: DownloadFileOptions["content"]): Uint8Array {
	if (typeof content === "string") {
		return new TextEncoder().encode(content);
	}
	if (content instanceof Uint8Array) {
		return content;
	}
	return new Uint8Array(content);
}

function unsupportedFeature(feature: string): never {
	throw new Error(`${feature} is not available in this environment`);
}

export function getDesktopRuntimeKind(): DesktopRuntimeKind {
	if (detectTauriRuntime()) {
		return "tauri";
	}
	return "browser";
}

export function isDesktopShell(): boolean {
	return getDesktopRuntimeKind() !== "browser";
}

export async function initDesktopConfig(): Promise<void> {
	if (getDesktopRuntimeKind() === "tauri") {
		await initTauriServerConfig();
	}
}

export function getDesktopServerConfig(): DesktopServerConfig | null {
	if (getDesktopRuntimeKind() === "tauri") {
		return getTauriServerConfig();
	}
	return null;
}

export function getDesktopAuthToken(): string | null {
	return getDesktopServerConfig()?.secret ?? null;
}

export async function downloadFile({
	filename,
	content,
	mimeType = "application/octet-stream",
}: DownloadFileOptions): Promise<void> {
	const bytes = toUint8Array(content);

	if (getDesktopRuntimeKind() === "tauri") {
		const { toast } = await import("svelte-sonner");
		await saveFileToDownloads(filename, bytes);
		toast.success(`${filename} saved to Downloads`);
		return;
	}

	await downloadBrowserFile(filename, bytes, mimeType);
}

export async function readClipboardText(): Promise<string> {
	if (getDesktopRuntimeKind() === "tauri") {
		return readTauriClipboardText();
	}
	return readBrowserClipboardText();
}

export async function writeClipboardText(text: string): Promise<void> {
	if (getDesktopRuntimeKind() === "tauri") {
		await writeTauriClipboardText(text);
		return;
	}
	await writeBrowserClipboardText(text);
}

export async function openUrl(url: string): Promise<void> {
	if (getDesktopRuntimeKind() === "tauri") {
		await openTauriExternalUrl(url);
		return;
	}
	await openBrowserExternalUrl(url);
}

export async function pickDirectory(): Promise<string | null> {
	if (getDesktopRuntimeKind() === "tauri") {
		try {
			return await pickTauriDirectory();
		} catch (error) {
			throw new Error(
				`Failed to open the directory picker: ${error instanceof Error ? error.message : String(error)}`,
				{ cause: error },
			);
		}
	}
	return pickBrowserDirectory();
}

export async function withCurrentDesktopWindow<T>(
	callback: DesktopWindowCallback<T>,
): Promise<T | undefined> {
	if (getDesktopRuntimeKind() === "tauri") {
		return withCurrentTauriWindow(callback);
	}
	return undefined;
}

export async function checkForAppUpdate(
	endpoint?: string | null,
): Promise<DesktopUpdateMetadata | null> {
	if (getDesktopRuntimeKind() === "tauri") {
		return checkForTauriAppUpdate(endpoint);
	}
	return null;
}

export async function downloadAppUpdate(
	rid: number,
	onEvent: (event: DesktopDownloadEvent) => void,
): Promise<number> {
	if (getDesktopRuntimeKind() === "tauri") {
		return downloadTauriAppUpdate(rid, onEvent);
	}
	return unsupportedFeature("App updates");
}

export async function installAppUpdate(
	updateRid: number,
	bytesRid: number,
): Promise<void> {
	if (getDesktopRuntimeKind() === "tauri") {
		await installTauriAppUpdate(updateRid, bytesRid);
		return;
	}
	unsupportedFeature("App updates");
}

export async function closeAppUpdate(
	updateRid?: number | null,
	bytesRid?: number | null,
): Promise<void> {
	if (getDesktopRuntimeKind() === "tauri") {
		await closeTauriAppUpdate(updateRid, bytesRid);
	}
}

export async function relaunchApp(): Promise<void> {
	if (getDesktopRuntimeKind() === "tauri") {
		await relaunchTauriApp();
		return;
	}
	unsupportedFeature("App relaunch");
}
