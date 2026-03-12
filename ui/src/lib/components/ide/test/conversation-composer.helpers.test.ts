import assert from "node:assert/strict";
import test from "node:test";

import {
	buildEmptySessionStartChatRequest,
	shouldSubmitComposerOnEnter,
} from "../conversation-composer.helpers";

test("shouldSubmitComposerOnEnter only allows non-whitespace drafts", () => {
	assert.equal(shouldSubmitComposerOnEnter(""), false);
	assert.equal(shouldSubmitComposerOnEnter("   \n\t"), false);
	assert.equal(shouldSubmitComposerOnEnter("hello"), true);
});

test("buildEmptySessionStartChatRequest normalizes plan and thinking selections", () => {
	assert.deepEqual(
		buildEmptySessionStartChatRequest({
			sessionId: "session-123",
			workspaceId: "workspace-123",
			mode: "plan",
			modelId: "claude-sonnet:thinking",
			reasoning: true,
		}),
		{
			sessionId: "session-123",
			workspaceId: "workspace-123",
			messages: [],
			model: "claude-sonnet",
			reasoning: "enabled",
			mode: "plan",
		},
	);
});

test("buildEmptySessionStartChatRequest omits optional workspace and model values", () => {
	assert.deepEqual(
		buildEmptySessionStartChatRequest({
			sessionId: "session-456",
			workspaceId: null,
			mode: "build",
			modelId: null,
			reasoning: false,
		}),
		{
			sessionId: "session-456",
			messages: [],
			reasoning: "disabled",
			mode: "",
		},
	);
});
