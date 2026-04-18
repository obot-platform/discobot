function toArrayBuffer(bytes: Uint8Array): ArrayBuffer {
	return bytes.buffer.slice(
		bytes.byteOffset,
		bytes.byteOffset + bytes.byteLength,
	) as ArrayBuffer;
}

export async function downloadFile(
	filename: string,
	bytes: Uint8Array,
	mimeType: string,
): Promise<void> {
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
	if (typeof navigator === "undefined" || !navigator.clipboard?.readText) {
		throw new Error("Clipboard API not available");
	}

	return navigator.clipboard.readText();
}

export async function writeClipboardText(text: string): Promise<void> {
	if (typeof navigator === "undefined" || !navigator.clipboard?.writeText) {
		throw new Error("Clipboard API not available");
	}

	await navigator.clipboard.writeText(text);
}

export async function openExternalUrl(url: string): Promise<void> {
	if (url.startsWith("http://") || url.startsWith("https://")) {
		window.open(url, "_blank", "noopener,noreferrer");
		return;
	}

	window.location.href = url;
}

export async function pickDirectory(): Promise<string | null> {
	return null;
}
