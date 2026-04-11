import assert from "node:assert/strict";
import test from "node:test";

import type { Session } from "$lib/api-types";
import type { SessionSummary } from "$lib/shell-types";
import {
	getRecentThreadEntryForSessionSelection,
	PREFERRED_IDE_STORAGE_KEY,
	RECENT_THREADS_STORAGE_KEY,
	RECENT_THREADS_VISIBLE_LIMIT_STORAGE_KEY,
	readPreferredIde,
	readRecentThreadsVisibleLimit,
	reconcileRecentThreadsWithSessions,
	readRecentThreadEntries,
	refreshRecentThread,
	getMountedSessionIds,
	getVisibleRecentThreads,
	removeRecentThread,
	removeRecentThreadsForSession,
	toRecentThreadSummaries,
	toSessionSummaries,
	touchRecentThread,
	writeRecentThreadEntries,
} from "../app-helpers";
import {
	getNextSelectedSessionId,
	upsertSession,
} from "../domains/app-sessions.helpers";

type StorageLike = {
	getItem: (key: string) => string | null;
	setItem: (key: string, value: string) => void;
	removeItem: (key: string) => void;
	clear: () => void;
};

const sessions: SessionSummary[] = [
	{ id: "session-1", name: "One", isRecent: true, status: "ready" },
	{ id: "session-2", name: "Two", isRecent: true, status: "ready" },
	{ id: "session-3", name: "Three", isRecent: false, status: "error" },
];

function makeSession(overrides: Partial<Session> = {}): Session {
	return {
		id: "session-1",
		name: "Session",
		description: "",
		createdAt: "2026-01-01T00:00:00Z",
		timestamp: "2026-01-01T00:00:00Z",
		status: "ready",
		files: [],
		...overrides,
	};
}

function createLocalStorage(): StorageLike {
	const values = new Map<string, string>();
	return {
		getItem: (key) => values.get(key) ?? null,
		setItem: (key, value) => {
			values.set(key, value);
		},
		removeItem: (key) => {
			values.delete(key);
		},
		clear: () => {
			values.clear();
		},
	};
}

function withLocalStorage(run: (storage: StorageLike) => void) {
	const windowWithStorage = globalThis as typeof globalThis & {
		window?: Window;
	};
	const previousWindow = windowWithStorage.window;
	const storage = createLocalStorage();
	windowWithStorage.window = { localStorage: storage } as unknown as Window &
		typeof globalThis;
	try {
		run(storage);
	} finally {
		windowWithStorage.window = previousWindow;
	}
}

test("readPreferredIde defaults to Zed when nothing is stored", () => {
	withLocalStorage(() => {
		assert.equal(readPreferredIde(), "zed");
	});
});

test("readPreferredIde returns the stored IDE when present", () => {
	withLocalStorage((storage) => {
		storage.setItem(PREFERRED_IDE_STORAGE_KEY, "cursor");
		assert.equal(readPreferredIde(), "cursor");
	});
});

test("readRecentThreadsVisibleLimit accepts the disabled preset", () => {
	withLocalStorage((storage) => {
		storage.setItem(RECENT_THREADS_VISIBLE_LIMIT_STORAGE_KEY, "1");
		assert.equal(readRecentThreadsVisibleLimit(), 1);
	});
});

test("readRecentThreadsVisibleLimit falls back for unsupported values", () => {
	withLocalStorage((storage) => {
		storage.setItem(RECENT_THREADS_VISIBLE_LIMIT_STORAGE_KEY, "2");
		assert.equal(readRecentThreadsVisibleLimit(), 4);
	});
});

test("getNextSelectedSessionId keeps the current selection when another session is deleted", () => {
	assert.equal(
		getNextSelectedSessionId(sessions, "session-1", "session-2"),
		"session-2",
	);
});

test("getNextSelectedSessionId falls back to the first remaining session", () => {
	assert.equal(
		getNextSelectedSessionId(sessions, "session-2", "session-2"),
		"session-1",
	);
});

test("getNextSelectedSessionId returns null when the last session is deleted", () => {
	assert.equal(
		getNextSelectedSessionId(
			[{ id: "session-1", name: "Only", isRecent: true, status: "ready" }],
			"session-1",
			"session-1",
		),
		null,
	);
});

test("toSessionSummaries sorts all sessions by createdAt descending", () => {
	const summaries = toSessionSummaries([
		makeSession({
			id: "session-1",
			name: "Oldest",
			createdAt: "2026-01-01T00:00:00Z",
		}),
		makeSession({
			id: "session-2",
			name: "Newest",
			createdAt: "2026-01-03T00:00:00Z",
		}),
		makeSession({
			id: "session-3",
			name: "Middle",
			createdAt: "2026-01-02T00:00:00Z",
		}),
	]);

	assert.deepEqual(
		summaries.map((session) => session.id),
		["session-2", "session-3", "session-1"],
	);
	assert.ok(summaries.every((session) => session.isRecent === false));
	assert.equal(summaries[0]?.workspaceId, undefined);
});

test("toSessionSummaries preserves workspace ids", () => {
	const summaries = toSessionSummaries([
		makeSession({
			id: "session-1",
			name: "Workspace session",
			workspaceId: "workspace-1",
		}),
	]);

	assert.equal(summaries[0]?.workspaceId, "workspace-1");
});

test("upsertSession replaces an existing session in place", () => {
	const existingSessions: Session[] = [
		makeSession({
			id: "session-1",
			name: "One",
			createdAt: "2026-01-01T00:00:00Z",
		}),
		makeSession({
			id: "session-2",
			name: "Two",
			createdAt: "2026-01-02T00:00:00Z",
		}),
	];

	const nextSession: Session = {
		...existingSessions[1],
		status: "error",
		errorMessage: "boom",
	};

	assert.deepEqual(upsertSession(existingSessions, nextSession), [
		existingSessions[0],
		nextSession,
	]);
});

test("upsertSession appends a newly seen session", () => {
	const existingSessions: Session[] = [
		makeSession({
			id: "session-1",
			name: "One",
			createdAt: "2026-01-01T00:00:00Z",
		}),
	];

	const nextSession: Session = makeSession({
		id: "session-2",
		name: "Two",
		createdAt: "2026-01-02T00:00:00Z",
		timestamp: "2026-01-02T00:00:00Z",
	});

	assert.deepEqual(upsertSession(existingSessions, nextSession), [
		existingSessions[0],
		nextSession,
	]);
});

test("touchRecentThread moves existing entries to the head of the recent list", () => {
	const nextEntries = touchRecentThread(
		[
			{
				sessionId: "session-2",
				sessionName: "Two",
				threadId: "thread-2",
				threadName: "Thread Two",
				lastAccessedAt: "2026-01-02T00:00:00Z",
			},
			{
				sessionId: "session-1",
				sessionName: "One",
				threadId: "thread-1",
				threadName: "Thread One",
				lastAccessedAt: "2026-01-01T00:00:00Z",
			},
		],
		{
			sessionId: "session-1",
			sessionName: "One renamed",
			threadId: "thread-1",
			threadName: "Thread One renamed",
			lastMessage: "latest prompt",
		},
		"2026-02-01T00:00:00Z",
	);

	assert.deepEqual(nextEntries, [
		{
			sessionId: "session-1",
			sessionName: "One renamed",
			threadId: "thread-1",
			threadName: "Thread One renamed",
			lastMessage: "latest prompt",
			lastAccessedAt: "2026-02-01T00:00:00Z",
		},
		{
			sessionId: "session-2",
			sessionName: "Two",
			threadId: "thread-2",
			threadName: "Thread Two",
			lastAccessedAt: "2026-01-02T00:00:00Z",
		},
	]);
});

test("touchRecentThread inserts unseen entries at the head of the recent list", () => {
	let nextEntries = [
		{
			sessionId: "session-1",
			sessionName: "One",
			threadId: "thread-1",
			threadName: "Thread One",
			lastAccessedAt: "2026-01-01T00:00:00Z",
		},
		{
			sessionId: "session-2",
			sessionName: "Two",
			threadId: "thread-2",
			threadName: "Thread Two",
			lastAccessedAt: "2026-01-02T00:00:00Z",
		},
		{
			sessionId: "session-3",
			sessionName: "Three",
			threadId: "thread-3",
			threadName: "Thread Three",
			lastAccessedAt: "2026-01-03T00:00:00Z",
		},
		{
			sessionId: "session-4",
			sessionName: "Four",
			threadId: "thread-4",
			threadName: "Thread Four",
			lastAccessedAt: "2026-01-04T00:00:00Z",
		},
	];

	nextEntries = touchRecentThread(
		nextEntries,
		{
			sessionId: "session-5",
			sessionName: "Five",
			threadId: "thread-5",
			threadName: "Thread Five",
			lastMessage: "",
		},
		"2026-02-01T00:00:00Z",
	);

	assert.deepEqual(nextEntries, [
		{
			sessionId: "session-5",
			sessionName: "Five",
			threadId: "thread-5",
			threadName: "Thread Five",
			lastAccessedAt: "2026-02-01T00:00:00Z",
		},
		{
			sessionId: "session-1",
			sessionName: "One",
			threadId: "thread-1",
			threadName: "Thread One",
			lastAccessedAt: "2026-01-01T00:00:00Z",
		},
		{
			sessionId: "session-2",
			sessionName: "Two",
			threadId: "thread-2",
			threadName: "Thread Two",
			lastAccessedAt: "2026-01-02T00:00:00Z",
		},
		{
			sessionId: "session-3",
			sessionName: "Three",
			threadId: "thread-3",
			threadName: "Thread Three",
			lastAccessedAt: "2026-01-03T00:00:00Z",
		},
		{
			sessionId: "session-4",
			sessionName: "Four",
			threadId: "thread-4",
			threadName: "Thread Four",
			lastAccessedAt: "2026-01-04T00:00:00Z",
		},
	]);
});

test("touchRecentThread trims the oldest entry while preserving list order", () => {
	const nextEntries = touchRecentThread(
		Array.from({ length: 12 }, (_, index) => ({
			sessionId: `session-${index + 1}`,
			sessionName: `Session ${index + 1}`,
			threadId: `thread-${index + 1}`,
			threadName: `Thread ${index + 1}`,
			lastAccessedAt: `2026-01-${String(index + 1).padStart(2, "0")}T00:00:00Z`,
		})),
		{
			sessionId: "session-13",
			sessionName: "Session 13",
			threadId: "thread-13",
			threadName: "Thread 13",
			lastMessage: "",
		},
		"2026-02-01T00:00:00Z",
	);

	assert.equal(nextEntries.length, 12);
	assert.equal(nextEntries[0]?.threadId, "thread-13");
	assert.equal(
		nextEntries.some((entry) => entry.threadId === "thread-1"),
		false,
	);
	assert.equal(nextEntries[1]?.threadId, "thread-2");
});

test("getRecentThreadEntryForSessionSelection returns the selected thread", () => {
	assert.deepEqual(
		getRecentThreadEntryForSessionSelection({
			session: { id: "session-1", name: "Session One" },
			sessionContext: {
				threads: {
					selectedId: "thread-2",
					list: [
						{ id: "thread-1", name: "Thread One", lastMessage: "first" },
						{
							id: "thread-2",
							name: "Thread Two",
							state: "interrupted",
							lastMessage: "second",
						},
					],
				},
			},
		}),
		{
			sessionId: "session-1",
			sessionName: "Session One",
			threadId: "thread-2",
			threadName: "Thread Two",
			state: "interrupted",
			lastMessage: "second",
		},
	);
});

test("getRecentThreadEntryForSessionSelection returns null without a resolvable thread", () => {
	assert.equal(
		getRecentThreadEntryForSessionSelection({
			session: { id: "session-1", name: "Session One" },
			sessionContext: {
				threads: {
					selectedId: null,
					list: [{ id: "thread-1", name: "Thread One" }],
				},
			},
		}),
		null,
	);
});

test("refreshRecentThread updates labels without adding missing threads", () => {
	const existingEntries = [
		{
			sessionId: "session-1",
			sessionName: "One",
			threadId: "thread-1",
			threadName: "Thread One",
			lastAccessedAt: "2026-01-01T00:00:00Z",
		},
	];

	assert.deepEqual(
		refreshRecentThread(existingEntries, {
			sessionId: "session-1",
			sessionName: "One renamed",
			threadId: "thread-1",
			threadName: "Thread One renamed",
			lastMessage: "",
		}),
		[
			{
				sessionId: "session-1",
				sessionName: "One renamed",
				threadId: "thread-1",
				threadName: "Thread One renamed",
				lastAccessedAt: "2026-01-01T00:00:00Z",
			},
		],
	);
	assert.deepEqual(
		refreshRecentThread(existingEntries, {
			sessionId: "session-2",
			sessionName: "Two",
			threadId: "thread-2",
			threadName: "Thread Two",
			lastMessage: "",
		}),
		existingEntries,
	);
});

test("reconcileRecentThreadsWithSessions removes deleted sessions", () => {
	assert.deepEqual(
		reconcileRecentThreadsWithSessions(
			[
				{
					sessionId: "session-1",
					sessionName: "One",
					threadId: "thread-1",
					threadName: "Thread One",
					lastAccessedAt: "2026-01-01T00:00:00Z",
				},
				{
					sessionId: "session-2",
					sessionName: "Two",
					threadId: "thread-2",
					threadName: "Thread Two",
					lastAccessedAt: "2026-01-02T00:00:00Z",
				},
			],
			["session-2"],
		),
		[
			{
				sessionId: "session-2",
				sessionName: "Two",
				threadId: "thread-2",
				threadName: "Thread Two",
				lastAccessedAt: "2026-01-02T00:00:00Z",
			},
		],
	);
});

test("toRecentThreadSummaries preserves stored recent-thread order", () => {
	assert.deepEqual(
		toRecentThreadSummaries(
			[
				{ id: "session-3", name: "Three", isRecent: false, status: "ready" },
				{ id: "session-2", name: "Two", isRecent: false, status: "error" },
				{ id: "session-1", name: "One", isRecent: false, status: "ready" },
			],
			[
				{
					sessionId: "session-1",
					sessionName: "One old",
					threadId: "thread-1",
					threadName: "Thread One",
					lastMessage: "First prompt",
					lastAccessedAt: "2026-01-01T00:00:00Z",
				},
				{
					sessionId: "session-3",
					sessionName: "Three old",
					threadId: "thread-3",
					threadName: "Thread Three",
					lastMessage: "Third prompt",
					lastAccessedAt: "2026-01-03T00:00:00Z",
				},
			],
		),
		[
			{
				sessionId: "session-1",
				sessionName: "One",
				sessionStatus: "ready",
				threadId: "thread-1",
				threadName: "Thread One",
				lastMessage: "First prompt",
				lastAccessedAt: "2026-01-01T00:00:00Z",
			},
			{
				sessionId: "session-3",
				sessionName: "Three",
				sessionStatus: "ready",
				threadId: "thread-3",
				threadName: "Thread Three",
				lastMessage: "Third prompt",
				lastAccessedAt: "2026-01-03T00:00:00Z",
			},
		],
	);
});

test("getVisibleRecentThreads picks the newest entries while preserving stable list order", () => {
	assert.deepEqual(
		getVisibleRecentThreads(
			[
				{
					sessionId: "session-1",
					sessionName: "One",
					sessionStatus: "ready",
					threadId: "thread-1",
					threadName: "Thread One",
					lastAccessedAt: "2026-01-01T00:00:00Z",
				},
				{
					sessionId: "session-2",
					sessionName: "Two",
					sessionStatus: "ready",
					threadId: "thread-2",
					threadName: "Thread Two",
					lastAccessedAt: "2026-01-05T00:00:00Z",
				},
				{
					sessionId: "session-3",
					sessionName: "Three",
					sessionStatus: "ready",
					threadId: "thread-3",
					threadName: "Thread Three",
					lastAccessedAt: "2026-01-02T00:00:00Z",
				},
				{
					sessionId: "session-4",
					sessionName: "Four",
					sessionStatus: "ready",
					threadId: "thread-4",
					threadName: "Thread Four",
					lastAccessedAt: "2026-01-04T00:00:00Z",
				},
				{
					sessionId: "session-5",
					sessionName: "Five",
					sessionStatus: "ready",
					threadId: "thread-5",
					threadName: "Thread Five",
					lastAccessedAt: "2026-01-03T00:00:00Z",
				},
			],
			4,
		).map((thread) => thread.threadId),
		["thread-2", "thread-3", "thread-4", "thread-5"],
	);
});

test("visible recent sessions mount from the rendered recent subset rather than the first stored entries", () => {
	const visibleRecentThreads = getVisibleRecentThreads(
		[
			{
				sessionId: "session-1",
				threadId: "thread-1",
				sessionName: "One",
				sessionStatus: "ready",
				threadName: "Thread One",
				lastMessage: "one",
				lastAccessedAt: "2026-01-01T00:00:00Z",
			},
			{
				sessionId: "session-2",
				threadId: "thread-2",
				sessionName: "Two",
				sessionStatus: "ready",
				threadName: "Thread Two",
				lastMessage: "two",
				lastAccessedAt: "2026-01-05T00:00:00Z",
			},
			{
				sessionId: "session-3",
				threadId: "thread-3",
				sessionName: "Three",
				sessionStatus: "ready",
				threadName: "Thread Three",
				lastMessage: "three",
				lastAccessedAt: "2026-01-02T00:00:00Z",
			},
			{
				sessionId: "session-4",
				threadId: "thread-4",
				sessionName: "Four",
				sessionStatus: "ready",
				threadName: "Thread Four",
				lastMessage: "four",
				lastAccessedAt: "2026-01-06T00:00:00Z",
			},
			{
				sessionId: "session-5",
				threadId: "thread-5",
				sessionName: "Five",
				sessionStatus: "ready",
				threadName: "Thread Five",
				lastMessage: "five",
				lastAccessedAt: "2026-01-03T00:00:00Z",
			},
			{
				sessionId: "session-6",
				threadId: "thread-6",
				sessionName: "Six",
				sessionStatus: "ready",
				threadName: "Thread Six",
				lastMessage: "six",
				lastAccessedAt: "2026-01-04T00:00:00Z",
			},
		],
		4,
	);

	assert.deepEqual(
		visibleRecentThreads.map((thread) => thread.sessionId),
		["session-2", "session-4", "session-5", "session-6"],
	);
	assert.deepEqual(getMountedSessionIds(null, visibleRecentThreads, 4), [
		"session-2",
		"session-4",
		"session-5",
		"session-6",
	]);
});

test("getMountedSessionIds keeps the selected session and fills unique recent sessions", () => {
	assert.deepEqual(
		getMountedSessionIds(
			"session-5",
			[
				{ sessionId: "session-4" },
				{ sessionId: "session-3" },
				{ sessionId: "session-3" },
				{ sessionId: "session-2" },
				{ sessionId: "session-1" },
			],
			4,
		),
		["session-5", "session-4", "session-3", "session-2"],
	);
});

test("getMountedSessionIds does not spend a slot on a pending selection", () => {
	assert.deepEqual(
		getMountedSessionIds(
			null,
			[
				{ sessionId: "session-4" },
				{ sessionId: "session-3" },
				{ sessionId: "session-2" },
				{ sessionId: "session-1" },
				{ sessionId: "session-0" },
			],
			4,
		),
		["session-4", "session-3", "session-2", "session-1"],
	);
});

test("removeRecentThread and removeRecentThreadsForSession prune entries", () => {
	const entries = [
		{
			sessionId: "session-1",
			sessionName: "One",
			threadId: "thread-1",
			threadName: "Thread One",
			lastAccessedAt: "2026-01-01T00:00:00Z",
		},
		{
			sessionId: "session-1",
			sessionName: "One",
			threadId: "thread-2",
			threadName: "Thread Two",
			lastAccessedAt: "2026-01-02T00:00:00Z",
		},
		{
			sessionId: "session-2",
			sessionName: "Two",
			threadId: "thread-3",
			threadName: "Thread Three",
			lastAccessedAt: "2026-01-03T00:00:00Z",
		},
	];

	assert.deepEqual(removeRecentThread(entries, "session-1", "thread-2"), [
		{
			sessionId: "session-1",
			sessionName: "One",
			threadId: "thread-1",
			threadName: "Thread One",
			lastAccessedAt: "2026-01-01T00:00:00Z",
		},
		{
			sessionId: "session-2",
			sessionName: "Two",
			threadId: "thread-3",
			threadName: "Thread Three",
			lastAccessedAt: "2026-01-03T00:00:00Z",
		},
	]);
	assert.deepEqual(removeRecentThreadsForSession(entries, "session-1"), [
		{
			sessionId: "session-2",
			sessionName: "Two",
			threadId: "thread-3",
			threadName: "Thread Three",
			lastAccessedAt: "2026-01-03T00:00:00Z",
		},
	]);
});

test("recent threads persist in local storage without reordering duplicates", () => {
	withLocalStorage(() => {
		let entries = touchRecentThread(
			[],
			{
				sessionId: "session-1",
				sessionName: "One",
				threadId: "thread-1",
				threadName: "Thread One",
				lastMessage: "first prompt",
			},
			"2026-01-01T00:00:00Z",
		);
		entries = touchRecentThread(
			entries,
			{
				sessionId: "session-2",
				sessionName: "Two",
				threadId: "thread-2",
				threadName: "Thread Two",
				lastMessage: "second prompt",
			},
			"2026-01-02T00:00:00Z",
		);
		entries = touchRecentThread(
			entries,
			{
				sessionId: "session-1",
				sessionName: "One updated",
				threadId: "thread-1",
				threadName: "Thread One updated",
				lastMessage: "updated prompt",
			},
			"2026-02-01T00:00:00Z",
		);
		writeRecentThreadEntries(entries);

		assert.equal(
			(globalThis.window as Window).localStorage.getItem(
				RECENT_THREADS_STORAGE_KEY,
			),
			JSON.stringify([
				{
					sessionId: "session-1",
					sessionName: "One updated",
					threadId: "thread-1",
					threadName: "Thread One updated",
					lastMessage: "updated prompt",
					lastAccessedAt: "2026-02-01T00:00:00Z",
				},
				{
					sessionId: "session-2",
					sessionName: "Two",
					threadId: "thread-2",
					threadName: "Thread Two",
					lastMessage: "second prompt",
					lastAccessedAt: "2026-01-02T00:00:00Z",
				},
			]),
		);
		assert.deepEqual(readRecentThreadEntries(), [
			{
				sessionId: "session-1",
				sessionName: "One updated",
				threadId: "thread-1",
				threadName: "Thread One updated",
				lastMessage: "updated prompt",
				lastAccessedAt: "2026-02-01T00:00:00Z",
			},
			{
				sessionId: "session-2",
				sessionName: "Two",
				threadId: "thread-2",
				threadName: "Thread Two",
				lastMessage: "second prompt",
				lastAccessedAt: "2026-01-02T00:00:00Z",
			},
		]);
	});
});

test("readRecentThreadEntries migrates stored entries without lastMessage", () => {
	withLocalStorage((storage) => {
		storage.setItem(
			RECENT_THREADS_STORAGE_KEY,
			JSON.stringify([
				{
					sessionId: "session-1",
					sessionName: "One",
					threadId: "thread-1",
					threadName: "Thread One",
					lastAccessedAt: "2026-01-01T00:00:00Z",
				},
			]),
		);

		assert.deepEqual(readRecentThreadEntries(), [
			{
				sessionId: "session-1",
				sessionName: "One",
				threadId: "thread-1",
				threadName: "Thread One",
				lastMessage: "",
				lastAccessedAt: "2026-01-01T00:00:00Z",
			},
		]);
	});
});

test("readRecentThreadEntries ignores malformed stored values", () => {
	withLocalStorage((storage) => {
		storage.setItem(
			RECENT_THREADS_STORAGE_KEY,
			JSON.stringify([{ bad: true }, "nope"]),
		);
		assert.deepEqual(readRecentThreadEntries(), []);

		storage.setItem(RECENT_THREADS_STORAGE_KEY, "not json");
		assert.deepEqual(readRecentThreadEntries(), []);
	});
});
