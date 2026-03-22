import assert from "node:assert/strict";
import test from "node:test";

import { getReconciledSelectedSessionId } from "../domains/app-sessions.helpers";
import type { SessionSummary } from "$lib/shell-types";

const sessions: SessionSummary[] = [
	{ id: "session-1", name: "One", isRecent: true, status: "ready" },
	{ id: "session-2", name: "Two", isRecent: false, status: "ready" },
];

test("getReconciledSelectedSessionId prefers an explicit valid session", () => {
	assert.equal(
		getReconciledSelectedSessionId(sessions, "session-1", "session-2"),
		"session-2",
	);
});

test("getReconciledSelectedSessionId keeps the current valid selection", () => {
	assert.equal(getReconciledSelectedSessionId(sessions, "session-1"), "session-1");
});

test("getReconciledSelectedSessionId clears invalid selections", () => {
	assert.equal(getReconciledSelectedSessionId(sessions, "missing-session"), null);
});
