import assert from "node:assert/strict";
import test from "node:test";

import { resolveOpenFileState } from "../view/create-session-view-state.svelte";

test("resolveOpenFileState clears the active file when given an empty string", () => {
	const nextState = resolveOpenFileState("", "src/app.ts", ["src/app.ts"]);

	assert.deepEqual(nextState.activeView, { kind: "file", path: "" });
	assert.equal(nextState.selectedFile, "");
});

test("resolveOpenFileState without an argument falls back to the remembered file", () => {
	const nextState = resolveOpenFileState(undefined, "src/app.ts", [
		"src/app.ts",
	]);

	assert.deepEqual(nextState.activeView, { kind: "file", path: "src/app.ts" });
	assert.equal(nextState.selectedFile, "src/app.ts");
});
