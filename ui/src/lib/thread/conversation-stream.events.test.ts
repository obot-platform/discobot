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

test("parseChatStreamMessageValue rejects legacy part formats", async () => {
	await assert.rejects(() =>
		parseChatStreamMessageValue({
			id: "assistant-legacy",
			role: "assistant",
			parts: [
				{
					type: "tool-call",
					toolCallId: "tool-legacy-1",
					toolName: "EnterPlanMode",
					input: {},
				},
			],
		}),
	);

	await assert.rejects(() =>
		parseChatStreamMessageValue({
			id: "assistant-legacy-approval",
			role: "assistant",
			parts: [
				{
					type: "dynamic-tool",
					toolCallId: "tool-legacy-approval-1",
					toolName: "ExitPlanMode",
					state: "output-available",
					input: {},
					output: "Plan feedback from user: Continue with your work.",
					approval: { id: "approval-legacy-1" },
				},
			],
		}),
	);

	await assert.rejects(() =>
		parseChatStreamMessageValue({
			id: "user-legacy-image",
			role: "user",
			parts: [
				{
					type: "image",
					image: "data:image/png;base64,abc123",
					mediaType: "image/png",
				},
			],
		}),
	);
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
			{
				type: "data-completion-status",
				data: {
					threadId: "thread-1",
					completionId: "completion-1",
					isRunning: true,
				},
			},
		],
	});

	assert.equal(parsed.parts[0]?.type, "data-thread-update");
	assert.equal(parsed.parts[1]?.type, "data-completion-status");
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
