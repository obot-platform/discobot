// Default project ID for anonymous user mode (matches Go backend)
export const PROJECT_ID = "local";

const DEFAULT_SSH_PORT = 3333;
const tauriLocalhost = "localhost";

// Cached Tauri server config (populated on first use)
let tauriServerConfig: { port: number; secret: string } | null = null;
let tauriInitialized = false;

// Server config (fetched from backend)
let sshPort = DEFAULT_SSH_PORT;

/**
 * Initialize Tauri server config (port and secret).
 * Call this early in app startup when running in Tauri.
 */
export async function initTauriConfig(): Promise<void> {
	if (!isTauri()) {
		tauriInitialized = true;
		return;
	}

	const { invoke } = await import("@tauri-apps/api/core");
	const [port, secret] = await Promise.all([
		invoke<number>("get_server_port"),
		invoke<string>("get_server_secret"),
	]);
	tauriInitialized = true;
	tauriServerConfig = { port, secret };
}

/**
 * Get cached Tauri server config. Returns null if not in Tauri or not initialized.
 */
export function getTauriServerConfig() {
	return tauriServerConfig;
}

export function isTauri() {
	if (tauriServerConfig) {
		return true;
	}
	return typeof window !== "undefined" && "__TAURI_INTERNALS__" in window;
}

/**
 * Get the Tauri auth token if in Tauri mode, otherwise null.
 */
export function getTauriToken(): string | null {
	return tauriServerConfig?.secret ?? null;
}

/**
 * Append the Tauri auth token to a URL if in Tauri mode.
 * Used for WebSocket and SSE URLs that need authentication.
 */
export function appendAuthToken(url: string): string {
	const token = getTauriToken();
	if (!token) {
		return url;
	}
	const separator = url.includes("?") ? "&" : "?";
	return `${url}${separator}token=${encodeURIComponent(token)}`;
}

/**
 * Get the backend API root URL (without project path).
 *
 * - In Tauri: connects directly to Go server on dynamic port
 * - In browser with *-svc-ui.* or *-svc-ui-svelte.* hostname: routes to corresponding *-svc-api.* host
 * - Otherwise: calls Go backend directly on port 3001
 */
export function getApiRootBase() {
	if (typeof window === "undefined") {
		// Server-side rendering - call backend directly
		return "http://localhost:3001/api";
	}

	// Check if hostname matches *-svc-ui.* or *-svc-ui-svelte.* pattern
	const hostname = window.location.hostname;
	const svcUiHostToken = hostname.includes("-svc-ui-svelte.")
		? "-svc-ui-svelte."
		: hostname.includes("-svc-ui.")
			? "-svc-ui."
			: null;
	if (svcUiHostToken) {
		const apiHostname = hostname.replace(svcUiHostToken, "-svc-api.");
		const protocol = window.location.protocol;
		const port = window.location.port;
		const apiHost = port ? `${apiHostname}:${port}` : apiHostname;
		return `${protocol}//${apiHost}/api`;
	}

	// Check if running in Tauri with initialized config
	if (tauriServerConfig) {
		return `http://${tauriLocalhost}:${tauriServerConfig.port}/api`;
	}

	if (!tauriInitialized && isTauri()) {
		throw new Error("not initialized, must call initTauriConfig() first");
	}

	return "http://localhost:3001/api";
}

/**
 * Get the backend API base URL (with project path).
 */
export function getApiBase() {
	return `${getApiRootBase()}/projects/${PROJECT_ID}`;
}

/**
 * Get the backend WebSocket base URL.
 * Includes auth token in Tauri mode.
 *
 * - In Tauri: connects directly to Go server on dynamic port with token
 * - In browser with *-svc-ui.* or *-svc-ui-svelte.* hostname: routes to corresponding *-svc-api.* host
 * - Otherwise: connects to Go backend directly on port 3001
 */
export function getWsBase() {
	const url = getApiRootBase();
	return `${url.replace(/^http/, "ws")}/projects/${PROJECT_ID}`;
}

/**
 * Initialize server config by fetching from the backend.
 * Call after the backend is ready.
 */
export async function initServerConfig(): Promise<void> {
	try {
		const resp = await fetch(
			appendAuthToken(`${getApiRootBase()}/server-config`),
		);
		if (resp.ok) {
			const config = await resp.json();
			if (typeof config.ssh_port === "number" && config.ssh_port > 0) {
				sshPort = config.ssh_port;
			}
		}
	} catch {
		// Fall back to default SSH port
	}
}

/**
 * Get the SSH port configured on the server. Defaults to 3333.
 */
export function getSSHPort(): number {
	return sshPort;
}
