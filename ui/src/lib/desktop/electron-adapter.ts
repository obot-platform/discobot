import type {
	DesktopDownloadEvent,
	DesktopRendererBridge,
	DesktopServerConfig,
	DesktopUpdateMetadata,
	DesktopWindowCallback,
} from "$lib/desktop/types";

let serverConfig: DesktopServerConfig | null = null;

function requireBridgeMethod<T>(
	method: T | undefined,
	message: string,
): NonNullable<T> {
	if (!method) {
		throw new Error(message);
	}
	return method as NonNullable<T>;
}

function getElectronBridge(): DesktopRendererBridge | null {
	if (typeof window === "undefined") {
		return null;
	}
	return window.__DISCOBOT_DESKTOP__ ?? null;
}

export function detectElectronRuntime(): boolean {
	if (getElectronBridge()?.kind === "electron") {
		return true;
	}
	return (
		typeof navigator !== "undefined" && /\bElectron\//.test(navigator.userAgent)
	);
}

export async function initServerConfig(): Promise<void> {
	const bridge = getElectronBridge();
	if (!bridge?.initServerConfig) {
		return;
	}
	serverConfig = await bridge.initServerConfig();
}

export function getServerConfig(): DesktopServerConfig | null {
	return serverConfig;
}

export async function downloadFile(
	filename: string,
	bytes: Uint8Array,
): Promise<string> {
	const bridge = getElectronBridge();
	if (!bridge?.downloadFile) {
		throw new Error("Downloads are not available in this Electron build");
	}
	return bridge.downloadFile(filename, bytes);
}

export async function readClipboardText(): Promise<string> {
	const bridge = getElectronBridge();
	if (!bridge?.readClipboardText) {
		throw new Error("Clipboard access is not available in this Electron build");
	}
	return bridge.readClipboardText();
}

export async function writeClipboardText(text: string): Promise<void> {
	const bridge = getElectronBridge();
	if (!bridge?.writeClipboardText) {
		throw new Error("Clipboard access is not available in this Electron build");
	}
	await bridge.writeClipboardText(text);
}

export async function openExternalUrl(url: string): Promise<void> {
	const bridge = getElectronBridge();
	if (!bridge?.openExternalUrl) {
		throw new Error(
			"External URL opening is not available in this Electron build",
		);
	}
	await bridge.openExternalUrl(url);
}

export async function pickDirectory(): Promise<string | null> {
	const bridge = getElectronBridge();
	if (!bridge?.pickDirectory) {
		return null;
	}
	return bridge.pickDirectory();
}

export async function withCurrentWindow<T>(
	callback: DesktopWindowCallback<T>,
): Promise<T> {
	const bridge = getElectronBridge();
	if (!bridge) {
		throw new Error("Window controls are not available in this Electron build");
	}
	return callback({
		minimize: () =>
			requireBridgeMethod(
				bridge.windowMinimize,
				"Window minimize is not available in this Electron build",
			)(),
		maximize: () =>
			requireBridgeMethod(
				bridge.windowMaximize,
				"Window maximize is not available in this Electron build",
			)(),
		unmaximize: () =>
			requireBridgeMethod(
				bridge.windowUnmaximize,
				"Window unmaximize is not available in this Electron build",
			)(),
		isMaximized: () =>
			requireBridgeMethod(
				bridge.windowIsMaximized,
				"Window maximize state is not available in this Electron build",
			)(),
		close: () =>
			requireBridgeMethod(
				bridge.windowClose,
				"Window close is not available in this Electron build",
			)(),
		isFullscreen: () =>
			requireBridgeMethod(
				bridge.windowIsFullscreen,
				"Window fullscreen state is not available in this Electron build",
			)(),
		onResized: async (listener) =>
			requireBridgeMethod(
				bridge.onWindowResized,
				"Window resize events are not available in this Electron build",
			)(listener),
	});
}

export async function checkForAppUpdate(
	endpoint?: string | null,
): Promise<DesktopUpdateMetadata | null> {
	const bridge = getElectronBridge();
	if (!bridge?.checkForAppUpdate) {
		return null;
	}
	return bridge.checkForAppUpdate(endpoint);
}

export async function downloadAppUpdate(
	rid: number,
	onEvent: (event: DesktopDownloadEvent) => void,
): Promise<number> {
	const bridge = getElectronBridge();
	if (!bridge?.downloadAppUpdate) {
		throw new Error("App updates are not available in this Electron build");
	}
	return bridge.downloadAppUpdate(rid, onEvent);
}

export async function installAppUpdate(
	updateRid: number,
	bytesRid: number,
): Promise<void> {
	const bridge = getElectronBridge();
	if (!bridge?.installAppUpdate) {
		throw new Error("App updates are not available in this Electron build");
	}
	await bridge.installAppUpdate(updateRid, bytesRid);
}

export async function closeAppUpdate(
	updateRid?: number | null,
	bytesRid?: number | null,
): Promise<void> {
	const bridge = getElectronBridge();
	if (!bridge?.closeAppUpdate) {
		return;
	}
	await bridge.closeAppUpdate(updateRid, bytesRid);
}

export async function relaunchApp(): Promise<void> {
	const bridge = getElectronBridge();
	if (!bridge?.relaunchApp) {
		throw new Error("App relaunch is not available in this Electron build");
	}
	await bridge.relaunchApp();
}
