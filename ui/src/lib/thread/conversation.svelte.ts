import { api } from "$lib/api-client";
import { useAppContext } from "$lib/context/app-context.svelte";
import type { ChatMessage, Session } from "$lib/api-types";
import {
	bindChatStreamEventSource,
	createChatStreamState,
} from "$lib/thread/conversation-stream";
import { createUserMessage } from "$lib/session/domains/session-domain.helpers";

type CreateConversationDomainArgs = {
	sessionId: string;
	hasSession: () => boolean;
	getSessionStatus: () => Session["status"] | null;
	threadId: string;
	refreshThread: () => Promise<void>;
	afterTurn?: () => Promise<void>;
};

function normalizeModelId(modelId: string | null): string | undefined {
	if (!modelId) {
		return undefined;
	}
	return modelId.endsWith(":thinking") ? modelId.slice(0, -":thinking".length) : modelId;
}

export function getSubmitMessages(userMessage: ChatMessage): ChatMessage[] {
	const { provisional: _provisional, ...submittedMessage } = userMessage;
	return [submittedMessage];
}

export function createConversationDomain(args: CreateConversationDomainArgs) {
	const app = useAppContext();
	let messages = $state<ChatMessage[]>([]);
	let streamError = $state<string | null>(null);
	let streamStatus = $state<"idle" | "streaming" | null>(null);
	let loadStatus = $state<"idle" | "loading" | "ready" | "error">("idle");
	let activeSource: EventSource | null = null;
	let unbindStream: (() => void) | null = null;
	let activeStreamKey: string | null = null;

	const status = $derived.by(() => {
		if (streamStatus === "streaming") {
			return "streaming" as const;
		}
		if (loadStatus === "loading") {
			return "loading" as const;
		}
		if (!args.hasSession()) {
			return "idle" as const;
		}
		if (streamError || loadStatus === "error") {
			return "error" as const;
		}
		return "ready" as const;
	});
	const error = $derived.by(() => streamError);

	const streamState = createChatStreamState({
		getMessages: () => messages,
		setMessages: (nextMessages) => {
			messages = nextMessages;
		},
		onStart: () => {
			streamError = null;
			streamStatus = "streaming";
			void args.refreshThread();
		},
		onFinish: () => {
			streamStatus = null;
			void args.afterTurn?.();
		},
		onChunkError: (errorText) => {
			streamStatus = null;
			streamError = errorText;
		},
		setMode: () => {
			void args.refreshThread();
		},
		setModel: () => {
			void args.refreshThread();
		},
		setReasoning: () => {
			void args.refreshThread();
		},
	});

	function disconnectStream() {
		unbindStream?.();
		unbindStream = null;
		activeSource?.close();
		activeSource = null;
		activeStreamKey = null;
	}

	function streamKey(sessionId: string) {
		return `${sessionId}:${args.threadId}`;
	}

	function syncStream() {
		if (!args.hasSession()) {
			disconnectStream();
			streamStatus = null;
			return;
		}
		if (args.getSessionStatus() === "ready") {
			ensureStream();
			return;
		}
		disconnectStream();
	}

	function ensureStream() {
		if (typeof window === "undefined") {
			return;
		}
		if (activeStreamKey === streamKey(args.sessionId)) {
			return;
		}

		disconnectStream();
		streamError = null;
		activeStreamKey = streamKey(args.sessionId);
		const source = new EventSource(
			api.getThreadChatStreamUrl(args.sessionId, args.threadId, true),
		);
		activeSource = source;
		source.onopen = () => {
			if (activeSource !== source) {
				return;
			}
			streamError = null;
		};
		unbindStream = bindChatStreamEventSource(source, streamState, {
			onError: (error) => {
				streamError = error instanceof Error ? error.message : "Failed to process chat stream";
			},
		});
		source.onerror = () => {
			if (activeSource !== source) {
				return;
			}

			if (source.readyState === EventSource.CLOSED) {
				streamError = "Lost chat stream connection";
				disconnectStream();
				if (args.hasSession() && args.getSessionStatus() === "ready") {
					ensureStream();
				}
			}
		};
	}

	async function refreshMessages() {
		const { messages: nextMessages } = await api.getThreadMessages(args.sessionId, args.threadId);
		messages = nextMessages;
	}

	async function load() {
		if (!args.hasSession()) {
			loadStatus = "idle";
			streamError = null;
			streamStatus = null;
			disconnectStream();
			return;
		}
		if (loadStatus !== "ready") {
			loadStatus = "loading";
			streamError = null;
			try {
				await refreshMessages();
				loadStatus = "ready";
			} catch (error) {
				loadStatus = "error";
				streamError = error instanceof Error ? error.message : "Failed to load messages";
				throw error;
			}
		}
		syncStream();
	}

	async function refresh() {
		if (!args.hasSession()) {
			loadStatus = "idle";
			streamError = null;
			streamStatus = null;
			disconnectStream();
			return;
		}
		loadStatus = "loading";
		streamError = null;
		try {
			await refreshMessages();
			loadStatus = "ready";
			syncStream();
		} catch (error) {
			loadStatus = "error";
			streamError = error instanceof Error ? error.message : "Failed to load messages";
			throw error;
		}
	}

	return {
		get messages() {
			return messages;
		},
		get status() {
			return status;
		},
		get error() {
			return error;
		},
		load,
		submit: async ({
			text,
			mode,
			modelId,
			reasoning,
		}: {
			text: string;
			mode: "build" | "plan";
			modelId: string | null;
			reasoning: boolean;
		}) => {
			const trimmedText = text.trim();
			if (!trimmedText) {
				return;
			}

			streamError = null;
			const nextModel = normalizeModelId(modelId ?? null);
			const nextReasoning = reasoning ? "enabled" : undefined;
			const nextMode = mode === "plan" ? "plan" : "";
			const userMessage = createUserMessage(trimmedText, { provisional: true });

			if (!args.hasSession()) {
				return;
			}

			messages = [...messages, userMessage];

			try {
				ensureStream();
				await app.chat({
					sessionId: args.sessionId,
					threadId: args.threadId,
					messages: getSubmitMessages(userMessage),
					...(nextModel ? { model: nextModel } : {}),
					...(nextReasoning !== undefined ? { reasoning: nextReasoning } : {}),
					mode: nextMode,
				});
			} catch (error) {
				streamStatus = null;
				const errorMessage = error instanceof Error ? error.message : "Failed to start chat";
				await refresh();
				streamError = errorMessage;
				throw error;
			}
		},
		cancel: async () => {
			if (!args.hasSession()) {
				return;
			}
			await api.cancelThreadChat(args.sessionId, args.threadId);
			streamStatus = null;
			await refresh();
		},
		refresh,
		dispose: () => {
			loadStatus = "idle";
			streamStatus = null;
			disconnectStream();
		},
	};
}
