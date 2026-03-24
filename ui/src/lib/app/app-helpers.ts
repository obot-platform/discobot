import type { Session } from "$lib/api-types";
import { getApiBase, isTauriShell } from "$lib/environment";
import type {
	ChatWidthMode,
	SettingsDialogTab,
	UpdateStatus,
} from "$lib/app/app-context.types";
import {
	isPreferredIde,
	type PreferredIde,
	type SessionSummary,
	type WindowControlsSide,
} from "$lib/shell-types";

export const PREFERRED_IDE_STORAGE_KEY = "preferred.ide";
export const CHAT_WIDTH_MODE_STORAGE_KEY = "chat.width.mode";
export const DEFAULT_MODEL_STORAGE_KEY = "chat.default.model";
export const IGNORED_UPDATE_VERSION_STORAGE_KEY = "update.ignored.version";
export const SIDEBAR_RECENT_OPEN_STORAGE_KEY = "sidebar.recent.open";
export const SIDEBAR_ALL_OPEN_STORAGE_KEY = "sidebar.all.open";
export const PROMPT_HISTORY_STORAGE_KEY = "discobot:composer-history";
export const PINNED_PROMPTS_STORAGE_KEY = "discobot:composer-history:pinned";
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
	return isPreferredIde(stored) ? stored : "cursor";
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

export function readSidebarRecentOpen(): boolean {
	if (typeof window === "undefined") {
		return true;
	}
	const stored = window.localStorage.getItem(SIDEBAR_RECENT_OPEN_STORAGE_KEY);
	return stored === null ? true : stored === "true";
}

export function readSidebarAllOpen(): boolean {
	if (typeof window === "undefined") {
		return true;
	}
	const stored = window.localStorage.getItem(SIDEBAR_ALL_OPEN_STORAGE_KEY);
	return stored === null ? true : stored === "true";
}

export function readPinnedPrompts(): string[] {
	if (typeof window === "undefined") {
		return [];
	}

	const stored = window.localStorage.getItem(PINNED_PROMPTS_STORAGE_KEY);
	if (!stored) {
		return [];
	}

	try {
		const parsed = JSON.parse(stored);
		return Array.isArray(parsed)
			? parsed.filter((item): item is string => typeof item === "string")
			: [];
	} catch {
		return [];
	}
}

export function readPromptHistory(): string[] {
	if (typeof window === "undefined") {
		return [];
	}

	const stored = window.localStorage.getItem(PROMPT_HISTORY_STORAGE_KEY);
	if (!stored) {
		return [];
	}

	try {
		const parsed = JSON.parse(stored);
		return Array.isArray(parsed)
			? parsed.filter((item): item is string => typeof item === "string")
			: [];
	} catch {
		return [];
	}
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

function compareIsoDatesDesc(left: string, right: string) {
	const leftTime = new Date(left).getTime();
	const rightTime = new Date(right).getTime();
	if (Number.isNaN(leftTime) || Number.isNaN(rightTime)) {
		return 0;
	}
	return rightTime - leftTime;
}

function toSessionSummary(session: Session, recentIds: Set<string>): SessionSummary {
	return {
		id: session.id,
		name: session.displayName || session.name,
		status: session.status,
		isRecent: recentIds.has(session.id),
	};
}

function getRecentSessionIds(sessions: Session[]) {
	return new Set(
		[...sessions]
			.sort((a, b) => compareIsoDatesDesc(a.timestamp, b.timestamp))
			.slice(0, RECENT_SESSIONS_LIMIT)
			.map((session) => session.id),
	);
}

export function toSessionSummaries(sessions: Session[]): SessionSummary[] {
	const recentIds = getRecentSessionIds(sessions);
	return [...sessions]
		.sort((a, b) => compareIsoDatesDesc(a.createdAt, b.createdAt))
		.map((session) => toSessionSummary(session, recentIds));
}

export function toRecentSessionSummaries(sessions: Session[]): SessionSummary[] {
	const recentIds = getRecentSessionIds(sessions);
	return [...sessions]
		.filter((session) => recentIds.has(session.id))
		.sort((a, b) => compareIsoDatesDesc(a.timestamp, b.timestamp))
		.map((session) => toSessionSummary(session, recentIds));
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
