import assert from "node:assert/strict";
import test from "node:test";

import type { ChatMessage } from "$lib/api-types";

import {
	bindChatStreamEventSource,
	createChatStreamState,
} from "$lib/thread/conversation-stream";

function makeTextMessage(id: string, role: ChatMessage["role"], text: string): ChatMessage {
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
							options: [{ label: "test.db.sql", description: "Keep the SQL extension." }],
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
	private listeners = new Map<string, Set<(event: MessageEvent<string>) => void>>();

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
): {
	messages: ChatMessage[];
	modeChanges: string[];
	modelChanges: string[];
	reasoningChanges: string[];
	actionableQuestionCount: number;
	meaningfulToolOutputCount: number;
	setCount: number;
	state: ReturnType<typeof createChatStreamState>;
} {
	let currentMessages: ChatMessage[] = initialMessages;
	let setCount = 0;
	let actionableQuestionCount = 0;
	let meaningfulToolOutputCount = 0;
	const modeChanges: string[] = [];
	const modelChanges: string[] = [];
	const reasoningChanges: string[] = [];

	const state = createChatStreamState({
		getMessages: () => currentMessages,
		setMessages: (nextMessages) => {
			currentMessages = nextMessages;
			setCount += 1;
		},
		onActionableQuestion: () => {
			actionableQuestionCount += 1;
		},
		onMeaningfulToolOutput: () => {
			meaningfulToolOutputCount += 1;
		},
		setMode: (mode) => {
			modeChanges.push(mode);
		},
		setModel: (model) => {
			modelChanges.push(model);
		},
		setReasoning: (reasoning) => {
			reasoningChanges.push(reasoning);
		},
	});

	return {
		get messages() {
			return currentMessages;
		},
		get modeChanges() {
			return modeChanges;
		},
		get modelChanges() {
			return modelChanges;
		},
		get reasoningChanges() {
			return reasoningChanges;
		},
		get actionableQuestionCount() {
			return actionableQuestionCount;
		},
		get meaningfulToolOutputCount() {
			return meaningfulToolOutputCount;
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

test("history replay buffers messages until history-end", async () => {
	const harness = createHarness();

	await harness.state.handleStreamEvent({
		event: "history-start",
		data: "{}",
	});
	await harness.state.handleStreamEvent({
		event: "history-message",
		data: JSON.stringify(makeTextMessage("history-user", "user", "old message")),
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
		data: JSON.stringify({ type: "text-delta", id: "part-1", delta: "live reply" }),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "text-end", id: "part-1" }),
	});

	assert.equal(harness.setCount, 0);
	assert.deepEqual(harness.messages, []);
	assert.equal(harness.state.isBufferingHistory, true);

	await harness.state.handleStreamEvent({
		event: "history-end",
		data: "{}",
	});

	const messages: ChatMessage[] = harness.messages;

	assert.equal(harness.setCount, 1);
	assert.equal(harness.state.isBufferingHistory, false);
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

	assert.deepEqual(
		harness.messages.map((message) => ({ id: message.id, provisional: message.provisional })),
		[{ id: "user-2", provisional: undefined }],
	);
});

test("assistant chunk updates keep the same message object and avoid array reassignment", async () => {
	const harness = createHarness();

	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "start", messageId: "assistant-1" }),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "text-start", id: "part-1" }),
	});

	assert.equal(harness.setCount, 1);
	const assistantMessage = harness.messages[0];

	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "text-delta", id: "part-1", delta: "stream" }),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "text-end", id: "part-1" }),
	});

	assert.equal(harness.setCount, 1);
	assert.equal(harness.messages[0], assistantMessage);
	assert.equal(harness.messages[0].parts[0]?.type, "text");
	assert.equal(harness.messages[0].parts[0]?.text, "stream");
	assert.equal(harness.messages[0].parts[0]?.state, "done");

	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "finish", finishReason: "stop" }),
	});
});

test("chunk events surface mode, model, and reasoning updates", async () => {
	const harness = createHarness();

	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "data-mode-change", data: { mode: "plan" } }),
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
			type: "message-metadata",
			messageMetadata: {
				model: "openai/gpt-5",
				reasoning: "disabled",
			},
		}),
	});

	assert.deepEqual(harness.modeChanges, ["plan"]);
	assert.deepEqual(harness.modelChanges, [
		"anthropic/claude-sonnet-4-6",
		"openai/gpt-5",
	]);
	assert.deepEqual(harness.reasoningChanges, ["enabled", "disabled"]);
	assert.equal(harness.messages[0]?.id, "assistant-1");
});

test("AskUserQuestion becoming answerable triggers a single actionable callback", async () => {
	const harness = createHarness();

	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({ type: "start", messageId: "assistant-ask" }),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "tool-input-start",
			toolCallId: "ask-tool-1",
			toolName: "AskUserQuestion",
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
						options: [{ label: "This file", description: "Change only the active file." }],
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

	assert.equal(harness.actionableQuestionCount, 1);
	assert.equal(
		harness.messages[0]?.parts[0]?.type === "tool-AskUserQuestion" ||
			harness.messages[0]?.parts[0]?.type === "dynamic-tool",
		true,
	);
	assert.equal(
		typeof (harness.messages[0]?.parts[0] as { state?: unknown } | undefined)?.state === "string"
			? (harness.messages[0]?.parts[0] as { state: string }).state
			: undefined,
		"approval-requested",
	);
});

test("only the first non-preliminary tool output triggers a meaningful output callback", async () => {
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
			type: "tool-input-available",
			toolCallId: "tool-1",
			toolName: "Read",
			input: { file_path: "/tmp/demo.txt" },
		}),
	});
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "tool-output-available",
			toolCallId: "tool-1",
			output: { file: "/tmp/demo.txt" },
			preliminary: true,
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
	await harness.state.handleStreamEvent({
		event: "chunk",
		data: JSON.stringify({
			type: "tool-output-available",
			toolCallId: "tool-1",
			output: { file: "/tmp/demo.txt", contents: "hello again" },
		}),
	});

	assert.equal(harness.meaningfulToolOutputCount, 1);
	assert.equal(
		harness.messages[0]?.parts[0]?.type === "tool-Read" ||
			harness.messages[0]?.parts[0]?.type === "dynamic-tool",
		true,
	);
	assert.equal(
		typeof (harness.messages[0]?.parts[0] as { state?: unknown } | undefined)?.state === "string"
			? (harness.messages[0]?.parts[0] as { state: string }).state
			: undefined,
		"output-available",
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
			type: "start",
			messageId: "assistant-1",
			messageMetadata: { model: "anthropic/claude-sonnet-4-6", reasoning: "enabled" },
		}),
	);
	eventSource.dispatch("chunk", JSON.stringify({ type: "text-start", id: "part-1" }));
	eventSource.dispatch(
		"chunk",
		JSON.stringify({ type: "text-delta", id: "part-1", delta: "response" }),
	);
	eventSource.dispatch("chunk", JSON.stringify({ type: "text-end", id: "part-1" }));
	eventSource.dispatch("history-end", "{}");
	eventSource.dispatch("chunk", JSON.stringify({ type: "finish", finishReason: "stop" }));
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
	assert.deepEqual(harness.modelChanges, ["anthropic/claude-sonnet-4-6"]);
	assert.deepEqual(harness.reasoningChanges, ["enabled"]);

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

test("message events replace an existing message in place", async () => {
	const existingMessage = makeTextMessage("assistant-1", "assistant", "before");
	const harness = createHarness([existingMessage]);

	await harness.state.handleStreamEvent({
		event: "history-message",
		data: JSON.stringify(makeTextMessage("assistant-1", "assistant", "after")),
	});

	assert.equal(harness.setCount, 0);
	assert.equal(harness.messages[0], existingMessage);
	assert.deepEqual(harness.messages[0].parts, [
		{ type: "text", text: "after", state: "done" },
	]);
});
