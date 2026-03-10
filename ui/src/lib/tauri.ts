import { isTauriShell } from "$lib/environment";

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
