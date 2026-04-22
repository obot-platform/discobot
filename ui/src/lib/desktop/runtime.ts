import {
	downloadFile as downloadBrowserFile,
	openExternalUrl as openBrowserExternalUrl,
	pickDirectory as pickBrowserDirectory,
	readClipboardText as readBrowserClipboardText,
	writeClipboardText as writeBrowserClipboardText,
} from "$lib/desktop/browser-adapter";
import {
	checkForAppUpdate as checkForElectronAppUpdate,
	closeAppUpdate as closeElectronAppUpdate,
	detectElectronRuntime,
	downloadAppUpdate as downloadElectronAppUpdate,
	downloadFile as downloadElectronFile,
	getServerConfig as getElectronServerConfig,
	initServerConfig as initElectronServerConfig,
	installAppUpdate as installElectronAppUpdate,
	openExternalUrl as openElectronExternalUrl,
	pickDirectory as pickElectronDirectory,
	readClipboardText as readElectronClipboardText,
	relaunchApp as relaunchElectronApp,
	withCurrentWindow as withCurrentElectronWindow,
	writeClipboardText as writeElectronClipboardText,
} from "$lib/desktop/electron-adapter";
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
	if (detectElectronRuntime()) {
		return "electron";
	}
	return "browser";
}

export function isDesktopShell(): boolean {
	return getDesktopRuntimeKind() !== "browser";
}

export function supportsNativeWindowControls(): boolean {
	return isDesktopShell();
}

export function supportsAppUpdates(): boolean {
	return getDesktopRuntimeKind() !== "browser";
}

export async function initDesktopConfig(): Promise<void> {
	switch (getDesktopRuntimeKind()) {
		case "tauri":
			await initTauriServerConfig();
			return;
		case "electron":
			await initElectronServerConfig();
			return;
		default:
			return;
	}
}

export function getDesktopServerConfig(): DesktopServerConfig | null {
	switch (getDesktopRuntimeKind()) {
		case "tauri":
			return getTauriServerConfig();
		case "electron":
			return getElectronServerConfig();
		default:
			return null;
	}
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

	switch (getDesktopRuntimeKind()) {
		case "tauri": {
			const { toast } = await import("svelte-sonner");
			await saveFileToDownloads(filename, bytes);
			toast.success(`${filename} saved to Downloads`);
			return;
		}
		case "electron": {
			const { toast } = await import("svelte-sonner");
			await downloadElectronFile(filename, bytes);
			toast.success(`${filename} saved to Downloads`);
			return;
		}
		default:
			await downloadBrowserFile(filename, bytes, mimeType);
			return;
	}
}

export async function readClipboardText(): Promise<string> {
	switch (getDesktopRuntimeKind()) {
		case "tauri":
			return readTauriClipboardText();
		case "electron":
			return readElectronClipboardText();
		default:
			return readBrowserClipboardText();
	}
}

export async function writeClipboardText(text: string): Promise<void> {
	switch (getDesktopRuntimeKind()) {
		case "tauri":
			await writeTauriClipboardText(text);
			return;
		case "electron":
			await writeElectronClipboardText(text);
			return;
		default:
			await writeBrowserClipboardText(text);
			return;
	}
}

export async function openUrl(url: string): Promise<void> {
	switch (getDesktopRuntimeKind()) {
		case "tauri":
			await openTauriExternalUrl(url);
			return;
		case "electron":
			await openElectronExternalUrl(url);
			return;
		default:
			await openBrowserExternalUrl(url);
			return;
	}
}

export async function pickDirectory(): Promise<string | null> {
	switch (getDesktopRuntimeKind()) {
		case "tauri":
			try {
				return await pickTauriDirectory();
			} catch (error) {
				throw new Error(
					`Failed to open the directory picker: ${error instanceof Error ? error.message : String(error)}`,
					{ cause: error },
				);
			}
		case "electron":
			return pickElectronDirectory();
		default:
			return pickBrowserDirectory();
	}
}

export async function withCurrentDesktopWindow<T>(
	callback: DesktopWindowCallback<T>,
): Promise<T | undefined> {
	switch (getDesktopRuntimeKind()) {
		case "tauri":
			return withCurrentTauriWindow(callback);
		case "electron":
			return withCurrentElectronWindow(callback);
		default:
			return undefined;
	}
}

export async function checkForAppUpdate(
	endpoint?: string | null,
): Promise<DesktopUpdateMetadata | null> {
	switch (getDesktopRuntimeKind()) {
		case "tauri":
			return checkForTauriAppUpdate(endpoint);
		case "electron":
			return checkForElectronAppUpdate(endpoint);
		default:
			return null;
	}
}

export async function downloadAppUpdate(
	rid: number,
	onEvent: (event: DesktopDownloadEvent) => void,
): Promise<number> {
	switch (getDesktopRuntimeKind()) {
		case "tauri":
			return downloadTauriAppUpdate(rid, onEvent);
		case "electron":
			return downloadElectronAppUpdate(rid, onEvent);
		default:
			return unsupportedFeature("App updates");
	}
}

export async function installAppUpdate(
	updateRid: number,
	bytesRid: number,
): Promise<void> {
	switch (getDesktopRuntimeKind()) {
		case "tauri":
			await installTauriAppUpdate(updateRid, bytesRid);
			return;
		case "electron":
			await installElectronAppUpdate(updateRid, bytesRid);
			return;
		default:
			unsupportedFeature("App updates");
	}
}

export async function closeAppUpdate(
	updateRid?: number | null,
	bytesRid?: number | null,
): Promise<void> {
	switch (getDesktopRuntimeKind()) {
		case "tauri":
			await closeTauriAppUpdate(updateRid, bytesRid);
			return;
		case "electron":
			await closeElectronAppUpdate(updateRid, bytesRid);
			return;
		default:
			return;
	}
}

export async function relaunchApp(): Promise<void> {
	switch (getDesktopRuntimeKind()) {
		case "tauri":
			await relaunchTauriApp();
			return;
		case "electron":
			await relaunchElectronApp();
			return;
		default:
			unsupportedFeature("App relaunch");
	}
}
