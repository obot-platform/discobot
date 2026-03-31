import assert from "node:assert/strict";
import test from "node:test";

import type { ChatMessage } from "$lib/api-types";
import { createChatStreamState } from "./conversation-stream";
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

test("parseChatStreamMessageValue normalizes legacy dynamic-tool approval payloads", async () => {
	const parsed = await parseChatStreamMessageValue({
		id: "assistant-legacy-approval",
		role: "assistant",
		parts: [
			{
				type: "dynamic-tool",
				toolCallId: "tool-legacy-approval-1",
				toolName: "ExitPlanMode",
				state: "output-available",
				output: "Plan feedback from user: Continue with your work.",
				approval: { id: "approval-legacy-1" },
			},
			{
				type: "dynamic-tool",
				toolCallId: "tool-legacy-denied-1",
				toolName: "Bash",
				state: "output-denied",
				input: { command: "rm -rf /tmp/demo" },
			},
		],
	});

	assert.equal(parsed.parts[0]?.type, "dynamic-tool");
	assert.equal(
		parsed.parts[0]?.type === "dynamic-tool"
			? parsed.parts[0].approval?.approved
			: undefined,
		true,
	);
	assert.equal(parsed.parts[1]?.type, "dynamic-tool");
	assert.deepEqual(
		parsed.parts[1]?.type === "dynamic-tool"
			? parsed.parts[1].approval
			: undefined,
		{ id: "tool-legacy-denied-1", approved: false },
	);
});

test("parseChatStreamMessageValue accepts denied dynamic tools with rejection approval", async () => {
	const parsed = await parseChatStreamMessageValue({
		id: "assistant-denied",
		role: "assistant",
		parts: [
			{
				type: "reasoning",
				text: "Requesting approval before making the change.",
				state: "done",
				providerMetadata: {
					openai: {
						type: "reasoning",
						encrypted_content: "gAAAA_enc",
					},
				},
			},
			{
				type: "dynamic-tool",
				toolCallId: "tool-denied-1",
				toolName: "ExitPlanMode",
				state: "output-denied",
				input: { allowedPrompts: [] },
				approval: {
					id: "approval-denied-1",
					approved: false,
					reason: "continue",
				},
			},
		],
	});

	assert.equal(parsed.id, "assistant-denied");
	assert.equal(parsed.parts[1]?.type, "dynamic-tool");
	assert.equal(
		parsed.parts[1]?.type === "dynamic-tool"
			? parsed.parts[1].approval?.approved
			: undefined,
		false,
	);
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

test("createChatStreamState clears stale messages on empty history replay", async () => {
	let messages: ChatMessage[] = [
		{
			id: "assistant-stale",
			role: "assistant",
			parts: [{ type: "text", text: "stale message" }],
		},
	];
	let historyReplayEnded = false;
	const streamState = createChatStreamState({
		getMessages: () => messages,
		setMessages: (nextMessages) => {
			messages = nextMessages;
		},
		onHistoryReplayEnd: () => {
			historyReplayEnded = true;
		},
	});

	await streamState.handleStreamEvent({ event: "history-start", data: "" });
	await streamState.handleStreamEvent({ event: "history-end", data: "" });

	assert.deepEqual(messages, []);
	assert.equal(historyReplayEnded, true);
});
