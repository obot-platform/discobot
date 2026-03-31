import assert from "node:assert/strict";
import test from "node:test";

import type { ChatMessage, Thread } from "$lib/api-types";

import {
	bindChatStreamEventSource,
	createChatStreamState,
	type ChatStreamStateOptions,
} from "$lib/thread/conversation-stream";

function makeTextMessage(
	id: string,
	role: ChatMessage["role"],
	text: string,
): ChatMessage {
	return {
		id,
		role,
		parts: [{ type: "text", text, state: "done" }],
	};
}

function makeCustomAssistantMessage(id: string): ChatMessage {
	return {
		id,
		role: "assistant",
		parts: [
			{
				type: "reasoning",
				text: "Thinking through the approval flow.",
				state: "done",
			},
			{
				type: "dynamic-tool",
				toolCallId: "tool-123",
				toolName: "AskUserQuestion",
				state: "approval-requested",
				input: {
					questions: [
						{
							header: "Filename",
							question: "What filename do you want?",
							multiSelect: false,
							options: [
								{
									label: "test.db.sql",
									description: "Keep the SQL extension.",
								},
							],
						},
					],
				},
				approval: { id: "approval-123" },
			},
			{ type: "step-start" },
			{ type: "text", text: "Waiting for your answer.", state: "done" },
		],
	} as ChatMessage;
}

class MockChatStreamEventSource {
	private listeners = new Map<
		string,
		Set<(event: MessageEvent<string>) => void>
	>();

	addEventListener(
		type: string,
		listener: (event: MessageEvent<string>) => void,
	) {
		const listeners = this.listeners.get(type) ?? new Set();
		listeners.add(listener);
		this.listeners.set(type, listeners);
	}

	removeEventListener(
		type: string,
		listener: (event: MessageEvent<string>) => void,
	) {
		this.listeners.get(type)?.delete(listener);
	}

	dispatch(type: string, data: string) {
		for (const listener of this.listeners.get(type) ?? []) {
			listener({ data } as MessageEvent<string>);
		}
	}
}

async function flushStreamEvents() {
	await new Promise((resolve) => setTimeout(resolve, 0));
}

function createHarness(
	initialMessages: ChatMessage[] = [],
	overrides: Partial<ChatStreamStateOptions> = {},
): {
	messages: ChatMessage[];
	threadUpdates: Thread[];
	startEvents: Array<{ resume?: boolean } | undefined>;
	completionStatusEvents: Array<{
		threadId?: string;
		completionId?: string;
		isRunning: boolean;
	}>;
	setCount: number;
	state: ReturnType<typeof createChatStreamState>;
} {
	let currentMessages: ChatMessage[] = initialMessages;
	let setCount = 0;
	const threadUpdates: Thread[] = [];
	const startEvents: Array<{ resume?: boolean } | undefined> = [];
	const completionStatusEvents: Array<{
		threadId?: string;
		completionId?: string;
		isRunning: boolean;
	}> = [];

	const state = createChatStreamState({
		getMessages: () => currentMessages,
		setMessages: (nextMessages) => {
			currentMessages = nextMessages;
			setCount += 1;
			overrides.setMessages?.(nextMessages);
		},
		onThreadUpdate: (thread) => {
			threadUpdates.push(thread);
			return overrides.onThreadUpdate?.(thread);
		},
		onStart: (info) => {
			startEvents.push(info);
			return overrides.onStart?.(info);
		},
		onCompletionStatus: (info) => {
			completionStatusEvents.push(info);
			return overrides.onCompletionStatus?.(info);
		},
		onFinish: overrides.onFinish,
		onHistoryReplayEnd: overrides.onHistoryReplayEnd,
		onChunkError: overrides.onChunkError,
	});

	return {
		get messages() {
			return currentMessages;
		},
		get threadUpdates() {
			return threadUpdates;
		},
		get startEvents() {
			return startEvents;
		},
		get completionStatusEvents() {
			return completionStatusEvents;
		},
		get setCount() {
			return setCount;
		},
		state,
	};
}

test("history replay accepts Discobot assistant parts", async () => {
	const harness = createHarness();

	await harness.state.handleStreamEvent({
		event: "history-start",
		data: "{}",
	});
	await harness.state.handleStreamEvent({
		event: "history-message",
		data: JSON.stringify(makeCustomAssistantMessage("assistant-custom")),
	});
	await harness.state.handleStreamEvent({
		event: "history-end",
		data: "{}",
	});

	assert.equal(harness.messages.length, 1);
	assert.equal(harness.messages[0]?.id, "assistant-custom");
	assert.deepEqual(
		harness.messages[0]?.parts.map((part) => part.type),
		["reasoning", "dynamic-tool", "step-start", "text"],
	);
	assert.equal(harness.messages[0]?.parts[1]?.type, "dynamic-tool");
	assert.equal(
		harness.messages[0]?.parts[1]?.type === "dynamic-tool"
			? (harness.messages[0]?.parts[1] as { toolName?: string }).toolName
			: undefined,
		"AskUserQuestion",
	);
});

test("history replay accepts denied dynamic tool parts", async () => {
	const harness = createHarness();

	await harness.state.handleStreamEvent({
		event: "history-start",
		data: "{}",
	});
	await harness.state.handleStreamEvent({
		event: "history-message",
		data: JSON.stringify({
			id: "assistant-denied",
			role: "assistant",
			parts: [
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
		}),
	});
	await harness.state.handleStreamEvent({
		event: "history-end",
		data: "{}",
	});

	assert.equal(harness.messages.length, 1);
	assert.equal(harness.messages[0]?.parts[0]?.type, "dynamic-tool");
	assert.equal(
		harness.messages[0]?.parts[0]?.type === "dynamic-tool"
			? harness.messages[0].parts[0].state
			: undefined,
		"output-denied",
	);
	assert.equal(
		harness.messages[0]?.parts[0]?.type === "dynamic-tool"
			? harness.messages[0].parts[0].approval?.approved
			: undefined,
		false,
	);
});

test("history replay normalizes legacy dynamic tool approvals", async () => {
	const harness = createHarness();

	await harness.state.handleStreamEvent({
		event: "history-start",
		data: "{}",
	});
	await harness.state.handleStreamEvent({
		event: "history-message",
		data: JSON.stringify({
			id: "assistant-legacy-tools",
			role: "assistant",
			parts: [
				{
					type: "dynamic-tool",
					toolCallId: "tool-legacy-approval-1",
					toolName: "ExitPlanMode",
					state: "output-available",
					output: "Approved plan:\n\nContinue",
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
		}),
	});
	await harness.state.handleStreamEvent({
		event: "history-end",
		data: "{}",
	});

	assert.equal(harness.messages.length, 1);
	assert.equal(harness.messages[0]?.parts[0]?.type, "dynamic-tool");
	assert.equal(
		harness.messages[0]?.parts[0]?.type === "dynamic-tool"
			? harness.messages[0].parts[0].approval?.approved
			: undefined,
		true,
	);
	assert.equal(harness.messages[0]?.parts[1]?.type, "dynamic-tool");
	assert.deepEqual(
		harness.messages[0]?.parts[1]?.type === "dynamic-tool"
			? harness.messages[0].parts[1].approval
			: undefined,
		{ id: "tool-legacy-denied-1", approved: false },
	);
});

test("history replay buffers messages until history-end", async () => {
	const harness = createHarness();

	await harness.state.handleStreamEvent({
		event: "history-start",
		data: "{}",
	});
	await harness.state.handleStreamEvent({
		event: "history-message",
		data: JSON.stringify(
			makeTextMessage("history-user", "user", "old message"),
		),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "start", messageId: "assistant-1" }),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "text-start", id: "part-1" }),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "text-delta",
			id: "part-1",
			delta: "live reply",
		}),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "text-end", id: "part-1" }),
	});

	assert.equal(harness.setCount, 0);
	assert.deepEqual(harness.messages, []);

	await harness.state.handleStreamEvent({
		event: "history-end",
		data: "{}",
	});

	const messages: ChatMessage[] = harness.messages;

	assert.equal(harness.setCount, 1);
	assert.deepEqual(
		messages.map((message) => message.id),
		["history-user", "assistant-1"],
	);
	assert.equal(messages[1].parts[0]?.type, "text");
	assert.equal(messages[1].parts[0]?.text, "live reply");
	assert.equal(messages[1].parts[0]?.state, "done");

	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "finish", finishReason: "stop" }),
	});

	assert.deepEqual(harness.startEvents, [{ resume: false }]);
});

test("start chunks replace an existing assistant message instead of appending", async () => {
	const existingMessage = makeTextMessage(
		"assistant-existing",
		"assistant",
		"stale reply",
	);
	existingMessage.metadata = { step: 1 };
	existingMessage.status = "streaming";

	const harness = createHarness([existingMessage]);

	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "start", messageId: "assistant-existing" }),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "text-start", id: "part-2" }),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "text-delta",
			id: "part-2",
			delta: "fresh reply",
		}),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "text-end", id: "part-2" }),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "finish", finishReason: "stop" }),
	});

	assert.equal(harness.messages.length, 1);
	assert.notEqual(harness.messages[0], existingMessage);
	assert.equal(harness.messages[0]?.id, "assistant-existing");
	assert.deepEqual(harness.messages[0]?.parts, [
		{ type: "text", text: "fresh reply", state: "done" },
	]);
	assert.equal(harness.messages[0]?.metadata, undefined);
	assert.equal(harness.messages[0]?.status, undefined);
	assert.deepEqual(harness.startEvents, [{ resume: false }]);
});

test("history replay resumes an existing assistant message when instructed", async () => {
	const harness = createHarness([
		makeTextMessage("assistant-existing", "assistant", "partial reply"),
	]);

	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "data-thread-resume",
			data: { threadId: "thread-1", messageId: "assistant-existing" },
		}),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "text-start", id: "part-2" }),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "text-delta",
			id: "part-2",
			delta: "continued reply",
		}),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "text-end", id: "part-2" }),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "finish", finishReason: "stop" }),
	});

	const resumedMessages: ChatMessage[] = harness.messages;
	assert.equal(resumedMessages.length, 1);
	assert.equal(resumedMessages[0]?.id, "assistant-existing");
	assert.deepEqual(
		resumedMessages[0]?.parts.map((part) => part.type),
		["text", "text"],
	);
	const firstPart = resumedMessages[0]?.parts[0];
	const secondPart = resumedMessages[0]?.parts[1];
	assert.equal(firstPart?.type, "text");
	assert.equal(secondPart?.type, "text");
	assert.equal(
		firstPart?.type === "text" ? firstPart.text : undefined,
		"partial reply",
	);
	assert.equal(
		secondPart?.type === "text" ? secondPart.text : undefined,
		"continued reply",
	);
	assert.equal(
		secondPart?.type === "text" ? secondPart.state : undefined,
		"done",
	);
	assert.deepEqual(harness.startEvents, [{ resume: true }]);
});

test("resumed tool input deltas create and update tool parts without a start chunk", async () => {
	const harness = createHarness([
		makeTextMessage("assistant-resume-tool", "assistant", "working"),
	]);

	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "data-thread-resume",
			data: {
				threadId: "thread-1",
				messageId: "assistant-resume-tool",
			},
		}),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "tool-input-delta",
			toolCallId: "tool-resume-1",
			inputTextDelta: '{"questions":',
		}),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "tool-input-delta",
			toolCallId: "tool-resume-1",
			inputTextDelta:
				'[{"header":"Scope","question":"Proceed?","multiSelect":false,"options":[{"label":"Yes","description":"Continue"}]}]}',
		}),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "tool-input-available",
			toolCallId: "tool-resume-1",
			toolName: "AskUserQuestion",
			input: {
				questions: [
					{
						header: "Scope",
						question: "Proceed?",
						multiSelect: false,
						options: [{ label: "Yes", description: "Continue" }],
					},
				],
			},
		}),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "tool-approval-request",
			toolCallId: "tool-resume-1",
			approvalId: "approval-resume-1",
		}),
	});

	assert.deepEqual(harness.startEvents, [{ resume: true }]);
	assert.equal(harness.messages.length, 1);
	assert.equal(harness.messages[0]?.id, "assistant-resume-tool");
	assert.equal(harness.messages[0]?.parts[0]?.type, "text");
	assert.equal(harness.messages[0]?.parts[1]?.type === "dynamic-tool", true);
	const resumedToolPart = harness.messages[0]?.parts[1];
	assert.equal(
		resumedToolPart?.type === "dynamic-tool"
			? resumedToolPart.toolName
			: undefined,
		"AskUserQuestion",
	);
	assert.equal(
		resumedToolPart?.type === "dynamic-tool"
			? resumedToolPart.state
			: undefined,
		"approval-requested",
	);
	assert.deepEqual(
		resumedToolPart?.type === "dynamic-tool"
			? resumedToolPart.input
			: undefined,
		{
			questions: [
				{
					header: "Scope",
					question: "Proceed?",
					multiSelect: false,
					options: [{ label: "Yes", description: "Continue" }],
				},
			],
		},
	);
	assert.equal(
		resumedToolPart?.type === "dynamic-tool"
			? resumedToolPart.approval?.id
			: undefined,
		"approval-resume-1",
	);
});

test("completion status chunks notify the caller", async () => {
	const harness = createHarness();

	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "data-completion-status",
			data: {
				threadId: "thread-1",
				completionId: "completion-1",
				isRunning: true,
			},
		}),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "data-completion-status",
			data: {
				threadId: "thread-1",
				completionId: "completion-1",
				isRunning: false,
			},
		}),
	});

	assert.deepEqual(harness.completionStatusEvents, [
		{ threadId: "thread-1", completionId: "completion-1", isRunning: true },
		{ threadId: "thread-1", completionId: "completion-1", isRunning: false },
	]);
});

test("data-user-message inserts a preserved user message before the assistant reply", async () => {
	const harness = createHarness();

	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "data-user-message",
			data: {
				insertBeforeMessageId: "assistant-1",
				message: makeTextMessage("user-1", "user", "prompt"),
			},
		}),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "start", messageId: "assistant-1" }),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "text-start", id: "part-1" }),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "text-delta", id: "part-1", delta: "answer" }),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "text-end", id: "part-1" }),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "finish", finishReason: "stop" }),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "data-user-message",
			data: {
				insertBeforeMessageId: "assistant-1",
				message: makeTextMessage("user-1", "user", "prompt"),
			},
		}),
	});

	const messages: ChatMessage[] = harness.messages;

	assert.deepEqual(
		messages.map((message) => message.id),
		["user-1", "assistant-1"],
	);
	assert.equal(messages[0].role, "user");
	assert.deepEqual(messages[0].parts, [
		{ type: "text", text: "prompt", state: "done" },
	]);
	assert.equal(messages[1].parts[0]?.type, "text");
	assert.equal(messages[1].parts[0]?.text, "answer");
	assert.equal(messages[1].parts[0]?.state, "done");
});

test("data-thread-update notifies the caller", async () => {
	const harness = createHarness();
	const thread: Thread = {
		id: "thread-1",
		name: "Fix thread naming",
		mode: "plan",
		model: "anthropic/claude-sonnet-4-6",
		reasoning: "enabled",
		state: "cancelled",
	};

	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "data-thread-update",
			data: { thread },
		}),
	});

	assert.deepEqual(harness.threadUpdates, [thread]);
});

test("appending a new message removes all provisional messages first", async () => {
	const harness = createHarness([
		{
			id: "provisional-1",
			role: "user",
			parts: [{ type: "text", text: "local prompt", state: "done" }],
			provisional: true,
		},
	]);

	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "data-user-message",
			data: {
				insertBeforeMessageId: "assistant-1",
				message: makeTextMessage("user-2", "user", "server prompt"),
			},
		}),
	});

	assert.equal(harness.setCount, 0);
	assert.deepEqual(
		harness.messages.map((message) => ({
			id: message.id,
			provisional: message.provisional,
		})),
		[{ id: "user-2", provisional: undefined }],
	);
});

test("assistant chunk updates keep the same message object without list reassignment", async () => {
	const harness = createHarness();

	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "start", messageId: "assistant-1" }),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "text-start", id: "part-1" }),
	});

	assert.equal(harness.setCount, 0);
	const assistantMessage = harness.messages[0];

	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "text-delta", id: "part-1", delta: "stream" }),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "text-end", id: "part-1" }),
	});

	assert.equal(harness.setCount, 0);
	assert.equal(harness.messages[0], assistantMessage);
	assert.equal(harness.messages[0].parts[0]?.type, "text");
	assert.equal(harness.messages[0].parts[0]?.text, "stream");
	assert.equal(harness.messages[0].parts[0]?.state, "done");

	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "finish", finishReason: "stop" }),
	});
});

test("reasoning chunks mark the active assistant message as streaming until finish", async () => {
	const harness = createHarness();

	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "start", messageId: "assistant-1" }),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "reasoning-start", id: "reason-1" }),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "reasoning-delta",
			id: "reason-1",
			delta: "Thinking through the renderer",
		}),
	});

	assert.equal(
		(harness.messages[0] as (ChatMessage & { status?: string }) | undefined)
			?.status,
		"streaming",
	);
	assert.deepEqual(harness.messages[0]?.parts, [
		{
			type: "reasoning",
			text: "Thinking through the renderer",
			state: "streaming",
		},
	]);

	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "finish", finishReason: "stop" }),
	});

	assert.equal(
		(harness.messages[0] as (ChatMessage & { status?: string }) | undefined)
			?.status,
		undefined,
	);
	assert.deepEqual(harness.messages[0]?.parts, [
		{
			type: "reasoning",
			text: "Thinking through the renderer",
			state: "done",
		},
	]);
});

test("tool approval requests finalize the active assistant message", async () => {
	let finishCount = 0;
	const harness = createHarness([], {
		onFinish: () => {
			finishCount += 1;
		},
	});

	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "start", messageId: "assistant-approval" }),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "reasoning-start", id: "reason-1" }),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "reasoning-delta",
			id: "reason-1",
			delta: "Thinking about approval",
		}),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "tool-input-available",
			toolCallId: "ask-tool-1",
			toolName: "AskUserQuestion",
			input: {
				questions: [
					{
						header: "Scope",
						question: "Which scope should I use?",
						multiSelect: false,
						options: [
							{
								label: "This file",
								description: "Change only the active file.",
							},
						],
					},
				],
			},
		}),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "tool-approval-request",
			toolCallId: "ask-tool-1",
			approvalId: "approval-1",
		}),
	});

	assert.equal(harness.messages[0]?.status, undefined);
	assert.equal(finishCount, 1);
	assert.deepEqual(
		harness.messages[0]?.parts.map((part) =>
			part.type === "reasoning"
				? { type: part.type, state: part.state }
				: part.type === "dynamic-tool"
					? { type: part.type, state: part.state }
					: { type: part.type },
		),
		[
			{ type: "reasoning", state: "done" },
			{ type: "dynamic-tool", state: "approval-requested" },
		],
	);
});

test("finish finalizes any still-streaming text and reasoning parts", async () => {
	const harness = createHarness();

	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "start", messageId: "assistant-1" }),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "text-start", id: "text-1" }),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "text-delta",
			id: "text-1",
			delta: "unfinished text",
		}),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "reasoning-start", id: "reason-1" }),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "reasoning-delta",
			id: "reason-1",
			delta: "unfinished reasoning",
		}),
	});

	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "finish", finishReason: "stop" }),
	});

	assert.deepEqual(
		harness.messages[0]?.parts.map((part) =>
			part.type === "text" || part.type === "reasoning"
				? { type: part.type, state: part.state }
				: { type: part.type },
		),
		[
			{ type: "text", state: "done" },
			{ type: "reasoning", state: "done" },
		],
	);
});

test("thread update chunks surface thread metadata updates", async () => {
	const harness = createHarness();
	const firstThread: Thread = {
		id: "thread-1",
		name: "Initial name",
		mode: "plan",
		model: "anthropic/claude-sonnet-4-6",
		reasoning: "enabled",
	};
	const secondThread: Thread = {
		...firstThread,
		name: "Updated name",
		model: "openai/gpt-5",
		reasoning: "disabled",
	};

	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "data-thread-update",
			data: { thread: firstThread },
		}),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "start",
			messageId: "assistant-1",
			messageMetadata: {
				model: "anthropic/claude-sonnet-4-6",
				reasoning: "enabled",
			},
		}),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "data-thread-update",
			data: { thread: secondThread },
		}),
	});

	assert.deepEqual(harness.threadUpdates, [firstThread, secondThread]);
	assert.equal(harness.messages[0]?.id, "assistant-1");
});

test("tool approval responses are accepted during resumed streams", async () => {
	const harness = createHarness([
		makeCustomAssistantMessage("assistant-custom"),
	]);

	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "data-thread-resume",
			data: { threadId: "thread-1", messageId: "assistant-custom" },
		}),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "data-tool-approval-response",
			data: {
				approvalId: "approval-123",
				approved: true,
			},
		}),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "finish", finishReason: "stop" }),
	});

	assert.deepEqual(harness.startEvents, [{ resume: true }]);
	assert.equal(
		harness.messages[0]?.parts[1]?.type === "dynamic-tool"
			? harness.messages[0]?.parts[1]?.approval?.approved
			: undefined,
		true,
	);
});

test("tool outputs update the tool part without extra callbacks", async () => {
	const harness = createHarness();

	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "start", messageId: "assistant-tool" }),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "tool-input-start",
			toolCallId: "tool-1",
			toolName: "Read",
		}),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "tool-output-available",
			toolCallId: "tool-1",
			output: { file: "/tmp/demo.txt", contents: "hello" },
		}),
	});

	assert.equal(
		harness.messages[0]?.parts[0]?.type === "tool-Read" ||
			harness.messages[0]?.parts[0]?.type === "dynamic-tool",
		true,
	);
	assert.equal(
		typeof (harness.messages[0]?.parts[0] as { state?: unknown } | undefined)
			?.state === "string"
			? (harness.messages[0]?.parts[0] as { state: string }).state
			: undefined,
		"output-available",
	);
});

test("denied tool outputs preserve rejected approval metadata", async () => {
	const harness = createHarness([
		makeCustomAssistantMessage("assistant-custom"),
	]);

	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "data-thread-resume",
			data: { threadId: "thread-1", messageId: "assistant-custom" },
		}),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "data-tool-approval-response",
			data: {
				approvalId: "approval-123",
				approved: false,
				reason: "continue",
			},
		}),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "tool-output-denied",
			toolCallId: "tool-123",
		}),
	});

	assert.equal(harness.messages[0]?.parts[1]?.type, "dynamic-tool");
	assert.equal(
		harness.messages[0]?.parts[1]?.type === "dynamic-tool"
			? harness.messages[0].parts[1].state
			: undefined,
		"output-denied",
	);
	assert.deepEqual(
		harness.messages[0]?.parts[1]?.type === "dynamic-tool"
			? harness.messages[0].parts[1].approval
			: undefined,
		{ id: "approval-123", approved: false, reason: "continue" },
	);
});

test("bindChatStreamEventSource wires EventSource events into the reducer", async () => {
	const harness = createHarness();
	const eventSource = new MockChatStreamEventSource();
	const cleanup = bindChatStreamEventSource(eventSource, harness.state);

	eventSource.dispatch("history-start", "{}");
	eventSource.dispatch(
		"history-message",
		JSON.stringify(makeTextMessage("user-1", "user", "hello")),
	);
	eventSource.dispatch(
		"chunk",
		JSON.stringify({
			type: "data-thread-update",
			data: {
				thread: {
					id: "assistant-1",
					name: "Event source thread",
					mode: "plan",
					model: "anthropic/claude-sonnet-4-6",
					reasoning: "enabled",
				},
			},
		}),
	);
	eventSource.dispatch(
		"chunk",
		JSON.stringify({
			type: "start",
			messageId: "assistant-1",
			messageMetadata: {
				model: "anthropic/claude-sonnet-4-6",
				reasoning: "enabled",
			},
		}),
	);
	eventSource.dispatch(
		"chunk",
		JSON.stringify({ type: "text-start", id: "part-1" }),
	);
	eventSource.dispatch(
		"chunk",
		JSON.stringify({ type: "text-delta", id: "part-1", delta: "response" }),
	);
	eventSource.dispatch(
		"chunk",
		JSON.stringify({ type: "text-end", id: "part-1" }),
	);
	eventSource.dispatch("history-end", "{}");
	eventSource.dispatch(
		"chunk",
		JSON.stringify({ type: "finish", finishReason: "stop" }),
	);
	eventSource.dispatch("done", "{}");

	await flushStreamEvents();

	assert.deepEqual(
		harness.messages.map((message) => message.id),
		["user-1", "assistant-1"],
	);
	assert.equal(harness.messages[1].parts[0]?.type, "text");
	assert.equal(
		harness.messages[1].parts[0]?.type === "text"
			? harness.messages[1].parts[0].text
			: undefined,
		"response",
	);
	assert.deepEqual(harness.threadUpdates, [
		{
			id: "assistant-1",
			name: "Event source thread",
			mode: "plan",
			model: "anthropic/claude-sonnet-4-6",
			reasoning: "enabled",
		},
	]);

	cleanup();
	eventSource.dispatch(
		"history-message",
		JSON.stringify(makeTextMessage("user-2", "user", "ignored")),
	);
	await flushStreamEvents();

	assert.deepEqual(
		harness.messages.map((message) => message.id),
		["user-1", "assistant-1"],
	);
});

test("message events replace an existing message without list reassignment", async () => {
	const existingMessage = makeTextMessage("assistant-1", "assistant", "before");
	const harness = createHarness([existingMessage]);

	await harness.state.handleStreamEvent({
		event: "history-message",
		data: JSON.stringify(makeTextMessage("assistant-1", "assistant", "after")),
	});

	assert.equal(harness.setCount, 0);
	assert.notEqual(harness.messages[0], existingMessage);
	assert.deepEqual(harness.messages[0].parts, [
		{ type: "text", text: "after", state: "done" },
	]);
});
