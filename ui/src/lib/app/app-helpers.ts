import type { Session, ThreadState } from "$lib/api-types";
import { getApiBase, isTauriShell } from "$lib/environment";
import type {
	ChatWidthMode,
	SettingsDialogTab,
	UpdateStatus,
} from "$lib/app/app-context.types";
import {
	isPreferredIde,
	type PreferredIde,
	type RecentThreadSummary,
	type SessionSummary,
	type WindowControlsSide,
} from "$lib/shell-types";

export const PREFERRED_IDE_STORAGE_KEY = "preferred.ide";
export const CHAT_WIDTH_MODE_STORAGE_KEY = "chat.width.mode";
export const DEFAULT_MODEL_STORAGE_KEY = "chat.default.model";
export const IGNORED_UPDATE_VERSION_STORAGE_KEY = "update.ignored.version";
export const SIDEBAR_RECENT_OPEN_STORAGE_KEY = "sidebar.recent.open";
export const SIDEBAR_ALL_OPEN_STORAGE_KEY = "sidebar.all.open";
export const SIDEBAR_ALL_GROUPED_STORAGE_KEY = "sidebar.all.grouped";
export const RECENT_THREADS_VISIBLE_LIMIT_STORAGE_KEY =
	"recent.threads.visible.limit";
export const RECENT_THREADS_STORAGE_KEY = "recent.threads";
export const PROMPT_HISTORY_STORAGE_KEY = "discobot:composer-history";
export const PINNED_PROMPTS_STORAGE_KEY = "discobot:composer-history:pinned";
export const RECENT_SESSIONS_LIMIT = 4;
export const RECENT_THREAD_ENTRIES_LIMIT = 12;
export const RECENT_THREADS_VISIBLE_LIMIT_PRESETS = [1, 4, 8, 12] as const;
export const DEFAULT_RECENT_THREADS_VISIBLE_LIMIT = 4;
export const DEFAULT_PREFERRED_IDE: PreferredIde = "zed";

export type RecentThreadEntry = {
	sessionId: string;
	sessionName: string;
	threadId: string;
	threadName: string;
	state?: ThreadState;
	lastMessage?: string;
	lastAccessedAt: string;
};

export type RecentThreadPayload = Omit<RecentThreadEntry, "lastAccessedAt"> & {
	lastMessage: string;
};

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
		return DEFAULT_PREFERRED_IDE;
	}

	const stored = window.localStorage.getItem(PREFERRED_IDE_STORAGE_KEY);
	return isPreferredIde(stored) ? stored : DEFAULT_PREFERRED_IDE;
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

export function readSidebarAllGrouped(): boolean {
	if (typeof window === "undefined") {
		return true;
	}
	const stored = window.localStorage.getItem(SIDEBAR_ALL_GROUPED_STORAGE_KEY);
	return stored === null ? true : stored === "true";
}

export function readRecentThreadsVisibleLimit(): number {
	if (typeof window === "undefined") {
		return DEFAULT_RECENT_THREADS_VISIBLE_LIMIT;
	}
	const stored = window.localStorage.getItem(
		RECENT_THREADS_VISIBLE_LIMIT_STORAGE_KEY,
	);
	const value = Number(stored);
	return RECENT_THREADS_VISIBLE_LIMIT_PRESETS.includes(
		value as (typeof RECENT_THREADS_VISIBLE_LIMIT_PRESETS)[number],
	)
		? value
		: DEFAULT_RECENT_THREADS_VISIBLE_LIMIT;
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

function isRecentThreadEntry(value: unknown): value is RecentThreadEntry {
	if (!value || typeof value !== "object") {
		return false;
	}

	const candidate = value as Partial<RecentThreadEntry>;
	return (
		typeof candidate.sessionId === "string" &&
		candidate.sessionId.length > 0 &&
		typeof candidate.sessionName === "string" &&
		typeof candidate.threadId === "string" &&
		candidate.threadId.length > 0 &&
		typeof candidate.threadName === "string" &&
		(candidate.state === undefined ||
			candidate.state === "interrupted" ||
			candidate.state === "cancelled") &&
		(candidate.lastMessage === undefined ||
			typeof candidate.lastMessage === "string") &&
		typeof candidate.lastAccessedAt === "string"
	);
}

function areRecentThreadEntriesEqual(
	left: RecentThreadEntry[],
	right: RecentThreadEntry[],
): boolean {
	return (
		left.length === right.length &&
		left.every(
			(entry, index) =>
				entry.sessionId === right[index]?.sessionId &&
				entry.sessionName === right[index]?.sessionName &&
				entry.threadId === right[index]?.threadId &&
				entry.threadName === right[index]?.threadName &&
				(entry.state ?? "") === (right[index]?.state ?? "") &&
				(entry.lastMessage ?? "") === (right[index]?.lastMessage ?? "") &&
				entry.lastAccessedAt === right[index]?.lastAccessedAt,
		)
	);
}

function areSameRecentThread(
	left: Pick<RecentThreadEntry, "sessionId" | "threadId">,
	right: Pick<RecentThreadEntry, "sessionId" | "threadId">,
): boolean {
	return left.sessionId === right.sessionId && left.threadId === right.threadId;
}

function normalizeRecentThreadEntries(
	entries: RecentThreadEntry[],
): RecentThreadEntry[] {
	const dedupedEntries: RecentThreadEntry[] = [];
	for (const entry of entries) {
		if (
			dedupedEntries.some((existing) => areSameRecentThread(existing, entry))
		) {
			continue;
		}
		dedupedEntries.push(entry);
	}

	if (dedupedEntries.length <= RECENT_THREAD_ENTRIES_LIMIT) {
		return dedupedEntries;
	}

	const oldestEntry = dedupedEntries.reduce((oldest, entry) =>
		compareIsoDatesDesc(oldest.lastAccessedAt, entry.lastAccessedAt) < 0
			? oldest
			: entry,
	);

	return dedupedEntries.filter((entry) => entry !== oldestEntry);
}

export function readRecentThreadEntries(): RecentThreadEntry[] {
	if (typeof window === "undefined") {
		return [];
	}

	const stored = window.localStorage.getItem(RECENT_THREADS_STORAGE_KEY);
	if (!stored) {
		return [];
	}

	try {
		const parsed = JSON.parse(stored);
		if (!Array.isArray(parsed)) {
			return [];
		}

		return normalizeRecentThreadEntries(
			parsed.filter(isRecentThreadEntry).map((entry) => ({
				...entry,
				lastMessage: entry.lastMessage ?? "",
			})),
		);
	} catch {
		return [];
	}
}

export function touchRecentThread(
	entries: RecentThreadEntry[],
	thread: RecentThreadPayload,
	lastAccessedAt = new Date().toISOString(),
): RecentThreadEntry[] {
	const existingIndex = entries.findIndex((entry) =>
		areSameRecentThread(entry, thread),
	);
	if (existingIndex !== -1) {
		const existingEntry = entries[existingIndex];
		if (!existingEntry) {
			return entries;
		}
		return [
			{ ...existingEntry, ...thread, lastAccessedAt },
			...entries.filter((_, index) => index !== existingIndex),
		];
	}

	return normalizeRecentThreadEntries([
		{ ...thread, lastAccessedAt },
		...entries,
	]);
}

export function getRecentThreadEntryForSessionSelection(args: {
	session: Pick<SessionSummary, "id" | "name">;
	sessionContext: {
		threads: {
			selectedId: string | null;
			list: Array<{
				id: string;
				name: string;
				state?: ThreadState;
				lastMessage?: string;
			}>;
		};
	} | null;
}): RecentThreadPayload | null {
	const { session, sessionContext } = args;
	if (!sessionContext) {
		return null;
	}

	const selectedThread = sessionContext.threads.list.find(
		(thread) => thread.id === sessionContext.threads.selectedId,
	);
	if (!selectedThread) {
		return null;
	}

	return {
		sessionId: session.id,
		sessionName: session.name,
		threadId: selectedThread.id,
		threadName: selectedThread.name,
		state: selectedThread.state,
		lastMessage: selectedThread.lastMessage ?? "",
	};
}

export function refreshRecentThread(
	entries: RecentThreadEntry[],
	thread: RecentThreadPayload,
): RecentThreadEntry[] {
	const existingIndex = entries.findIndex((entry) =>
		areSameRecentThread(entry, thread),
	);
	if (existingIndex === -1) {
		return entries;
	}

	return entries.map((entry, index) =>
		index === existingIndex ? { ...entry, ...thread } : entry,
	);
}

export function refreshRecentSessionName(
	entries: RecentThreadEntry[],
	sessionId: string,
	sessionName: string,
): RecentThreadEntry[] {
	return entries.map((entry) =>
		entry.sessionId === sessionId ? { ...entry, sessionName } : entry,
	);
}

export function removeRecentThread(
	entries: RecentThreadEntry[],
	sessionId: string,
	threadId: string,
): RecentThreadEntry[] {
	return entries.filter(
		(entry) => entry.sessionId !== sessionId || entry.threadId !== threadId,
	);
}

export function removeRecentThreadsForSession(
	entries: RecentThreadEntry[],
	sessionId: string,
): RecentThreadEntry[] {
	return entries.filter((entry) => entry.sessionId !== sessionId);
}

export function reconcileRecentThreadsWithSessions(
	entries: RecentThreadEntry[],
	sessionIds: Iterable<string>,
): RecentThreadEntry[] {
	const validSessionIds = new Set(sessionIds);
	const nextEntries = entries.filter((entry) =>
		validSessionIds.has(entry.sessionId),
	);
	return areRecentThreadEntriesEqual(entries, nextEntries)
		? entries
		: nextEntries;
}

export function writeRecentThreadEntries(entries: RecentThreadEntry[]) {
	writeStorage(
		RECENT_THREADS_STORAGE_KEY,
		entries.length > 0 ? JSON.stringify(entries) : null,
	);
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

export function toSessionSummaries(sessions: Session[]): SessionSummary[] {
	return [...sessions]
		.sort((a, b) => compareIsoDatesDesc(a.createdAt, b.createdAt))
		.map((session) => ({
			id: session.id,
			name: session.displayName || session.name,
			status: session.status,
			isRecent: false,
			workspaceId: session.workspaceId,
		}));
}

export function toRecentThreadSummaries(
	summaries: SessionSummary[],
	recentEntries: RecentThreadEntry[],
): RecentThreadSummary[] {
	const summariesById = new Map(
		summaries.map((summary) => [summary.id, summary]),
	);
	return recentEntries.flatMap((entry) => {
		const summary = summariesById.get(entry.sessionId);
		return summary
			? [
					{
						sessionId: entry.sessionId,
						sessionName: summary.name,
						sessionStatus: summary.status,
						threadId: entry.threadId,
						threadName: entry.threadName,
						...(entry.state ? { state: entry.state } : {}),
						lastMessage: entry.lastMessage ?? "",
						lastAccessedAt: entry.lastAccessedAt,
					},
				]
			: [];
	});
}

export function getVisibleRecentThreads(
	recentThreads: RecentThreadSummary[],
	limit: number,
): RecentThreadSummary[] {
	if (limit <= 0 || recentThreads.length === 0) {
		return [];
	}
	if (recentThreads.length <= limit) {
		return recentThreads;
	}

	const selectedIndexes = new Set(
		recentThreads
			.map((thread, index) => ({ thread, index }))
			.sort((left, right) =>
				compareIsoDatesDesc(
					left.thread.lastAccessedAt,
					right.thread.lastAccessedAt,
				),
			)
			.slice(0, limit)
			.map(({ index }) => index),
	);

	return recentThreads.filter((_, index) => selectedIndexes.has(index));
}

export function getMountedSessionIds(
	selectedSessionId: string | null,
	recentThreads: Array<Pick<RecentThreadSummary, "sessionId">>,
	limit = RECENT_SESSIONS_LIMIT,
): string[] {
	const sessionIds: string[] = [];
	const seen = new Set<string>();

	for (const sessionId of [
		selectedSessionId,
		...recentThreads.map((thread) => thread.sessionId),
	]) {
		if (!sessionId || seen.has(sessionId)) {
			continue;
		}
		seen.add(sessionId);
		sessionIds.push(sessionId);
		if (sessionIds.length >= limit) {
			break;
		}
	}

	return sessionIds;
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
