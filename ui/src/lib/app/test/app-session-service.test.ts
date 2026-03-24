import assert from "node:assert/strict";
import test from "node:test";

import type { Session } from "$lib/api-types";
import type { SessionSummary } from "$lib/shell-types";
import { toRecentSessionSummaries, toSessionSummaries } from "../app-helpers";
import {
	getNextSelectedSessionId,
	upsertSession,
} from "../domains/app-sessions.helpers";

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
});

test("toRecentSessionSummaries sorts recent sessions by updated time", () => {
	const summaries = toRecentSessionSummaries([
		makeSession({
			id: "session-1",
			name: "Created first, updated last",
			createdAt: "2026-01-01T00:00:00Z",
			timestamp: "2026-01-04T00:00:00Z",
		}),
		makeSession({
			id: "session-2",
			name: "Created last, updated first",
			createdAt: "2026-01-04T00:00:00Z",
			timestamp: "2026-01-01T00:00:00Z",
		}),
		makeSession({
			id: "session-3",
			name: "Middle",
			createdAt: "2026-01-02T00:00:00Z",
			timestamp: "2026-01-03T00:00:00Z",
		}),
	]);

	assert.deepEqual(
		summaries.map((session) => session.id),
		["session-1", "session-3", "session-2"],
	);
	assert.ok(summaries.every((session) => session.isRecent));
});

test("upsertSession replaces an existing session in place", () => {
	const existingSessions: Session[] = [
		makeSession({
			id: "session-1",
			name: "One",
			createdAt: "2026-01-01T00:00:00Z",
			timestamp: "2026-01-01T00:00:00Z",
		}),
		makeSession({
			id: "session-2",
			name: "Two",
			createdAt: "2026-01-02T00:00:00Z",
			timestamp: "2026-01-02T00:00:00Z",
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
			timestamp: "2026-01-01T00:00:00Z",
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
