import assert from "node:assert/strict";
import test from "node:test";

import type { Session } from "../../api-types";

import { getSessionToolbarOperationState } from "../app/session-toolbar-actions";

function makeSession(overrides: Partial<Session> = {}): Session {
	return {
		id: "session-1",
		name: "Session",
		description: "",
		timestamp: new Date().toISOString(),
		status: "ready",
		files: [],
		...overrides,
	};
}

test("shows commit as the primary action when changes exist", () => {
	const state = getSessionToolbarOperationState({
		filesChanged: 3,
		session: makeSession(),
		startingOperation: null,
	});

	assert.equal(state.showSplitButton, true);
	assert.equal(state.primaryAction, "commit");
	assert.equal(state.primaryLabel, "Commit");
	assert.equal(state.secondaryAction, "rebase");
	assert.equal(state.secondaryLabel, "Rebase");
	assert.equal(state.buttonLabel, "Commit");
	assert.equal(state.showBusy, false);
});

test("shows only rebase when there are no changes", () => {
	const state = getSessionToolbarOperationState({
		filesChanged: 0,
		session: makeSession(),
		startingOperation: null,
	});

	assert.equal(state.showSplitButton, false);
	assert.equal(state.primaryAction, "rebase");
	assert.equal(state.primaryLabel, "Rebase");
	assert.equal(state.secondaryAction, null);
	assert.equal(state.secondaryLabel, null);
	assert.equal(state.buttonLabel, "Rebase");
});

test("keeps commit as the primary action while a dropdown-triggered rebase is starting", () => {
	const state = getSessionToolbarOperationState({
		filesChanged: 2,
		session: makeSession(),
		startingOperation: "rebase",
	});

	assert.equal(state.primaryAction, "commit");
	assert.equal(state.activeOperation, "rebase");
	assert.equal(state.showBusy, true);
	assert.equal(state.buttonLabel, "Rebasing...");
});

test("uses session commit operation for pending rebase updates", () => {
	const state = getSessionToolbarOperationState({
		filesChanged: 1,
		session: makeSession({
			commitStatus: "pending",
			commitOperation: "rebase",
		}),
		startingOperation: null,
	});

	assert.equal(state.showPending, true);
	assert.equal(state.activeOperation, "rebase");
	assert.equal(state.buttonLabel, "Pending...");
});
