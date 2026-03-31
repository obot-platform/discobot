import type { UIMessageChunk } from "ai";
import { safeValidateUIMessages, uiMessageChunkSchema } from "ai";

import type {
	ChatMessage,
	ChatMessageDataTypes,
	ChatMessageMetadata,
} from "$lib/api-types";

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
			event: "ping";
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

export type ChatStreamChunk = UIMessageChunk<
	ChatMessageMetadata,
	ChatMessageDataTypes
>;

export async function parseChatStreamMessageValue(
	message: unknown,
): Promise<ChatMessage> {
	const validation = await safeValidateUIMessages<ChatMessage>({
		messages: [message],
	});
	if (!validation.success) {
		throw validation.error;
	}

	return validation.data[0];
}

export async function parseChatStreamMessage(
	data: string,
): Promise<ChatMessage> {
	return parseChatStreamMessageValue(JSON.parse(data));
}

export async function parseChatStreamChunk(
	data: string,
): Promise<ChatStreamChunk> {
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

	return validation.value as ChatStreamChunk;
}

export function bindChatStreamEventSource(
	eventSource: ChatStreamEventSource,
	streamState: {
		handleStreamEvent: (event: ChatStreamEvent) => Promise<unknown>;
	},
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
		ping: (event: MessageEvent<string>) => {
			dispatchEvent({ event: "ping", data: event.data });
		},
	} satisfies Record<
		ChatStreamEventName,
		(event: MessageEvent<string>) => void
	>;

	const eventNames: ChatStreamEventName[] = [
		"history-start",
		"history-message",
		"history-end",
		"chunk",
		"ping",
	];

	for (const eventName of eventNames) {
		eventSource.addEventListener(eventName, listeners[eventName]);
	}

	return () => {
		for (const eventName of eventNames) {
			eventSource.removeEventListener(eventName, listeners[eventName]);
		}
	};
}
