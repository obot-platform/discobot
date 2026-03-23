import { isTauriShell } from "$lib/environment";

type DownloadFileOptions = {
	filename: string;
	content: string | Uint8Array | ArrayBuffer;
	mimeType?: string;
};

function toUint8Array(content: DownloadFileOptions["content"]): Uint8Array {
	if (typeof content === "string") {
		return new TextEncoder().encode(content);
	}
	if (content instanceof Uint8Array) {
		return content;
	}
	return new Uint8Array(content);
}

function toArrayBuffer(bytes: Uint8Array): ArrayBuffer {
	return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer;
}

export async function downloadFile({ filename, content, mimeType = "application/octet-stream" }: DownloadFileOptions): Promise<void> {
	const bytes = toUint8Array(content);

	if (isTauriShell()) {
		const { invoke } = await import("@tauri-apps/api/core");
		const { toast } = await import("svelte-sonner");
		await invoke("save_file_to_downloads", {
			filename,
			content: Array.from(bytes),
		});
		toast.success(`${filename} saved to Downloads`);
		return;
	}

	if (typeof document === "undefined") {
		throw new Error("Download is not available in this environment");
	}

	const blob = new Blob([toArrayBuffer(bytes)], { type: mimeType });
	const url = URL.createObjectURL(blob);
	const link = document.createElement("a");
	link.href = url;
	link.download = filename;
	document.body.appendChild(link);
	link.click();
	document.body.removeChild(link);
	URL.revokeObjectURL(url);
}

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
