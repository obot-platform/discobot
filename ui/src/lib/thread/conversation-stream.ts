import { addToolApprovalResponse } from "$lib/session/domains/session-domain.helpers";
import type { ChatMessage, Thread } from "$lib/api-types";
import {
	parseChatStreamChunk,
	parseChatStreamMessage,
	parseChatStreamMessageValue,
	type ChatStreamChunk,
	type ChatStreamEvent,
} from "$lib/thread/conversation-stream.events";

export {
	bindChatStreamEventSource,
	type ChatStreamEventName,
	type ChatStreamEventSource,
	type ChatStreamEventSourceOptions,
} from "$lib/thread/conversation-stream.events";

export type ChatStreamStateOptions = {
	getMessages: () => ChatMessage[];
	setMessages: (messages: ChatMessage[]) => void;
	onStart?: (info?: { resume?: boolean }) => void | Promise<void>;
	onFinish?: () => void | Promise<void>;
	onCompletionStatus?: (info: {
		threadId?: string;
		completionId?: string;
		isRunning: boolean;
	}) => void | Promise<void>;
	onHistoryReplayEnd?: () => void | Promise<void>;
	onChunkError?: (errorText: string) => void | Promise<void>;
	onThreadUpdate?: (thread: Thread) => void | Promise<void>;
};

type MessagePart = ChatMessage["parts"][number];
type TextPart = Extract<MessagePart, { type: "text" }>;
type ReasoningPart = Extract<MessagePart, { type: "reasoning" }>;
type DynamicToolPart = Extract<MessagePart, { type: "dynamic-tool" }>;

type StreamStateCallback<TResult = void, TArgs extends unknown[] = []> = (
	...args: TArgs
) => TResult | Promise<TResult>;

export function createChatStreamState(options: ChatStreamStateOptions) {
	let activeAssistantMessage: ChatMessage | null = null;
	let historyMessages: ChatMessage[] | null = null;
	let updateQueue = Promise.resolve();

	const getTargetMessages = () => historyMessages ?? options.getMessages();

	const beginHistoryReplay = () => {
		historyMessages = [];
	};

	const commitHistoryReplay = () => {
		if (historyMessages === null) {
			return;
		}

		if (historyMessages.length === 0) {
			historyMessages = null;
			return;
		}

		options.setMessages(historyMessages);
		historyMessages = null;
		runCallbackInBackground("onHistoryReplayEnd", options.onHistoryReplayEnd);
	};

	const removeProvisionalMessages = (messages: ChatMessage[]) => {
		for (let index = messages.length - 1; index >= 0; index -= 1) {
			if (messages[index]?.provisional) {
				messages.splice(index, 1);
			}
		}
	};

	const insertMessage = (
		messages: ChatMessage[],
		message: ChatMessage,
		insertBeforeMessageId?: string,
	) => {
		const insertBeforeIndex = insertBeforeMessageId
			? messages.findIndex(
					(candidate) => candidate.id === insertBeforeMessageId,
				)
			: -1;

		if (insertBeforeIndex === -1) {
			messages.push(message);
			// Return the array entry so callers keep Svelte's reactive proxy instead of the plain object we inserted.
			return messages[messages.length - 1];
		}

		messages.splice(insertBeforeIndex, 0, message);
		// Return the array entry so callers keep Svelte's reactive proxy instead of the plain object we inserted.
		return messages[insertBeforeIndex];
	};

	const setAssistantMessageStreamingStatus = (
		message: ChatMessage,
		status?: "streaming",
	) => {
		if (status) {
			message.status = status;
			return;
		}

		delete message.status;
	};

	const isObjectRecord = (value: unknown): value is Record<string, unknown> => {
		return typeof value === "object" && value !== null;
	};

	const runCallbackInBackground = <TArgs extends unknown[]>(
		label: string,
		callback: StreamStateCallback<void, TArgs> | undefined,
		...args: TArgs
	) => {
		const result = callback?.(...args);
		if (!(result instanceof Promise)) {
			return;
		}
		void result.catch((error) => {
			console.error(`Failed to run ${label} chat stream callback`, error);
		});
	};

	const upsertMessage = (
		message: ChatMessage,
		insertBeforeMessageId?: string,
	): ChatMessage => {
		const targetMessages = getTargetMessages();
		const existingIndex = targetMessages.findIndex(
			(candidate) => candidate.id === message.id,
		);

		if (existingIndex !== -1) {
			targetMessages[existingIndex] = message;
			// Return the array entry so callers keep Svelte's reactive proxy instead of the plain object we assigned.
			return targetMessages[existingIndex];
		}

		removeProvisionalMessages(targetMessages);
		return insertMessage(targetMessages, message, insertBeforeMessageId);
	};

	const mergeMessageMetadata = (message: ChatMessage, metadata: unknown) => {
		if (!isObjectRecord(metadata)) {
			return;
		}
		const current = isObjectRecord(message.metadata)
			? message.metadata
			: undefined;
		message.metadata = current ? { ...current, ...metadata } : { ...metadata };
	};

	const finalizeAssistantMessageParts = (message: ChatMessage) => {
		for (const part of message.parts) {
			if (
				(part.type === "text" || part.type === "reasoning") &&
				part.state === "streaming"
			) {
				part.state = "done";
			}
		}
	};

	const closeActiveAssistantStream = (
		expectedMessage: ChatMessage | null = activeAssistantMessage,
	) => {
		if (!expectedMessage) {
			return;
		}

		finalizeAssistantMessageParts(expectedMessage);
		setAssistantMessageStreamingStatus(expectedMessage);
		if (activeAssistantMessage === expectedMessage) {
			activeAssistantMessage = null;
		}
	};

	const ensureAssistantMessage = (
		messageId: string,
		options: { resetIfExisting?: boolean } = {},
	): ChatMessage => {
		const existingMessage = getTargetMessages().find(
			(message) => message.id === messageId,
		);
		if (existingMessage?.role === "assistant" && !options.resetIfExisting) {
			return existingMessage;
		}
		return upsertMessage({
			id: messageId,
			role: "assistant",
			parts: [],
		});
	};

	const createStreamingPart = (type: "text" | "reasoning") => {
		return type === "text"
			? ({ type, text: "", state: "streaming" } satisfies TextPart)
			: ({ type, text: "", state: "streaming" } satisfies ReasoningPart);
	};

	const getLastStreamingPart = (
		message: ChatMessage,
		type: "text" | "reasoning",
	) => {
		for (let index = message.parts.length - 1; index >= 0; index -= 1) {
			const part = message.parts[index];
			if (part?.type === type && part.state === "streaming") {
				return part;
			}
		}

		return undefined;
	};

	const getOrCreateStreamingPart = (
		message: ChatMessage,
		type: "text" | "reasoning",
	) => {
		let part = getLastStreamingPart(message, type);
		if (!part) {
			part = createStreamingPart(type);
			message.parts.push(part);
		}
		return part;
	};

	const finishStreamingPart = (
		message: ChatMessage,
		type: "text" | "reasoning",
	) => {
		const part = getLastStreamingPart(message, type);
		if (!part) {
			return;
		}

		part.state = "done";
	};

	const getToolPart = (
		message: ChatMessage,
		toolCallId: string,
	): DynamicToolPart | undefined => {
		const part = message.parts.find(
			(candidate) =>
				candidate.type === "dynamic-tool" &&
				candidate.toolCallId === toolCallId,
		);
		return part?.type === "dynamic-tool" ? part : undefined;
	};

	const getOrCreateToolPart = (
		message: ChatMessage,
		toolCallId: string,
		toolName?: string,
		title?: string,
	): DynamicToolPart => {
		const existingPart = getToolPart(message, toolCallId);
		if (existingPart) {
			if (toolName) {
				existingPart.toolName = toolName;
			}
			if (title !== undefined) {
				existingPart.title = title;
			}
			return existingPart;
		}
		const nextPart: DynamicToolPart = {
			type: "dynamic-tool",
			toolCallId,
			toolName: toolName ?? "Unknown",
			state: "input-streaming",
			input: undefined,
			...(title !== undefined ? { title } : {}),
		};
		message.parts.push(nextPart);
		return nextPart;
	};

	const parsePartialToolInput = (text: string): unknown => {
		try {
			return JSON.parse(text);
		} catch {
			return text;
		}
	};

	const getToolInputText = (toolPart: DynamicToolPart | undefined) => {
		if (toolPart?.input === undefined) {
			return "";
		}

		if (typeof toolPart.input === "string") {
			return toolPart.input;
		}

		return JSON.stringify(toolPart.input);
	};

	const updateToolPart = (
		message: ChatMessage,
		toolCallId: string,
		updates: Partial<
			Pick<
				DynamicToolPart,
				"state" | "input" | "output" | "errorText" | "approval"
			>
		>,
		toolName?: string,
		title?: string,
	) => {
		const toolPart = getOrCreateToolPart(message, toolCallId, toolName, title);
		Object.assign(toolPart, updates);
		return toolPart;
	};

	const startAssistantStream = (
		messageId: string | undefined,
		options: { resume?: boolean } = {},
	) => {
		if (!messageId) {
			throw new Error("Received start chunk without a messageId");
		}

		if (activeAssistantMessage) {
			closeActiveAssistantStream();
		}

		const message = ensureAssistantMessage(messageId, {
			resetIfExisting: !options.resume,
		});
		setAssistantMessageStreamingStatus(message, "streaming");
		activeAssistantMessage = message;
	};

	const applyChunkEvent = async (chunk: ChatStreamChunk) => {
		switch (chunk.type) {
			case "data-thread-update": {
				runCallbackInBackground(
					"onThreadUpdate",
					options.onThreadUpdate,
					chunk.data.thread,
				);
				return;
			}

			case "data-thread-resume": {
				if (typeof chunk.data?.messageId !== "string") {
					return;
				}
				runCallbackInBackground("onStart", options.onStart, { resume: true });
				startAssistantStream(chunk.data.messageId, { resume: true });
				return;
			}

			case "data-completion-status": {
				if (typeof chunk.data?.isRunning !== "boolean") {
					return;
				}
				runCallbackInBackground(
					"onCompletionStatus",
					options.onCompletionStatus,
					{
						threadId:
							typeof chunk.data.threadId === "string"
								? chunk.data.threadId
								: undefined,
						completionId:
							typeof chunk.data.completionId === "string"
								? chunk.data.completionId
								: undefined,
						isRunning: chunk.data.isRunning,
					},
				);
				return;
			}

			case "message-metadata": {
				if (activeAssistantMessage) {
					mergeMessageMetadata(activeAssistantMessage, chunk.messageMetadata);
				}
				return;
			}

			case "data-user-message": {
				const message = await parseChatStreamMessageValue(chunk.data.message);
				upsertMessage(message, chunk.data.insertBeforeMessageId);
				return;
			}

			case "error": {
				runCallbackInBackground(
					"onChunkError",
					options.onChunkError,
					chunk.errorText ?? "Unknown chat error",
				);
				return;
			}

			case "data-tool-approval-response": {
				const approvalId = chunk.data.approvalId;
				const approved = chunk.data.approved;
				if (typeof approvalId !== "string" || typeof approved !== "boolean") {
					throw new Error("Approval response chunk is missing approval data");
				}
				addToolApprovalResponse(getTargetMessages(), {
					id: approvalId,
					approved,
					reason: chunk.data.reason,
				});
				return;
			}

			case "start": {
				runCallbackInBackground("onStart", options.onStart, {
					resume: false,
				});
				startAssistantStream(chunk.messageId);
				if (activeAssistantMessage) {
					mergeMessageMetadata(activeAssistantMessage, chunk.messageMetadata);
				}
				return;
			}
		}

		const activeMessage = activeAssistantMessage;
		if (!activeMessage) {
			throw new Error(`Received ${chunk.type} chunk before a start chunk`);
		}

		switch (chunk.type) {
			case "text-start": {
				getOrCreateStreamingPart(activeMessage, "text");
				return;
			}

			case "text-delta": {
				const textPart = getOrCreateStreamingPart(activeMessage, "text");
				textPart.text += chunk.delta;
				return;
			}

			case "text-end": {
				finishStreamingPart(activeMessage, "text");
				return;
			}

			case "reasoning-start": {
				getOrCreateStreamingPart(activeMessage, "reasoning");
				return;
			}

			case "reasoning-delta": {
				const reasoningPart = getOrCreateStreamingPart(
					activeMessage,
					"reasoning",
				);
				reasoningPart.text += chunk.delta;
				return;
			}

			case "reasoning-end": {
				finishStreamingPart(activeMessage, "reasoning");
				return;
			}

			case "tool-input-start": {
				updateToolPart(
					activeMessage,
					chunk.toolCallId,
					{ state: "input-streaming", input: undefined },
					chunk.toolName,
					chunk.title,
				);
				return;
			}

			case "tool-input-delta": {
				const existingToolPart = getToolPart(activeMessage, chunk.toolCallId);
				const nextInputText =
					getToolInputText(existingToolPart) + chunk.inputTextDelta;
				updateToolPart(
					activeMessage,
					chunk.toolCallId,
					{
						state: "input-streaming",
						input: parsePartialToolInput(nextInputText),
					},
					existingToolPart?.toolName,
					existingToolPart?.title,
				);
				return;
			}

			case "tool-input-available": {
				updateToolPart(
					activeMessage,
					chunk.toolCallId,
					{ state: "input-available", input: chunk.input },
					chunk.toolName,
					chunk.title,
				);
				return;
			}

			case "tool-input-error": {
				updateToolPart(
					activeMessage,
					chunk.toolCallId,
					{
						state: "output-error",
						input: chunk.input,
						errorText: chunk.errorText,
					},
					chunk.toolName,
					chunk.title,
				);
				return;
			}

			case "tool-approval-request": {
				const toolPart = getToolPart(activeMessage, chunk.toolCallId);
				updateToolPart(
					activeMessage,
					chunk.toolCallId,
					{
						state: "approval-requested",
						approval: { id: chunk.approvalId },
					},
					toolPart?.toolName,
					toolPart?.title,
				);
				closeActiveAssistantStream(activeMessage);
				runCallbackInBackground("onFinish", options.onFinish);
				return;
			}

			case "tool-output-available": {
				const toolPart = getToolPart(activeMessage, chunk.toolCallId);
				updateToolPart(
					activeMessage,
					chunk.toolCallId,
					{ state: "output-available", output: chunk.output },
					toolPart?.toolName,
					toolPart?.title,
				);
				return;
			}

			case "tool-output-error": {
				const toolPart = getToolPart(activeMessage, chunk.toolCallId);
				updateToolPart(
					activeMessage,
					chunk.toolCallId,
					{ state: "output-error", errorText: chunk.errorText },
					toolPart?.toolName,
					toolPart?.title,
				);
				return;
			}

			case "tool-output-denied": {
				const toolPart = getToolPart(activeMessage, chunk.toolCallId);
				updateToolPart(
					activeMessage,
					chunk.toolCallId,
					{ state: "output-denied" },
					toolPart?.toolName,
					toolPart?.title,
				);
				return;
			}

			case "source-url":
				activeMessage.parts.push({
					type: "source-url",
					sourceId: chunk.sourceId,
					url: chunk.url,
					...(chunk.title ? { title: chunk.title } : {}),
				});
				return;

			case "source-document":
				activeMessage.parts.push({
					type: "source-document",
					sourceId: chunk.sourceId,
					mediaType: chunk.mediaType,
					title: chunk.title,
					...(chunk.filename ? { filename: chunk.filename } : {}),
				});
				return;

			case "file":
				activeMessage.parts.push({
					type: "file",
					url: chunk.url,
					mediaType: chunk.mediaType,
				});
				return;

			case "start-step":
				activeMessage.parts.push({ type: "step-start" });
				return;

			case "finish-step":
				return;

			case "abort":
			case "finish":
				closeActiveAssistantStream(activeMessage);
				runCallbackInBackground("onFinish", options.onFinish);
				return;
		}
	};

	const applyEvent = async (event: ChatStreamEvent) => {
		if (event.event === "ping") {
			return;
		}

		if (event.event === "history-start") {
			beginHistoryReplay();
			return;
		}

		if (event.event === "history-message") {
			upsertMessage(await parseChatStreamMessage(event.data));
			return;
		}

		if (event.event === "history-end") {
			commitHistoryReplay();
			return;
		}

		await applyChunkEvent(await parseChatStreamChunk(event.data));
	};

	const handleStreamEvent = (event: ChatStreamEvent) => {
		const task = updateQueue
			.catch(() => undefined)
			.then(() => applyEvent(event));
		updateQueue = task;
		return task;
	};

	const reset = () => {
		historyMessages = null;
		closeActiveAssistantStream();
		options.setMessages([]);
	};

	return {
		handleStreamEvent,
		reset,
	};
}
