import assert from "node:assert/strict";
import test from "node:test";

import { getNextSelectedSessionId } from "../domains/app-sessions.helpers";
import type { SessionSummary } from "../../shell-types";

const sessions: SessionSummary[] = [
	{ id: "session-1", name: "One", isRecent: true, status: "ready" },
	{ id: "session-2", name: "Two", isRecent: true, status: "running" },
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
