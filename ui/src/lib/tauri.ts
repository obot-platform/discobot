import { isTauriShell } from "$lib/environment";

export async function readClipboardText(): Promise<string> {
	if (isTauriShell()) {
		const { readText } = await import("@tauri-apps/plugin-clipboard-manager");
		return (await readText()) ?? "";
	}

	if (typeof navigator === "undefined" || !navigator.clipboard?.readText) {
		throw new Error("Clipboard API not available");
	}

	return navigator.clipboard.readText();
}

export async function writeClipboardText(text: string): Promise<void> {
	if (isTauriShell()) {
		const { writeText } = await import("@tauri-apps/plugin-clipboard-manager");
		await writeText(text);
		return;
	}

	if (typeof navigator === "undefined" || !navigator.clipboard?.writeText) {
		throw new Error("Clipboard API not available");
	}

	await navigator.clipboard.writeText(text);
}

export async function openUrl(url: string): Promise<void> {
	if (isTauriShell()) {
		const { openUrl: tauriOpenUrl } = await import("@tauri-apps/plugin-opener");
		await tauriOpenUrl(url);
		return;
	}

	if (url.startsWith("http://") || url.startsWith("https://")) {
		window.open(url, "_blank", "noopener,noreferrer");
		return;
	}

	window.location.href = url;
}
