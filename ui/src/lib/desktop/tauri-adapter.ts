import type {
	DesktopDownloadEvent,
	DesktopServerConfig,
	DesktopUpdateMetadata,
	DesktopWindow,
	DesktopWindowCallback,
} from "$lib/desktop/types";

let serverConfig: DesktopServerConfig | null = null;

function isTopLevelWindowContext(): boolean {
	if (typeof window === "undefined") {
		return false;
	}
	try {
		return window.self === window.top;
	} catch {
		return false;
	}
}

function hasInjectedStandaloneConfig(): boolean {
	const runtimeWindow = window as Window & {
		__DISCOBOT_CONFIG__?: {
			apiRoot?: string;
		};
	};
	return (
		typeof window !== "undefined" &&
		Boolean(runtimeWindow.__DISCOBOT_CONFIG__?.apiRoot)
	);
}

export function detectTauriRuntime(): boolean {
	if (serverConfig) {
		return true;
	}
	return (
		typeof window !== "undefined" &&
		"__TAURI_INTERNALS__" in window &&
		isTopLevelWindowContext() &&
		!hasInjectedStandaloneConfig()
	);
}

export async function initServerConfig(): Promise<void> {
	if (!detectTauriRuntime()) {
		return;
	}

	const { invoke } = await import("@tauri-apps/api/core");
	const [port, secret] = await Promise.all([
		invoke<number>("get_desktop_server_port"),
		invoke<string>("get_desktop_server_secret"),
	]);
	serverConfig = { port, secret };
}

export function getServerConfig(): DesktopServerConfig | null {
	return serverConfig;
}

export async function saveFileToDownloads(
	filename: string,
	bytes: Uint8Array,
): Promise<void> {
	const { invoke } = await import("@tauri-apps/api/core");
	await invoke("save_file_to_downloads", {
		filename,
		content: Array.from(bytes),
	});
}

export async function readClipboardText(): Promise<string> {
	const { readText } = await import("@tauri-apps/plugin-clipboard-manager");
	return (await readText()) ?? "";
}

export async function writeClipboardText(text: string): Promise<void> {
	const { writeText } = await import("@tauri-apps/plugin-clipboard-manager");
	await writeText(text);
}

export async function openExternalUrl(url: string): Promise<void> {
	const { openUrl } = await import("@tauri-apps/plugin-opener");
	await openUrl(url);
}

export async function pickDirectory(): Promise<string | null> {
	const { open } = await import("@tauri-apps/plugin-dialog");
	const selection = await open({
		directory: true,
		multiple: false,
	});
	return typeof selection === "string" ? selection : null;
}

export async function withCurrentWindow<T>(
	callback: DesktopWindowCallback<T>,
): Promise<T> {
	const { getCurrentWindow } = await import("@tauri-apps/api/window");
	const currentWindow = getCurrentWindow() as unknown as DesktopWindow;
	return callback(currentWindow);
}

export async function checkForAppUpdate(
	endpoint?: string | null,
): Promise<DesktopUpdateMetadata | null> {
	const { invoke } = await import("@tauri-apps/api/core");
	return invoke<DesktopUpdateMetadata | null>("check_for_app_update", {
		endpoint: endpoint ?? null,
	});
}

export async function downloadAppUpdate(
	rid: number,
	onEvent: (event: DesktopDownloadEvent) => void,
): Promise<number> {
	const { Channel, invoke } = await import("@tauri-apps/api/core");
	const channel = new Channel<DesktopDownloadEvent>();
	channel.onmessage = (event) => {
		onEvent(event);
	};
	return invoke<number>("download_app_update", {
		rid,
		onEvent: channel,
	});
}

export async function installAppUpdate(
	updateRid: number,
	bytesRid: number,
): Promise<void> {
	const { invoke } = await import("@tauri-apps/api/core");
	await invoke("install_app_update", {
		updateRid,
		bytesRid,
	});
}

export async function closeAppUpdate(
	updateRid?: number | null,
	bytesRid?: number | null,
): Promise<void> {
	const { invoke } = await import("@tauri-apps/api/core");
	await invoke("close_app_update", {
		updateRid: updateRid ?? null,
		bytesRid: bytesRid ?? null,
	});
}

export async function relaunchApp(): Promise<void> {
	const { relaunch } = await import("@tauri-apps/plugin-process");
	await relaunch();
}
