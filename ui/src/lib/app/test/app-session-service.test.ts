import assert from "node:assert/strict";
import test from "node:test";

import type { Session } from "$lib/api-types";
import type { SessionSummary } from "$lib/shell-types";
import {
	PREFERRED_IDE_STORAGE_KEY,
	RECENT_THREADS_STORAGE_KEY,
	readPreferredIde,
	reconcileRecentThreadsForSession,
	reconcileRecentThreadsWithSessions,
	readRecentThreadEntries,
	refreshRecentThread,
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

test("touchRecentThread updates timestamps without reordering existing entries", () => {
	const nextEntries = touchRecentThread(
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
		{
			sessionId: "session-1",
			sessionName: "One renamed",
			threadId: "thread-1",
			threadName: "Thread One renamed",
		},
		"2026-02-01T00:00:00Z",
	);

	assert.deepEqual(nextEntries, [
		{
			sessionId: "session-1",
			sessionName: "One renamed",
			threadId: "thread-1",
			threadName: "Thread One renamed",
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

test("touchRecentThread appends unseen entries and evicts the oldest when full", () => {
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
		},
		"2026-02-01T00:00:00Z",
	);

	assert.deepEqual(nextEntries, [
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
		{
			sessionId: "session-5",
			sessionName: "Five",
			threadId: "thread-5",
			threadName: "Thread Five",
			lastAccessedAt: "2026-02-01T00:00:00Z",
		},
	]);
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
		}),
		existingEntries,
	);
});

test("reconcileRecentThreadsForSession removes stale thread ids", () => {
	assert.deepEqual(
		reconcileRecentThreadsForSession(
			[
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
			],
			"session-1",
			["thread-2"],
		),
		[
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
		],
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

test("toRecentThreadSummaries preserves stored insertion order", () => {
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
