import type { UIMessage, UIMessageChunk } from "ai";
import {
	readUIMessageStream,
	safeValidateUIMessages,
	uiMessageChunkSchema,
} from "ai";

export type ChatStreamEvent =
	| {
			event: "history-start";
			data: string;
	  }
	| {
			event: "history-message";
			data: string;
	  }
	| {
			event: "history-end";
			data: string;
	  }
	| {
			event: "chunk";
			data: string;
	  }
	| {
			event: "done";
			data: string;
	  };

export type ChatStreamEventName = ChatStreamEvent["event"];

export type ChatStreamEventSource = {
	addEventListener: (
		type: ChatStreamEventName,
		listener: (event: MessageEvent<string>) => void,
	) => void;
	removeEventListener: (
		type: ChatStreamEventName,
		listener: (event: MessageEvent<string>) => void,
	) => void;
};

export type ChatStreamEventSourceOptions = {
	onError?: (error: unknown) => void;
};

export type ChatStreamStateOptions = {
	getMessages: () => UIMessage[];
	setMessages: (messages: UIMessage[]) => void;
	setMode?: (mode: string) => void;
	setModel?: (model: string) => void;
	setReasoning?: (reasoning: string) => void;
	setThreadName?: (name: string) => void;
};

type StreamMessageMetadata = {
	model?: string;
	reasoning?: string;
};

type ActiveAssistantStream = {
	error: unknown;
	messageId: string;
	readerTask: Promise<void>;
	writer: WritableStreamDefaultWriter<UIMessageChunk>;
};

type UserMessageChunk = {
	type: "data-user-message";
	data: {
		insertBeforeMessageId?: string;
		message: UIMessage;
	};
};

const terminalChunkTypes = new Set<UIMessageChunk["type"]>(["abort", "finish"]);

export function createChatStreamState(options: ChatStreamStateOptions) {
	let activeAssistantStream: ActiveAssistantStream | null = null;
	let historyMessages: UIMessage[] | null = null;
	let updateQueue = Promise.resolve();

	const getTargetMessages = () => historyMessages ?? options.getMessages();

	const setTargetMessages = (messages: UIMessage[]) => {
		if (historyMessages !== null) {
			historyMessages = messages;
			return;
		}

		options.setMessages(messages);
	};

	const replaceMessageInPlace = (target: UIMessage, source: UIMessage) => {
		const mutableTarget = target as unknown as Record<string, unknown>;
		const sourceRecord = source as unknown as Record<string, unknown>;

		for (const key of Object.keys(mutableTarget)) {
			if (!(key in sourceRecord)) {
				delete mutableTarget[key];
			}
		}

		Object.assign(mutableTarget, sourceRecord);
	};

	const upsertMessage = (
		message: UIMessage,
		insertBeforeMessageId?: string,
	) => {
		const targetMessages = getTargetMessages();
		const existingIndex = targetMessages.findIndex(
			(candidate) => candidate.id === message.id,
		);

		if (existingIndex !== -1) {
			replaceMessageInPlace(targetMessages[existingIndex], message);
			return;
		}

		const nextMessages = [...targetMessages];
		const insertBeforeIndex = insertBeforeMessageId
			? nextMessages.findIndex(
					(candidate) => candidate.id === insertBeforeMessageId,
				)
			: -1;

		if (insertBeforeIndex === -1) {
			nextMessages.push(message);
		} else {
			nextMessages.splice(insertBeforeIndex, 0, message);
		}

		setTargetMessages(nextMessages);
	};

	const validateMessage = async (message: unknown): Promise<UIMessage> => {
		const validation = await safeValidateUIMessages({ messages: [message] });
		if (!validation.success) {
			throw validation.error;
		}

		return validation.data[0];
	};

	const parseMessageEvent = async (data: string) => {
		return validateMessage(JSON.parse(data));
	};

	const parseChunkEvent = async (data: string): Promise<UIMessageChunk> => {
		const schema = uiMessageChunkSchema();
		if (!schema.validate) {
			throw new Error(
				"UIMessageChunk schema does not expose a validate function",
			);
		}

		const validation = await schema.validate(JSON.parse(data));
		if (!validation.success) {
			throw validation.error;
		}

		return validation.value as unknown as UIMessageChunk;
	};

	const applyStreamMetadata = (metadata: StreamMessageMetadata | undefined) => {
		if (!metadata) {
			return;
		}

		if (metadata.model) {
			options.setModel?.(metadata.model);
		}
		if (metadata.reasoning) {
			options.setReasoning?.(metadata.reasoning);
		}
	};

	const parseMessageMetadata = (
		value: unknown,
	): StreamMessageMetadata | undefined => {
		if (!value || typeof value !== "object") {
			return undefined;
		}

		const candidate = value as { model?: unknown; reasoning?: unknown };
		return {
			model: typeof candidate.model === "string" ? candidate.model : undefined,
			reasoning:
				typeof candidate.reasoning === "string"
					? candidate.reasoning
					: undefined,
		};
	};

	const isModeChangeChunk = (
		chunk: UIMessageChunk,
	): chunk is UIMessageChunk & {
		type: "data-mode-change";
		data: { mode?: string };
	} => {
		return chunk.type === "data-mode-change";
	};

	const isThreadNameChunk = (
		chunk: UIMessageChunk,
	): chunk is UIMessageChunk & {
		type: "data-thread-name";
		data: { name?: string };
	} => {
		return chunk.type === "data-thread-name";
	};

	const isMessageMetadataChunk = (
		chunk: UIMessageChunk,
	): chunk is UIMessageChunk & {
		type: "message-metadata";
		messageMetadata?: unknown;
	} => {
		return chunk.type === "message-metadata";
	};

	const isStartChunk = (
		chunk: UIMessageChunk,
	): chunk is UIMessageChunk & {
		type: "start";
		messageId?: string;
		messageMetadata?: unknown;
	} => {
		return chunk.type === "start";
	};

	const throwIfStreamErrored = (stream: ActiveAssistantStream) => {
		if (!stream.error) {
			return;
		}

		if (stream.error instanceof Error) {
			throw stream.error;
		}

		throw new Error(String(stream.error));
	};

	const waitForStreamingUpdate = async (stream: ActiveAssistantStream) => {
		await Promise.resolve();
		await Promise.resolve();
		throwIfStreamErrored(stream);
	};

	const closeActiveAssistantStream = async (
		expectedStream: ActiveAssistantStream | null = activeAssistantStream,
	) => {
		if (!expectedStream) {
			return;
		}

		if (activeAssistantStream === expectedStream) {
			activeAssistantStream = null;
		}

		try {
			await expectedStream.writer.close();
		} catch {
			// The stream may already be closed when replay or reset finishes.
		}

		await expectedStream.readerTask;
		throwIfStreamErrored(expectedStream);
	};

	const startAssistantStream = async (messageId: string | undefined) => {
		if (!messageId) {
			throw new Error("Received start chunk without a messageId");
		}

		if (activeAssistantStream) {
			await closeActiveAssistantStream();
		}

		const seedMessage = getTargetMessages().find(
			(message) => message.id === messageId,
		);
		const transform = new TransformStream<UIMessageChunk, UIMessageChunk>();
		const stream: ActiveAssistantStream = {
			error: null,
			messageId,
			readerTask: Promise.resolve(),
			writer: transform.writable.getWriter(),
		};

		stream.readerTask = (async () => {
			try {
				for await (const nextMessage of readUIMessageStream({
					message: seedMessage ? structuredClone(seedMessage) : undefined,
					stream: transform.readable,
				})) {
					upsertMessage(nextMessage);
				}
			} catch (error) {
				stream.error = error;
			} finally {
				if (activeAssistantStream === stream) {
					activeAssistantStream = null;
				}
			}
		})();

		activeAssistantStream = stream;
	};

	const isUserMessageChunk = (
		chunk: UIMessageChunk,
	): chunk is UIMessageChunk & UserMessageChunk => {
		return chunk.type === "data-user-message";
	};

	const applyChunkEvent = async (chunk: UIMessageChunk) => {
		if (isModeChangeChunk(chunk) && typeof chunk.data?.mode === "string") {
			options.setMode?.(chunk.data.mode);
			return;
		}

		if (isThreadNameChunk(chunk) && typeof chunk.data?.name === "string") {
			options.setThreadName?.(chunk.data.name);
			return;
		}

		if (isMessageMetadataChunk(chunk)) {
			applyStreamMetadata(parseMessageMetadata(chunk.messageMetadata));
			return;
		}

		if (isUserMessageChunk(chunk)) {
			const message = await validateMessage(chunk.data.message);
			upsertMessage(message, chunk.data.insertBeforeMessageId);
			return;
		}

		if (isStartChunk(chunk)) {
			applyStreamMetadata(parseMessageMetadata(chunk.messageMetadata));
			await startAssistantStream(chunk.messageId);
		}

		const activeStream = activeAssistantStream;
		if (!activeStream) {
			throw new Error(`Received ${chunk.type} chunk before a start chunk`);
		}

		await activeStream.writer.write(chunk);

		if (terminalChunkTypes.has(chunk.type)) {
			await closeActiveAssistantStream(activeStream);
			return;
		}

		await waitForStreamingUpdate(activeStream);
	};

	const applyEvent = async (event: ChatStreamEvent) => {
		if (event.event === "done") {
			await closeActiveAssistantStream();
			return;
		}

		if (event.event === "history-start") {
			historyMessages = [];
			return;
		}

		if (event.event === "history-message") {
			upsertMessage(await parseMessageEvent(event.data));
			return;
		}

		if (event.event === "history-end") {
			if (historyMessages !== null) {
				options.setMessages(historyMessages);
				historyMessages = null;
			}
			return;
		}

		await applyChunkEvent(await parseChunkEvent(event.data));
	};

	const handleStreamEvent = (event: ChatStreamEvent) => {
		const task = updateQueue
			.catch(() => undefined)
			.then(() => applyEvent(event));
		updateQueue = task;
		return task;
	};

	const reset = async () => {
		historyMessages = null;
		await closeActiveAssistantStream();
		options.setMessages([]);
	};

	return {
		get isBufferingHistory() {
			return historyMessages !== null;
		},
		handleStreamEvent,
		reset,
	};
}

export function bindChatStreamEventSource(
	eventSource: ChatStreamEventSource,
	streamState: Pick<
		ReturnType<typeof createChatStreamState>,
		"handleStreamEvent"
	>,
	options: ChatStreamEventSourceOptions = {},
) {
	const handleError = (error: unknown) => {
		if (options.onError) {
			options.onError(error);
			return;
		}

		console.error("Failed to process chat stream event", error);
	};

	const dispatchEvent = (event: ChatStreamEvent) => {
		void streamState.handleStreamEvent(event).catch(handleError);
	};

	const listeners = {
		"history-start": (event: MessageEvent<string>) => {
			dispatchEvent({ event: "history-start", data: event.data });
		},
		"history-message": (event: MessageEvent<string>) => {
			dispatchEvent({ event: "history-message", data: event.data });
		},
		"history-end": (event: MessageEvent<string>) => {
			dispatchEvent({ event: "history-end", data: event.data });
		},
		chunk: (event: MessageEvent<string>) => {
			dispatchEvent({ event: "chunk", data: event.data });
		},
		done: (event: MessageEvent<string>) => {
			dispatchEvent({ event: "done", data: event.data });
		},
	} satisfies Record<
		ChatStreamEventName,
		(event: MessageEvent<string>) => void
	>;

	for (const [eventName, listener] of Object.entries(listeners) as Array<
		[ChatStreamEventName, (event: MessageEvent<string>) => void]
	>) {
		eventSource.addEventListener(eventName, listener);
	}

	return () => {
		for (const [eventName, listener] of Object.entries(listeners) as Array<
			[ChatStreamEventName, (event: MessageEvent<string>) => void]
		>) {
			eventSource.removeEventListener(eventName, listener);
		}
	};
}
