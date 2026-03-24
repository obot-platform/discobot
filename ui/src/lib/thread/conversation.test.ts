import assert from "node:assert/strict";
import test from "node:test";

import type { ChatMessage } from "$lib/api-types";
import { getSubmitMessages } from "./conversation.svelte";

function makeUserMessage(
	parts: ChatMessage["parts"] = [{ type: "text", text: "latest prompt" }],
	provisional = false,
): ChatMessage {
	return {
		id: "user-1",
		role: "user",
		parts,
		...(provisional ? { provisional: true } : {}),
	};
}

test("getSubmitMessages only includes the latest user message", () => {
	const userMessage = makeUserMessage();

	assert.deepEqual(getSubmitMessages(userMessage), [userMessage]);
});

test("getSubmitMessages strips the provisional flag", () => {
	const userMessage = makeUserMessage(
		[{ type: "text", text: "latest prompt" }],
		true,
	);

	assert.deepEqual(getSubmitMessages(userMessage), [
		{
			id: "user-1",
			role: "user",
			parts: [{ type: "text", text: "latest prompt" }],
		},
	]);
});

test("getSubmitMessages preserves attachment parts", () => {
	const userMessage = makeUserMessage(
		[
			{ type: "text", text: "latest prompt" },
			{
				type: "file",
				filename: "preview.png",
				mediaType: "image/png",
				url: "data:image/png;base64,abc123",
			},
		],
		true,
	);

	assert.deepEqual(getSubmitMessages(userMessage), [
		{
			id: "user-1",
			role: "user",
			parts: [
				{ type: "text", text: "latest prompt" },
				{
					type: "file",
					filename: "preview.png",
					mediaType: "image/png",
					url: "data:image/png;base64,abc123",
				},
			],
		},
	]);
});
