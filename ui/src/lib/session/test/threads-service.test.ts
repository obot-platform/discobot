import assert from "node:assert/strict";
import test from "node:test";

import { createSessionThreadService, getNextSelectedThreadId } from "../services/threads-service";
import type { SessionData } from "../../shell-types";

function makeSession(threadIds: string[]): SessionData {
	return {
		id: "session-1",
		name: "Session",
		description: "",
		timestamp: "2026-03-11T00:00:00.000Z",
		status: "ready",
		files: [],
		baseBranch: "main",
		baseCommit: "abcdef0",
		references: {
			issueReference: "",
			pullRequestReference: "",
		},
		threads: threadIds.map((id, index) => ({
			id,
			name: `Thread ${index + 1}`,
		})),
		editorFiles: ["src/app.ts"],
		fileContents: {
			"src/app.ts": "export const ok = true;",
		},
		services: [],
	};
}

test("getNextSelectedThreadId picks the next thread after removal", () => {
		const nextId = getNextSelectedThreadId(
			[
				{ id: "a", name: "A" },
				{ id: "b", name: "B" },
				{ id: "c", name: "C" },
			],
			"b",
			"b",
		);

		assert.equal(nextId, "c");
});

test("getNextSelectedThreadId falls back to the previous thread", () => {
		const nextId = getNextSelectedThreadId(
			[
				{ id: "a", name: "A" },
				{ id: "b", name: "B" },
			],
			"b",
			"b",
		);

		assert.equal(nextId, "a");
});

test("thread service creates and selects a new thread", () => {
		let sessionDataById: Record<string, SessionData> = {
			"session-1": makeSession(["thread-1"]),
		};
		let selectedId: string | null = "thread-1";

		const service = createSessionThreadService({
			getSessionId: () => "session-1",
			getSessionDataById: () => sessionDataById,
			setSessionDataById: (value) => {
				sessionDataById = value;
			},
			getList: () => sessionDataById["session-1"].threads,
			getSelectedId: () => selectedId,
			setSelectedId: (value) => {
				selectedId = value;
			},
			createThreadId: () => "thread-2",
		});

		service.create("Research");

		assert.deepEqual(sessionDataById["session-1"].threads, [
			{ id: "thread-1", name: "Thread 1" },
			{ id: "thread-2", name: "Research" },
		]);
		assert.equal(selectedId, "thread-2");
});

test("thread service removes the selected thread and selects a fallback", () => {
		let sessionDataById: Record<string, SessionData> = {
			"session-1": makeSession(["thread-1", "thread-2", "thread-3"]),
		};
		let selectedId: string | null = "thread-2";

		const service = createSessionThreadService({
			getSessionId: () => "session-1",
			getSessionDataById: () => sessionDataById,
			setSessionDataById: (value) => {
				sessionDataById = value;
			},
			getList: () => sessionDataById["session-1"].threads,
			getSelectedId: () => selectedId,
			setSelectedId: (value) => {
				selectedId = value;
			},
			createThreadId: () => "thread-new",
		});

		service.remove("thread-2");

		assert.deepEqual(
			sessionDataById["session-1"].threads.map((thread) => thread.id),
			["thread-1", "thread-3"],
		);
		assert.equal(selectedId, "thread-3");
});
