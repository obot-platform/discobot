import assert from "node:assert/strict";
import test from "node:test";

import type { Session } from "$lib/api-types";
import {
	getNextSelectedSessionId,
	upsertSession,
} from "../domains/app-sessions.helpers";
import type { SessionSummary } from "$lib/shell-types";

const sessions: SessionSummary[] = [
	{ id: "session-1", name: "One", isRecent: true, status: "ready" },
	{ id: "session-2", name: "Two", isRecent: true, status: "ready" },
	{ id: "session-3", name: "Three", isRecent: false, status: "error" },
];

test("getNextSelectedSessionId keeps the current selection when another session is deleted", () => {
	assert.equal(getNextSelectedSessionId(sessions, "session-1", "session-2"), "session-2");
});

test("getNextSelectedSessionId falls back to the first remaining session", () => {
	assert.equal(getNextSelectedSessionId(sessions, "session-2", "session-2"), "session-1");
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

test("upsertSession replaces an existing session in place", () => {
	const existingSessions: Session[] = [
		{
			id: "session-1",
			name: "One",
			description: "",
			timestamp: "2026-01-01T00:00:00Z",
			status: "ready",
			files: [],
		},
		{
			id: "session-2",
			name: "Two",
			description: "",
			timestamp: "2026-01-02T00:00:00Z",
			status: "ready",
			files: [],
		},
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
		{
			id: "session-1",
			name: "One",
			description: "",
			timestamp: "2026-01-01T00:00:00Z",
			status: "ready",
			files: [],
		},
	];

	const nextSession: Session = {
		id: "session-2",
		name: "Two",
		description: "",
		timestamp: "2026-01-02T00:00:00Z",
		status: "ready",
		files: [],
	};

	assert.deepEqual(upsertSession(existingSessions, nextSession), [
		existingSessions[0],
		nextSession,
	]);
});
