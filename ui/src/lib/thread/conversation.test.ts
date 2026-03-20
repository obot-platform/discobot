import assert from "node:assert/strict";
import test from "node:test";

import type { ChatMessage } from "$lib/api-types";
import { getSubmitMessages } from "./conversation.svelte";

function makeUserMessage(text: string, provisional = false): ChatMessage {
	return {
		id: "user-1",
		role: "user",
		parts: [{ type: "text", text }],
		...(provisional ? { provisional: true } : {}),
	};
}

test("getSubmitMessages only includes the latest user message", () => {
	const userMessage = makeUserMessage("latest prompt");

	assert.deepEqual(getSubmitMessages(userMessage), [userMessage]);
});

test("getSubmitMessages strips the provisional flag", () => {
	const userMessage = makeUserMessage("latest prompt", true);

	assert.deepEqual(getSubmitMessages(userMessage), [
		{
			id: "user-1",
			role: "user",
			parts: [{ type: "text", text: "latest prompt" }],
		},
	]);
});
