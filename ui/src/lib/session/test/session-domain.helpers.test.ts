import assert from "node:assert/strict";
import test from "node:test";

import type { ChatMessage } from "$lib/api-types";

import {
	addToolApprovalResponse,
	createUserMessage,
} from "../domains/session-domain.helpers";

test("createUserMessage leaves provisional unset by default", () => {
	const message = createUserMessage("hello");

	assert.equal(message.role, "user");
	assert.deepEqual(message.parts, [{ type: "text", text: "hello" }]);
	assert.equal(message.provisional, undefined);
});

test("createUserMessage can mark a message as provisional", () => {
	const message = createUserMessage("hello", { provisional: true });

	assert.equal(message.role, "user");
	assert.deepEqual(message.parts, [{ type: "text", text: "hello" }]);
	assert.equal(message.provisional, true);
});

test("addToolApprovalResponse updates a pending dynamic tool in place", () => {
	const messages = [
		{
			id: "assistant-1",
			role: "assistant" as const,
			parts: [
				{
					type: "dynamic-tool" as const,
					toolCallId: "call-1",
					toolName: "AskUserQuestion",
					state: "approval-requested" as const,
					input: { questions: [] },
					approval: { id: "call-1" },
				},
			],
		},
	];

	const updated = addToolApprovalResponse(messages, { id: "call-1", approved: true });

	assert.equal(updated, true);
	assert.equal(messages[0]?.parts[0]?.type, "dynamic-tool");
	assert.equal(
		messages[0]?.parts[0]?.type === "dynamic-tool"
			? messages[0].parts[0].state
			: undefined,
		"approval-responded",
	);
	assert.deepEqual(
		messages[0]?.parts[0]?.type === "dynamic-tool"
			? messages[0].parts[0].approval
			: undefined,
		{ id: "call-1", approved: true },
	);
});

test("addToolApprovalResponse ignores already-resolved tools", () => {
	const messages: ChatMessage[] = [
		{
			id: "assistant-1",
			role: "assistant",
			parts: [
				{
					type: "dynamic-tool",
					toolCallId: "call-1",
					toolName: "AskUserQuestion",
					state: "output-available",
					input: { questions: [] },
					output: [],
					approval: { id: "call-1", approved: true },
				},
			],
		},
	] as ChatMessage[];

	const updated = addToolApprovalResponse(messages, { id: "call-1", approved: true });

	assert.equal(updated, false);
	assert.equal(messages[0]?.parts[0]?.type, "dynamic-tool");
	assert.equal(
		messages[0]?.parts[0]?.type === "dynamic-tool"
			? messages[0].parts[0].state
			: undefined,
		"output-available",
	);
});
