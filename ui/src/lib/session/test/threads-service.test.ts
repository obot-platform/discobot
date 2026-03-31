import assert from "node:assert/strict";
import test from "node:test";

import type { Session } from "../../api-types";
import {
	buildImplicitThread,
	getNextSelectedThreadId,
} from "../domains/session-domain.helpers";

test("getNextSelectedThreadId picks the next thread after removal", () => {
	const nextId = getNextSelectedThreadId(
		[
			{ id: "a", name: "A", mode: "build" },
			{ id: "b", name: "B", mode: "build" },
			{ id: "c", name: "C", mode: "build" },
		],
		"b",
		"b",
	);

	assert.equal(nextId, "c");
});

test("getNextSelectedThreadId falls back to the previous thread", () => {
	const nextId = getNextSelectedThreadId(
		[
			{ id: "a", name: "A", mode: "build" },
			{ id: "b", name: "B", mode: "build" },
		],
		"b",
		"b",
	);

	assert.equal(nextId, "a");
});

test("buildImplicitThread derives a single thread from the current session", () => {
	const session: Session = {
		id: "session-1",
		name: "Session",
		displayName: "Friendly session",
		description: "",
		createdAt: "2026-03-10T00:00:00.000Z",
		timestamp: "2026-03-11T00:00:00.000Z",
		status: "ready",
		files: [],
		model: "openai/gpt-5",
		reasoning: "enabled",
		mode: "plan",
	};

	assert.deepEqual(buildImplicitThread(session), [
		{
			id: "session-1",
			name: "Friendly session",
			model: "openai/gpt-5",
			reasoning: "enabled",
			mode: "plan",
			promptQueue: [],
		},
	]);
});
