import type { Session } from "$lib/api-types";
import { getApiBase, isTauriShell } from "$lib/environment";
import type {
	ChatWidthMode,
	SettingsDialogTab,
	UpdateStatus,
} from "$lib/app/app-context.types";
import type { PreferredIde, SessionSummary, WindowControlsSide } from "$lib/shell-types";

export const PREFERRED_IDE_STORAGE_KEY = "preferred.ide";
export const CHAT_WIDTH_MODE_STORAGE_KEY = "chat.width.mode";
export const DEFAULT_MODEL_STORAGE_KEY = "chat.default.model";
export const IGNORED_UPDATE_VERSION_STORAGE_KEY = "update.ignored.version";
export const RECENT_SESSIONS_LIMIT = 4;

export function detectWindowControlsSide(): WindowControlsSide {
	if (typeof navigator === "undefined") {
		return "right";
	}

	const nav = navigator as Navigator & {
		userAgentData?: {
			platform?: string;
		};
	};
	const platform = nav.userAgentData?.platform || nav.platform || nav.userAgent;
	return /mac/i.test(platform) ? "left" : "right";
}

export function readPreferredIde(): PreferredIde {
	if (typeof window === "undefined") {
		return "cursor";
	}

	const stored = window.localStorage.getItem(PREFERRED_IDE_STORAGE_KEY);
	return stored === "cursor" || stored === "vscode" || stored === "zed" ? stored : "cursor";
}

export function readChatWidthMode(): ChatWidthMode {
	if (typeof window === "undefined") {
		return "constrained";
	}

	const stored = window.localStorage.getItem(CHAT_WIDTH_MODE_STORAGE_KEY);
	return stored === "full" ? "full" : "constrained";
}

export function readDefaultModel(): string {
	if (typeof window === "undefined") {
		return "";
	}

	return window.localStorage.getItem(DEFAULT_MODEL_STORAGE_KEY) ?? "";
}

export function readIgnoredUpdateVersion(): string | null {
	if (typeof window === "undefined") {
		return null;
	}

	return window.localStorage.getItem(IGNORED_UPDATE_VERSION_STORAGE_KEY);
}

export function writeStorage(key: string, value: string | null) {
	if (typeof window === "undefined") {
		return;
	}

	if (value === null) {
		window.localStorage.removeItem(key);
		return;
	}

	window.localStorage.setItem(key, value);
}

export async function delay(ms: number) {
	await new Promise((resolve) => window.setTimeout(resolve, ms));
}

export function toSessionSummaries(sessions: Session[]): SessionSummary[] {
	const sortedSessions = [...sessions].sort((a, b) => {
		const left = new Date(a.timestamp).getTime();
		const right = new Date(b.timestamp).getTime();
		if (Number.isNaN(left) || Number.isNaN(right)) {
			return 0;
		}
		return right - left;
	});

	const recentIds = new Set(
		sortedSessions.slice(0, RECENT_SESSIONS_LIMIT).map((session) => session.id),
	);

	return sortedSessions.map((session) => ({
		id: session.id,
		name: session.displayName || session.name,
		status: session.status,
		isRecent: recentIds.has(session.id),
	}));
}

export function getAppEnvironment() {
	return {
		apiBase: getApiBase(),
		isTauri: isTauriShell(),
		windowControlsSide: detectWindowControlsSide(),
	};
}

export function getDefaultSettingsTab(): SettingsDialogTab {
	return "appearance";
}

export function getDefaultUpdateStatus(): UpdateStatus {
	return "idle";
}
