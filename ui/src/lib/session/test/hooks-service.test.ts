import assert from "node:assert/strict";
import test from "node:test";

import { createSessionHooksService } from "../services/hooks-service";
import type { SessionData } from "../../shell-types";

function makeSession(): SessionData {
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
		threads: [{ id: "thread-1", name: "Thread 1" }],
		hooksStatus: {
			hooks: [
				{
					hookId: "hook-1",
					hookName: "Format check",
					type: "pre_tool_use",
					lastResult: "failure",
					runCount: 1,
					failCount: 1,
				},
			],
			pendingHookIds: [],
		},
		hookOutputById: {
			"hook-1": "previous output",
		},
		editorFiles: ["src/app.ts"],
		fileContents: {
			"src/app.ts": "export const ok = true;",
		},
		services: [],
	};
}

test("hooks service rerun transitions a hook from running to success", async () => {
		let sessionDataById: Record<string, SessionData> = {
			"session-1": makeSession(),
		};
		let tick = 0;

		const service = createSessionHooksService({
			getSessionId: () => "session-1",
			getSessionDataById: () => sessionDataById,
			setSessionDataById: (value) => {
				sessionDataById = value;
			},
			getStatus: () => sessionDataById["session-1"].hooksStatus ?? { hooks: [], pendingHookIds: [] },
			getOutputById: () => sessionDataById["session-1"].hookOutputById ?? {},
			nowIsoString: () => `2026-03-11T00:00:0${tick++}.000Z`,
		});

		service.rerun("hook-1");

		assert.equal(service.status.hooks[0].lastResult, "running");
		assert.equal(service.status.hooks[0].runCount, 2);
		assert.match(service.outputById["hook-1"], /rerun requested/);

		await new Promise((resolve) => setTimeout(resolve, 950));

		assert.equal(service.status.hooks[0].lastResult, "success");
		assert.equal(service.status.hooks[0].lastExitCode, 0);
		assert.match(service.outputById["hook-1"], /completed successfully/);
});
