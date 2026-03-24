import assert from "node:assert/strict";
import test from "node:test";

import type { ChatMessage } from "$lib/api-types";

import {
	addToolApprovalResponse,
	buildUserMessageParts,
	createUserMessage,
	createUserMessageAttachment,
	getPlanEntries,
	hasUserMessageContent,
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

	const updated = addToolApprovalResponse(messages, {
		id: "call-1",
		approved: true,
	});

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
	];

	const updated = addToolApprovalResponse(messages, {
		id: "call-1",
		approved: true,
	});

	assert.equal(updated, false);
	assert.equal(messages[0]?.parts[0]?.type, "dynamic-tool");
	assert.equal(
		messages[0]?.parts[0]?.type === "dynamic-tool"
			? messages[0].parts[0].state
			: undefined,
		"output-available",
	);
});

test("buildUserMessageParts includes attachments without dropping text", () => {
	const parts = buildUserMessageParts("hello", [
		{
			filename: "preview.png",
			mediaType: "image/png",
			url: "data:image/png;base64,abc123",
		},
	]);

	assert.deepEqual(parts, [
		{ type: "text", text: "hello" },
		{
			type: "file",
			filename: "preview.png",
			mediaType: "image/png",
			url: "data:image/png;base64,abc123",
		},
	]);
});

test("getPlanEntries returns the last TodoWrite update from the same assistant message", () => {
	const messages: ChatMessage[] = [
		{
			id: "assistant-1",
			role: "assistant",
			parts: [
				{
					type: "dynamic-tool",
					toolCallId: "todo-1",
					toolName: "TodoWrite",
					state: "output-available",
					input: {
						todos: [
							{
								content: "Use the first todo update",
								activeForm: "Using the first todo update",
								status: "in_progress",
							},
						],
					},
					output: {},
				},
				{
					type: "dynamic-tool",
					toolCallId: "todo-2",
					toolName: "TodoWrite",
					state: "output-available",
					input: {
						todos: [
							{
								content: "Use the last todo update",
								activeForm: "Using the last todo update",
								status: "completed",
							},
						],
					},
					output: {},
				},
			],
		},
	];

	assert.deepEqual(getPlanEntries(messages), [
		{
			content: "Use the last todo update",
			activeForm: "Using the last todo update",
			status: "completed",
		},
	]);
});

test("hasUserMessageContent treats attachment-only messages as non-empty", () => {
	assert.equal(
		hasUserMessageContent([
			{
				type: "file",
				filename: "preview.png",
				mediaType: "image/png",
				url: "data:image/png;base64,abc123",
			},
		]),
		true,
	);
	assert.equal(hasUserMessageContent([{ type: "text", text: "   " }]), false);
});

test("createUserMessageAttachment encodes files as data URLs", async () => {
	const file = new File([Uint8Array.from([104, 105])], "greeting.txt", {
		type: "text/plain",
	});

	const attachment = await createUserMessageAttachment(file);

	assert.deepEqual(attachment, {
		filename: "greeting.txt",
		mediaType: "text/plain",
		url: "data:text/plain;base64,aGk=",
	});
});

test("getPlanEntries returns the last TodoWrite update across messages", () => {
	const messages: ChatMessage[] = [
		{
			id: "assistant-1",
			role: "assistant",
			parts: [
				{
					type: "dynamic-tool",
					toolCallId: "todo-1",
					toolName: "TodoWrite",
					state: "output-available",
					input: {
						todos: [
							{
								content: "Earlier todo update",
								activeForm: "Using the earlier todo update",
								status: "pending",
							},
						],
					},
					output: {},
				},
			],
		},
		{
			id: "assistant-2",
			role: "assistant",
			parts: [
				{
					type: "dynamic-tool",
					toolCallId: "todo-2",
					toolName: "TodoWrite",
					state: "output-available",
					input: {
						todos: [
							{
								content: "Latest todo update",
								activeForm: "Using the latest todo update",
								status: "in_progress",
							},
						],
					},
					output: {},
				},
			],
		},
	];

	assert.deepEqual(getPlanEntries(messages), [
		{
			content: "Latest todo update",
			activeForm: "Using the latest todo update",
			status: "in_progress",
		},
	]);
});
