import assert from "node:assert/strict";
import test from "node:test";

import type { ChatMessage } from "$lib/api-types";

import {
	addToolApprovalResponse,
	buildUserMessageParts,
	createUserMessage,
	createUserMessageAttachment,
	getLatestPlanState,
	getPendingQuestionApprovalId,
	getPlanEntries,
	hasUserMessageContent,
	sortServiceItems,
	toServiceItem,
} from "../domains/session-domain.helpers";

test("sortServiceItems prioritizes explicit order before alphabetical fallbacks", () => {
	const services = [
		toServiceItem({
			id: "beta",
			name: "Beta",
			status: "stopped",
			path: "/tmp/beta.sh",
		}),
		toServiceItem({
			id: "zeta",
			name: "Zeta",
			order: 20,
			status: "stopped",
			path: "/tmp/zeta.sh",
		}),
		toServiceItem({
			id: "alpha",
			name: "Alpha",
			status: "stopped",
			path: "/tmp/alpha.sh",
		}),
		toServiceItem({
			id: "eta",
			name: "Eta",
			order: 10,
			status: "stopped",
			path: "/tmp/eta.sh",
		}),
		toServiceItem({
			id: "theta",
			name: "Theta",
			order: 10,
			status: "stopped",
			path: "/tmp/theta.sh",
		}),
	];

	assert.deepEqual(
		sortServiceItems(services).map((service) => service.id),
		["eta", "theta", "zeta", "alpha", "beta"],
	);
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

test("getPendingQuestionApprovalId returns the latest pending approval id", () => {
	const messages: ChatMessage[] = [
		{
			id: "assistant-1",
			role: "assistant",
			parts: [
				{
					type: "dynamic-tool",
					toolCallId: "call-old",
					toolName: "AskUserQuestion",
					state: "approval-responded",
					approval: { id: "approval-old", approved: true },
					input: { questions: [] },
				},
			],
		},
		{
			id: "assistant-2",
			role: "assistant",
			parts: [
				{
					type: "dynamic-tool",
					toolCallId: "call-new",
					toolName: "AskUserQuestion",
					state: "approval-requested",
					approval: { id: "approval-new" },
					input: { questions: [] },
				},
			],
		},
	];

	assert.equal(getPendingQuestionApprovalId(messages), "approval-new");
});

test("getLatestPlanState returns the latest EnterPlanMode plan file path", () => {
	const messages: ChatMessage[] = [
		{
			id: "assistant-1",
			role: "assistant",
			parts: [
				{
					type: "dynamic-tool",
					toolCallId: "enter-1",
					toolName: "EnterPlanMode",
					state: "output-available",
					input: {},
					output:
						"Plan mode activated. Plan file: /tmp/old-plan.md\n\nDo something",
				},
				{
					type: "dynamic-tool",
					toolCallId: "enter-2",
					toolName: "EnterPlanMode",
					state: "output-available",
					input: {},
					output:
						"Plan mode activated. Plan file: /tmp/latest-plan.md\n\nDo something else",
				},
			],
		},
	];

	assert.deepEqual(getLatestPlanState(messages), {
		toolName: "EnterPlanMode",
		toolCallId: "enter-2",
		approvalId: "enter-2",
		partState: "output-available",
		phase: "entered",
		planFilePath: "/tmp/latest-plan.md",
		planMarkdown: null,
		feedback: null,
		errorText: null,
	});
});

test("getLatestPlanState parses approved ExitPlanMode plan markdown", () => {
	const messages: ChatMessage[] = [
		{
			id: "assistant-1",
			role: "assistant",
			parts: [
				{
					type: "dynamic-tool",
					toolCallId: "exit-1",
					toolName: "ExitPlanMode",
					state: "output-available",
					input: {},
					approval: { id: "approval-1", approved: true },
					output:
						"Plan approved. Continue forward and implement the plan now.\n\nApproved plan:\n\n## Plan\n- Ship it\n",
				},
			],
		},
	];

	assert.deepEqual(getLatestPlanState(messages), {
		toolName: "ExitPlanMode",
		toolCallId: "exit-1",
		approvalId: "approval-1",
		partState: "output-available",
		phase: "approved",
		planFilePath: null,
		planMarkdown: "## Plan\n- Ship it",
		feedback: null,
		errorText: null,
	});
});

test("getLatestPlanState parses pending approval and keeps the approval id", () => {
	const messages: ChatMessage[] = [
		{
			id: "assistant-1",
			role: "assistant",
			parts: [
				{
					type: "dynamic-tool",
					toolCallId: "exit-1",
					toolName: "ExitPlanMode",
					state: "approval-requested",
					input: {},
					approval: { id: "approval-1" },
				},
			],
		},
	];

	assert.deepEqual(getLatestPlanState(messages), {
		toolName: "ExitPlanMode",
		toolCallId: "exit-1",
		approvalId: "approval-1",
		partState: "approval-requested",
		phase: "awaiting_approval",
		planFilePath: null,
		planMarkdown: null,
		feedback: null,
		errorText: null,
	});
});

test("getLatestPlanState parses feedback and errors", () => {
	const feedbackMessages: ChatMessage[] = [
		{
			id: "assistant-1",
			role: "assistant",
			parts: [
				{
					type: "dynamic-tool",
					toolCallId: "exit-feedback",
					toolName: "ExitPlanMode",
					state: "output-available",
					input: {},
					output:
						"Plan feedback from user: tighten the test plan\n\nRevise your plan file and call ExitPlanMode again when ready.",
				},
			],
		},
	];
	assert.deepEqual(getLatestPlanState(feedbackMessages), {
		toolName: "ExitPlanMode",
		toolCallId: "exit-feedback",
		approvalId: "exit-feedback",
		partState: "output-available",
		phase: "feedback",
		planFilePath: null,
		planMarkdown: null,
		feedback: "tighten the test plan",
		errorText: null,
	});

	const errorMessages: ChatMessage[] = [
		{
			id: "assistant-2",
			role: "assistant",
			parts: [
				{
					type: "dynamic-tool",
					toolCallId: "exit-error",
					toolName: "ExitPlanMode",
					state: "output-error",
					input: {},
					errorText:
						"No plan written yet. Write your complete plan to /tmp/plan.md before calling ExitPlanMode.",
				},
			],
		},
	];
	assert.deepEqual(getLatestPlanState(errorMessages), {
		toolName: "ExitPlanMode",
		toolCallId: "exit-error",
		approvalId: "exit-error",
		partState: "output-error",
		phase: "error",
		planFilePath: "/tmp/plan.md",
		planMarkdown: null,
		feedback: null,
		errorText:
			"No plan written yet. Write your complete plan to /tmp/plan.md before calling ExitPlanMode.",
	});
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
