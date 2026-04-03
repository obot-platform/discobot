import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";

import type { HooksStatusResponse } from "../../api-types";
import {
	getHookDisplayState,
	mergeHookOutput,
	toHooksStatus,
} from "../domains/session-domain.helpers";

const CONVERSATION_HOOKS_PANEL_COMPONENT = path.resolve(
	import.meta.dirname,
	"../../components/app/ConversationHooksPanel.svelte",
);

const COMPOSER_HOOKS_CONTROL_COMPONENT = path.resolve(
	import.meta.dirname,
	"../../components/app/parts/ConversationComposerHooksControl.svelte",
);

function readConversationHooksPanelSource() {
	return readFileSync(CONVERSATION_HOOKS_PANEL_COMPONENT, "utf-8");
}

function readComposerHooksControlSource() {
	return readFileSync(COMPOSER_HOOKS_CONTROL_COMPONENT, "utf-8");
}

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
		hooksStatus.hooks.map((hook) => ({
			id: hook.hookId,
			type: hook.type,
			result: hook.lastResult,
		})),
		[
			{ id: "hook-1", type: "session", result: "success" },
			{ id: "hook-2", type: "file", result: "failure" },
		],
	);
});

test("getHookDisplayState keeps failures visible when the hook is still pending", () => {
	const hooksStatus = toHooksStatus(hooksStatusResponse);
	const pendingHookIds = new Set(hooksStatus.pendingHookIds);

	assert.equal(
		getHookDisplayState(hooksStatus.hooks[1], pendingHookIds),
		"failure",
	);
	assert.equal(
		getHookDisplayState(hooksStatus.hooks[0], pendingHookIds),
		"success",
	);
});

test("conversation hooks panel uses the resolved display state for failure rendering", () => {
	const source = readConversationHooksPanelSource();

	assert.match(source, /\{@const displayState = hookDisplayState\(hook\)\}/);
	assert.match(source, /\{:else if displayState === "failure"\}/);
	assert.doesNotMatch(source, /\{:else if isHookPending\(hook\.hookId\)\}/);
});

test("composer hooks control resolves failures through shared hook display state", () => {
	const source = readComposerHooksControlSource();

	assert.match(source, /import \{ getHookDisplayState \} from/);
	assert.match(
		source,
		/getHookDisplayState\(hook, pendingHookSet\(\)\) === "failure"/,
	);
});

test("mergeHookOutput replaces the latest output for the given hook", () => {
	assert.deepEqual(
		mergeHookOutput({ "hook-1": "previous output" }, "hook-1", {
			output: "latest output",
		}),
		{ "hook-1": "latest output" },
	);
});
