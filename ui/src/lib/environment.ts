export function isTauriShell(): boolean {
	return typeof window !== "undefined" && "__TAURI_INTERNALS__" in window;
}

export function getApiBase(): string {
	return import.meta.env.VITE_API_BASE_URL || "/api";
}
