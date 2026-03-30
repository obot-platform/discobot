import assert from "node:assert/strict";
import test from "node:test";

import { StartChatError } from "$lib/api-client";
import type { ChatMessage } from "$lib/api-types";
import {
	getPendingQuestionState,
	getStartChatErrorDetails,
	getSubmitMessages,
	hasStreamingAssistantMessage,
	isStreamingAssistantMessage,
	removeProvisionalSubmitMessage,
} from "./conversation.svelte";

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

test("isStreamingAssistantMessage detects top-level streaming status", () => {
	const message: ChatMessage = {
		id: "assistant-streaming",
		role: "assistant",
		parts: [{ type: "text", text: "Working", state: "done" }],
		status: "streaming",
	};

	assert.equal(isStreamingAssistantMessage(message), true);
});

test("isStreamingAssistantMessage detects streaming assistant parts", () => {
	const message: ChatMessage = {
		id: "assistant-part-streaming",
		role: "assistant",
		parts: [
			{
				type: "reasoning",
				text: "Thinking through resume state",
				state: "streaming",
			},
		],
	};

	assert.equal(isStreamingAssistantMessage(message), true);
});

test("hasStreamingAssistantMessage ignores non-streaming user messages", () => {
	const messages: ChatMessage[] = [
		{
			id: "user-1",
			role: "user",
			parts: [{ type: "text", text: "hello" }],
		},
		{
			id: "assistant-1",
			role: "assistant",
			parts: [{ type: "text", text: "done", state: "done" }],
		},
	];

	assert.equal(hasStreamingAssistantMessage(messages), false);
});

test("hasStreamingAssistantMessage detects resumed streaming assistant state", () => {
	const messages: ChatMessage[] = [
		{
			id: "assistant-1",
			role: "assistant",
			parts: [
				{ type: "text", text: "Partial reply", state: "done" },
				{
					type: "reasoning",
					text: "Continuing after navigation",
					state: "streaming",
				},
			],
		},
	];

	assert.equal(hasStreamingAssistantMessage(messages), true);
});

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

test("removeProvisionalSubmitMessage removes the failed optimistic message", () => {
	const failedMessage = makeUserMessage(
		[{ type: "text", text: "pending" }],
		true,
	);
	const keptMessage: ChatMessage = {
		id: "assistant-1",
		role: "assistant",
		parts: [{ type: "text", text: "existing" }],
	};

	assert.deepEqual(
		removeProvisionalSubmitMessage(
			[failedMessage, keptMessage],
			failedMessage.id,
		),
		[keptMessage],
	);
});

test("getPendingQuestionState prefers the pending approval from messages", () => {
	const messages: ChatMessage[] = [
		{
			id: "assistant-1",
			role: "assistant",
			parts: [
				{
					type: "dynamic-tool",
					toolCallId: "tool-1",
					toolName: "AskUserQuestion",
					state: "approval-requested",
					approval: { id: "approval-from-messages" },
					input: { questions: [] },
				},
			],
		},
	];

	assert.deepEqual(getPendingQuestionState(messages, "approval-from-error"), {
		hasPendingQuestion: true,
		pendingQuestionId: "approval-from-messages",
	});
});

test("getStartChatErrorDetails extracts pending-question metadata", () => {
	const error = new StartChatError(
		"Answer the earlier question first.",
		"pending_question_requires_answer",
		"approval-123",
	);

	assert.deepEqual(getStartChatErrorDetails(error), {
		message: "Answer the earlier question first.",
		pendingQuestionId: "approval-123",
		completionId: null,
	});
});

test("getStartChatErrorDetails suppresses the auto-resume conflict message", () => {
	const error = new StartChatError(
		"This thread has an interrupted turn that must resume before sending a new message.",
		"interrupted_turn_requires_resume",
		undefined,
		"resume-123",
	);

	assert.deepEqual(getStartChatErrorDetails(error), {
		message: null,
		pendingQuestionId: null,
		completionId: "resume-123",
	});
});
