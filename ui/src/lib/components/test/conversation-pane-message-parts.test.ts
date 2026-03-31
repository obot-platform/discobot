import assert from "node:assert/strict";
import test from "node:test";

import type { ChatMessage } from "$lib/api-types";
import {
	getAssistantMessagePartGroups,
	getHookFailureMessageMetadata,
	getHookPathDisplayLabel,
	getUserMessageOriginalCommandDisplay,
	getUserMessageOriginalText,
	getUserMessageRenderableParts,
	isHookFailureMessage,
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

test("getUserMessageOriginalText returns the metadata original text for user messages", () => {
	const originalText = getUserMessageOriginalText({
		id: "user-legacy-command",
		role: "user",
		metadata: { originalText: "/commit fix the bug" },
		parts: [{ type: "text", text: "Expanded command body." }],
	});

	assert.equal(originalText, "/commit fix the bug");
});

test("getUserMessageOriginalCommandDisplay parses slash commands into command and args", () => {
	assert.deepEqual(
		getUserMessageOriginalCommandDisplay({
			id: "user-command-1",
			role: "user",
			metadata: { originalText: "/foo bar baz" },
			parts: [{ type: "text", text: "Expanded command body." }],
		}),
		{
			command: "foo",
			args: "bar baz",
			rawText: "/foo bar baz",
		},
	);
});

test("getUserMessageOriginalCommandDisplay parses slash commands without args", () => {
	assert.deepEqual(
		getUserMessageOriginalCommandDisplay({
			id: "user-command-2",
			role: "user",
			metadata: { originalText: "/foo" },
			parts: [{ type: "text", text: "Expanded command body." }],
		}),
		{
			command: "foo",
			args: null,
			rawText: "/foo",
		},
	);
});

test("getUserMessageOriginalCommandDisplay ignores non-command original text", () => {
	assert.equal(
		getUserMessageOriginalCommandDisplay({
			id: "user-command-3",
			role: "user",
			metadata: { originalText: "plain text" },
			parts: [{ type: "text", text: "Expanded command body." }],
		}),
		null,
	);
});

test("getUserMessageOriginalText ignores assistant messages and missing metadata", () => {
	assert.equal(
		getUserMessageOriginalText({
			id: "assistant-1",
			role: "assistant",
			metadata: { originalText: "/commit fix the bug" },
			parts: [{ type: "text", text: "Expanded command body." }],
		}),
		null,
	);
	assert.equal(
		getUserMessageOriginalText({
			id: "user-2",
			role: "user",
			parts: [{ type: "text", text: "Expanded command body." }],
		}),
		null,
	);
});

test("isHookFailureMessage returns true for hook-failure metadata", () => {
	const message = createUserMessage([
		{ type: "text", text: "### Hook failed: lint" },
	]);
	(message as ChatMessage & { metadata?: unknown }).metadata = {
		discobot: {
			kind: "hook-failure",
			hookName: "lint",
			exitCode: 1,
			pattern: "**/*.go",
			files: ["agent-go/main.go"],
			hookPath: ".claude/hooks/backend-check.sh",
			output: "lint failed",
		},
	};

	assert.equal(isHookFailureMessage(message), true);
	assert.deepEqual(getHookFailureMessageMetadata(message), {
		kind: "hook-failure",
		hookName: "lint",
		exitCode: 1,
		pattern: "**/*.go",
		files: ["agent-go/main.go"],
		extraFileCount: undefined,
		hookPath: ".claude/hooks/backend-check.sh",
		output: "lint failed",
		outputPath: undefined,
		outputTruncated: undefined,
	});
});

test("getHookFailureMessageMetadata normalizes absolute hook paths", () => {
	const message = createUserMessage([
		{ type: "text", text: "### Hook failed: lint" },
	]);
	(message as ChatMessage & { metadata?: unknown }).metadata = {
		discobot: {
			kind: "hook-failure",
			hookName: "lint",
			exitCode: 1,
			hookPath: "/home/discobot/workspace/.discobot/hooks/09-ci.sh",
		},
	};

	assert.deepEqual(getHookFailureMessageMetadata(message), {
		kind: "hook-failure",
		hookName: "lint",
		exitCode: 1,
		pattern: undefined,
		hookPath: ".discobot/hooks/09-ci.sh",
		files: undefined,
		extraFileCount: undefined,
		output: undefined,
		outputPath: undefined,
		outputTruncated: undefined,
	});
});

test("getHookPathDisplayLabel trims the discobot hook directory", () => {
	assert.equal(getHookPathDisplayLabel(".discobot/hooks/09-ci.sh"), "09-ci.sh");
});

test("getHookPathDisplayLabel keeps non-discobot hook paths intact", () => {
	assert.equal(
		getHookPathDisplayLabel(".claude/hooks/backend-check.sh"),
		".claude/hooks/backend-check.sh",
	);
});

test("isHookFailureMessage returns false for ordinary user messages", () => {
	const message = createUserMessage([{ type: "text", text: "hello" }]);

	assert.equal(isHookFailureMessage(message), false);
});
