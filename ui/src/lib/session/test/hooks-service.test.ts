import assert from "node:assert/strict";
import test from "node:test";

import type { HooksStatusResponse } from "../../api-types";
import { mergeHookOutput, toHooksStatus } from "../domains/session-domain.helpers";

const hooksStatusResponse: HooksStatusResponse = {
	hooks: {
		"hook-1": {
			hookId: "hook-1",
			hookName: "Prompt",
			type: "session",
			lastRunAt: "2026-03-11T00:00:00.000Z",
			lastResult: "success",
			lastExitCode: 0,
			outputPath: "/tmp/hook-1.log",
			runCount: 2,
			failCount: 0,
			consecutiveFailures: 0,
		},
		"hook-2": {
			hookId: "hook-2",
			hookName: "Formatter",
			type: "file",
			lastRunAt: "2026-03-11T00:00:01.000Z",
			lastResult: "failure",
			lastExitCode: 1,
			outputPath: "/tmp/hook-2.log",
			runCount: 3,
			failCount: 2,
			consecutiveFailures: 1,
		},
	},
	pendingHooks: ["hook-2"],
	lastEvaluatedAt: "2026-03-11T00:00:01.000Z",
};

test("toHooksStatus maps API hook response fields into session hook state", () => {
	const hooksStatus = toHooksStatus(hooksStatusResponse);

	assert.deepEqual(hooksStatus.pendingHookIds, ["hook-2"]);
	assert.deepEqual(
		hooksStatus.hooks.map((hook) => ({ id: hook.hookId, type: hook.type, result: hook.lastResult })),
		[
			{ id: "hook-1", type: "user_prompt_submit", result: "success" },
			{ id: "hook-2", type: "post_tool_use", result: "failure" },
		],
	);
});

test("mergeHookOutput replaces the latest output for the given hook", () => {
	assert.deepEqual(
		mergeHookOutput({ "hook-1": "previous output" }, "hook-1", { output: "latest output" }),
		{ "hook-1": "latest output" },
	);
});
