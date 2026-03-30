import assert from "node:assert/strict";
import test from "node:test";

import { parseChatStreamMessageValue } from "./conversation-stream.events";

test("parseChatStreamMessageValue accepts current AI SDK assistant parts", async () => {
	const parsed = await parseChatStreamMessageValue({
		id: "assistant-1",
		role: "assistant",
		parts: [
			{
				type: "reasoning",
				text: "Thinking through the flow.",
				state: "done",
			},
			{
				type: "dynamic-tool",
				toolCallId: "tool-1",
				toolName: "AskUserQuestion",
				state: "approval-requested",
				input: { questions: [] },
				approval: { id: "approval-1" },
			},
			{ type: "step-start" },
			{
				type: "text",
				text: "Waiting for approval.",
				state: "done",
			},
		],
	});

	assert.equal(parsed.id, "assistant-1");
	assert.deepEqual(
		parsed.parts.map((part) => part.type),
		["reasoning", "dynamic-tool", "step-start", "text"],
	);
});

test("parseChatStreamMessageValue normalizes legacy tool-call parts", async () => {
	const parsed = await parseChatStreamMessageValue({
		id: "assistant-legacy",
		role: "assistant",
		parts: [
			{
				type: "reasoning",
				text: "Planning the next step.",
				state: "done",
			},
			{
				type: "tool-call",
				toolCallId: "tool-legacy-1",
				toolName: "EnterPlanMode",
				input: {},
			},
		],
	});

	assert.equal(parsed.parts[1]?.type, "dynamic-tool");
	assert.deepEqual(parsed.parts[1], {
		type: "dynamic-tool",
		toolCallId: "tool-legacy-1",
		toolName: "EnterPlanMode",
		state: "input-available",
		input: {},
	});
});

test("parseChatStreamMessageValue accepts data-* parts", async () => {
	const parsed = await parseChatStreamMessageValue({
		id: "assistant-2",
		role: "assistant",
		parts: [
			{
				type: "data-thread-update",
				data: {
					thread: { id: "thread-1", name: "Thread 1", mode: "build" },
				},
			},
		],
	});

	assert.equal(parsed.parts[0]?.type, "data-thread-update");
});

test("parseChatStreamMessageValue rejects unsupported part types", async () => {
	await assert.rejects(() =>
		parseChatStreamMessageValue({
			id: "assistant-3",
			role: "assistant",
			parts: [{ type: "custom-part", value: "nope" }],
		}),
	);
});
