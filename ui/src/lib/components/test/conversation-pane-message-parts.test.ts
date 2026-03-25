import assert from "node:assert/strict";
import test from "node:test";

import type { ChatMessage } from "$lib/api-types";
import {
	getAssistantMessagePartGroups,
	getUserMessageRenderableParts,
} from "../app/conversation-pane-message-parts";

function createAssistantMessage(
	parts: ChatMessage["parts"],
	extra?: Partial<ChatMessage> & { status?: string },
): ChatMessage {
	return {
		id: "assistant-1",
		role: "assistant",
		parts,
		...extra,
	};
}

function createUserMessage(parts: ChatMessage["parts"]): ChatMessage {
	return {
		id: "user-1",
		role: "user",
		parts,
	};
}

test("getAssistantMessagePartGroups collapses leading assistant steps before trailing text", () => {
	const groups = getAssistantMessagePartGroups(
		createAssistantMessage([
			{ type: "reasoning", text: "Inspecting the renderer." },
			{
				type: "dynamic-tool",
				toolCallId: "tool-1",
				toolName: "Read",
				state: "output-available",
				input: { file_path: "/tmp/example.ts" },
				output: { content: "hello" },
			},
			{ type: "text", text: "I found the relevant file." },
		]),
	);

	assert.equal(groups.hasCollapsedSteps, true);
	assert.equal(groups.collapsedStepCount, 2);
	assert.deepEqual(
		groups.collapsedParts.map((part) => part.type),
		["reasoning", "dynamic-tool"],
	);
	assert.deepEqual(
		groups.visibleParts.map((part) => part.type),
		["text"],
	);
});

test("getAssistantMessagePartGroups does not collapse assistant messages that do not end in text", () => {
	const groups = getAssistantMessagePartGroups(
		createAssistantMessage([
			{ type: "reasoning", text: "Inspecting the renderer." },
			{
				type: "dynamic-tool",
				toolCallId: "tool-1",
				toolName: "Read",
				state: "output-available",
				input: { file_path: "/tmp/example.ts" },
				output: { content: "hello" },
			},
		]),
	);

	assert.equal(groups.hasCollapsedSteps, false);
	assert.equal(groups.collapsedStepCount, 0);
	assert.deepEqual(
		groups.visibleParts.map((part) => part.type),
		["reasoning", "dynamic-tool"],
	);
});

test("getAssistantMessagePartGroups keeps streaming assistant messages fully expanded", () => {
	const groups = getAssistantMessagePartGroups(
		createAssistantMessage(
			[
				{
					type: "dynamic-tool",
					toolCallId: "tool-1",
					toolName: "Read",
					state: "output-available",
					input: { file_path: "/tmp/example.ts" },
					output: { content: "hello" },
				},
				{ type: "text", text: "Still working..." },
			],
			{ status: "streaming" },
		),
	);

	assert.equal(groups.hasCollapsedSteps, false);
	assert.equal(groups.collapsedStepCount, 0);
	assert.deepEqual(
		groups.visibleParts.map((part) => part.type),
		["dynamic-tool", "text"],
	);
});

test("getAssistantMessagePartGroups keeps assistant messages expanded until streaming is complete", () => {
	const groups = getAssistantMessagePartGroups(
		createAssistantMessage([
			{
				type: "dynamic-tool",
				toolCallId: "tool-1",
				toolName: "Read",
				state: "output-available",
				input: { file_path: "/tmp/example.ts" },
				output: { content: "hello" },
			},
			{ type: "text", text: "Partial reply" },
		]),
		{ isMessageComplete: false },
	);

	assert.equal(groups.hasCollapsedSteps, false);
	assert.equal(groups.collapsedStepCount, 0);
	assert.deepEqual(
		groups.visibleParts.map((part) => part.type),
		["dynamic-tool", "text"],
	);
});

test("getAssistantMessagePartGroups keeps all trailing text parts visible", () => {
	const groups = getAssistantMessagePartGroups(
		createAssistantMessage([
			{ type: "reasoning", text: "Inspecting the renderer." },
			{ type: "text", text: "First summary paragraph." },
			{ type: "text", text: "Second summary paragraph." },
		]),
	);

	assert.equal(groups.hasCollapsedSteps, true);
	assert.equal(groups.collapsedStepCount, 1);
	assert.deepEqual(
		groups.visibleParts.map((part) =>
			part.type === "text" ? part.text : part.type,
		),
		["First summary paragraph.", "Second summary paragraph."],
	);
});

test("getUserMessageRenderableParts keeps text and file parts for user messages", () => {
	const parts = getUserMessageRenderableParts(
		createUserMessage([
			{ type: "text", text: "Please review this screenshot." },
			{
				type: "file",
				filename: "preview.png",
				mediaType: "image/png",
				url: "data:image/png;base64,abc123",
			},
		]),
	);

	assert.deepEqual(
		parts.map((part) => part.type),
		["text", "file"],
	);
	assert.equal(parts[1]?.type, "file");
	if (parts[1]?.type === "file") {
		assert.equal(parts[1].filename, "preview.png");
		assert.equal(parts[1].mediaType, "image/png");
	}
});
