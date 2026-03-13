import { createQuery, queryOptions } from "@tanstack/svelte-query";
import type { QueryClient } from "@tanstack/svelte-query";

import { api } from "$lib/api-client";
import { useAppContext } from "$lib/context/app-context.svelte";
import type { ChatMessage, Session } from "$lib/api-types";
import {
	bindChatStreamEventSource,
	createChatStreamState,
} from "$lib/session/domains/session-conversation.stream";
import type { SessionConversationDomain } from "$lib/session/session-context.types";
import { createUserMessage } from "$lib/session/domains/session-domain.helpers";

const CONVERSATION_DOMAIN = "conversation";

type CreateSessionConversationDomainArgs = {
	queryClient: QueryClient;
	getSession: () => Session | null;
	threadId: string;
	key: (...parts: string[]) => readonly unknown[];
	updateSession: (updater: (session: Session) => Session) => void;
	afterTurn?: () => Promise<void>;
};

function normalizeModelId(modelId: string | null): string | undefined {
	if (!modelId) {
		return undefined;
	}
	return modelId.endsWith(":thinking") ? modelId.slice(0, -":thinking".length) : modelId;
}

function messagesQueryOptions(args: CreateSessionConversationDomainArgs, sessionId: string) {
	return queryOptions({
		queryKey: args.key(CONVERSATION_DOMAIN, args.threadId),
		queryFn: async () => {
			const { messages } = await api.getThreadMessages(sessionId, args.threadId);
			return messages;
		},
		initialData: [],
	});
}

export function createSessionConversationDomain(
	args: CreateSessionConversationDomainArgs,
): SessionConversationDomain & { dispose: () => void } {
	const app = useAppContext();
	let streamError = $state<string | null>(null);
	let streamStatus = $state<"idle" | "streaming" | null>(null);
	let activeSource = $state<EventSource | null>(null);
	let unbindStream = $state<(() => void) | null>(null);
	let activeStreamKey = $state<string | null>(null);

	const messagesQuery = createQuery(() => {
		const sessionId = args.getSession()?.id;
		return queryOptions({
			queryKey: args.key(CONVERSATION_DOMAIN, args.threadId),
			queryFn: async () => {
				if (!sessionId) {
					return [];
				}
				const { messages } = await api.getThreadMessages(sessionId, args.threadId);
				return messages;
			},
			initialData: [],
		});
	});

	const messages = $derived.by(() => messagesQuery.data ?? []);
	const status = $derived.by(() => {
		if (streamStatus === "streaming") {
			return "streaming" as const;
		}
		if (!args.getSession()) {
			return "idle" as const;
		}
		if (messagesQuery.isPending) {
			return "loading" as const;
		}
		if (messagesQuery.isError || streamError) {
			return "error" as const;
		}
		return "ready" as const;
	});
	const error = $derived.by(() => {
		if (streamError) {
			return streamError;
		}
		return messagesQuery.error instanceof Error ? messagesQuery.error.message : null;
	});

	const streamState = createChatStreamState({
		getMessages: () => {
			return (
				args.queryClient.getQueryData<ChatMessage[]>(
					args.key(CONVERSATION_DOMAIN, args.threadId),
				) ?? []
			);
		},
		setMessages: (nextMessages) => {
			args.queryClient.setQueryData(args.key(CONVERSATION_DOMAIN, args.threadId), nextMessages);
		},
		setMode: (mode) => {
			const session = args.getSession();
			if (!session) {
				return;
			}
			args.updateSession((currentSession) => ({ ...currentSession, mode }));
		},
		setModel: (model) => {
			const session = args.getSession();
			if (!session) {
				return;
			}
			args.updateSession((currentSession) => ({ ...currentSession, model }));
		},
		setReasoning: (reasoning) => {
			const session = args.getSession();
			if (!session) {
				return;
			}
			args.updateSession((currentSession) => ({ ...currentSession, reasoning }));
		},
	});

	function closeStream() {
		unbindStream?.();
		unbindStream = null;
		activeSource?.close();
		activeSource = null;
		activeStreamKey = null;
		streamStatus = null;
	}

	function streamKey(sessionId: string) {
		return `${sessionId}:${args.threadId}`;
	}

	function buildStreamUrl(sessionId: string, replay: boolean) {
		const baseUrl = api.getThreadChatStreamUrl(sessionId, args.threadId);
		if (!replay) {
			return baseUrl;
		}
		return `${baseUrl}${baseUrl.includes("?") ? "&" : "?"}replay=true`;
	}

	function startStream(sessionId: string, replay: boolean) {
		if (typeof window === "undefined") {
			return;
		}

		closeStream();
		streamError = null;
		streamStatus = "streaming";
		activeStreamKey = streamKey(sessionId);
		const source = new EventSource(buildStreamUrl(sessionId, replay));
		activeSource = source;
		unbindStream = bindChatStreamEventSource(source, streamState, {
			onError: (error) => {
				streamError = error instanceof Error ? error.message : "Failed to process chat stream";
				closeStream();
			},
		});
		source.addEventListener("done", () => {
			closeStream();
			void messagesQuery.refetch();
			void args.afterTurn?.();
		});
		source.onerror = () => {
			if (activeSource !== source) {
				return;
			}
			streamError = "Lost chat stream connection";
			closeStream();
		};
	}

	$effect(() => {
		const session = args.getSession();
		if (!session) {
			closeStream();
			streamError = null;
			return;
		}

		if (session.status !== "running") {
			return;
		}

		const nextStreamKey = streamKey(session.id);
		if (activeStreamKey === nextStreamKey) {
			return;
		}

		startStream(session.id, true);
	});

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
		submit: async ({ text, mode, modelId, reasoning }) => {
			const trimmedText = text.trim();
			if (!trimmedText) {
				return;
			}

			streamError = null;
			const nextModel = normalizeModelId(modelId ?? null);
			const nextReasoning = reasoning ? "enabled" : "disabled";
			const nextMode = mode === "plan" ? "plan" : "";
			const userMessage = createUserMessage(trimmedText);
			const session = args.getSession();

			if (!session) {
				return;
			}

			const nextMessages = [...messages, userMessage];
			args.queryClient.setQueryData(args.key(CONVERSATION_DOMAIN, args.threadId), nextMessages);
			args.updateSession((currentSession) => ({
				...currentSession,
				model: nextModel,
				reasoning: nextReasoning,
				mode: nextMode,
			}));

			try {
				await app.chat({
					sessionId: session.id,
					threadId: args.threadId,
					messages: nextMessages,
					...(nextModel ? { model: nextModel } : {}),
					reasoning: nextReasoning,
					mode: nextMode,
				});
				startStream(session.id, false);
			} catch (error) {
				streamError = error instanceof Error ? error.message : "Failed to start chat";
				await messagesQuery.refetch();
				throw error;
			}
		},
		cancel: async () => {
			const session = args.getSession();
			if (!session) {
				return;
			}
			await api.cancelThreadChat(session.id, args.threadId);
			closeStream();
			await messagesQuery.refetch();
		},
		refresh: async () => {
			const session = args.getSession();
			if (!session) {
				return;
			}
			await args.queryClient.fetchQuery(messagesQueryOptions(args, session.id));
		},
		dispose: closeStream,
	};
}
