import assert from "node:assert/strict";
import test from "node:test";

import { clearComposerDraftState } from "./thread-context.svelte";

test("clearComposerDraftState clears storage before resetting the in-memory draft", () => {
	const calls: string[] = [];
	let composerDraft = "Start a new session with this prompt";

	clearComposerDraftState({
		cancelPersist: () => {
			calls.push("cancel");
		},
		clearStoredDraft: () => {
			calls.push("storage");
			assert.equal(composerDraft, "Start a new session with this prompt");
		},
		clearInMemoryDraft: () => {
			calls.push("memory");
			composerDraft = "";
		},
	});

	assert.deepEqual(calls, ["cancel", "storage", "memory"]);
	assert.equal(composerDraft, "");
});
