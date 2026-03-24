import assert from "node:assert/strict";
import test from "node:test";

import {
	getToolStatusLabel,
	isToolPreparingState,
	isToolRunningState,
} from "../ai/tool/tool-status";

test("tool status labels distinguish preparing from running", () => {
	assert.equal(getToolStatusLabel("input-streaming"), "Preparing");
	assert.equal(getToolStatusLabel("input-available"), "Running");
	assert.equal(getToolStatusLabel("output-available"), "Completed");
});

test("tool status helpers treat only input-streaming as preparing", () => {
	assert.equal(isToolPreparingState("input-streaming"), true);
	assert.equal(isToolPreparingState("input-available"), false);
	assert.equal(isToolRunningState("input-streaming"), true);
	assert.equal(isToolRunningState("input-available"), true);
	assert.equal(isToolRunningState("approval-requested"), false);
});
