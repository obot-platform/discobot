import { api } from "$lib/api-client";
import { StartChatError } from "$lib/api-client";
import { useAppContext } from "$lib/context/app-context.svelte";
import type { ChatMessage, Thread } from "$lib/api-types";
import {
	bindChatStreamEventSource,
	createChatStreamState,
} from "$lib/thread/conversation-stream";
import {
	addToolApprovalResponse,
	createUserMessageFromParts,
	getPendingQuestionApprovalId,
	hasUserMessageContent,
} from "$lib/session/domains/session-domain.helpers";

type CreateConversationDomainArgs = {
	sessionId: string;
	hasSession: () => boolean;
	threadId: string;
	refreshThread: () => Promise<void>;
	applyThreadUpdate?: (thread: Thread) => void;
	refreshSessionState?: () => Promise<void>;
	afterTurn?: () => Promise<void>;
};

function normalizeModelId(modelId: string | null): string | undefined {
	if (!modelId) {
		return undefined;
	}
	return modelId.endsWith(":thinking")
		? modelId.slice(0, -":thinking".length)
		: modelId;
}

export function getSubmitMessages(userMessage: ChatMessage): ChatMessage[] {
	const { provisional: _provisional, ...submittedMessage } = userMessage;
	return [submittedMessage];
}

export function removeProvisionalSubmitMessage(
	messages: ChatMessage[],
	messageId: string,
): ChatMessage[] {
	return messages.filter((message) => message.id !== messageId);
}

export function getPendingQuestionState(
	messages: ChatMessage[],
	pendingQuestionId: string | null,
): { hasPendingQuestion: boolean; pendingQuestionId: string | null } {
	const messagePendingQuestionId = getPendingQuestionApprovalId(messages);
	const resolvedPendingQuestionId =
		messagePendingQuestionId ?? pendingQuestionId;
	return {
		hasPendingQuestion: resolvedPendingQuestionId !== null,
		pendingQuestionId: resolvedPendingQuestionId,
	};
}

export function getStartChatErrorDetails(error: unknown): {
	message: string | null;
	pendingQuestionId: string | null;
	completionId: string | null;
} {
	if (error instanceof StartChatError) {
		const autoResuming =
			error.code === "interrupted_turn_requires_resume" &&
			typeof error.completionId === "string" &&
			error.completionId.length > 0;
		return {
			message: autoResuming ? null : error.message,
			pendingQuestionId:
				error.code === "pending_question_requires_answer"
					? (error.questionId ?? null)
					: null,
			completionId: error.completionId ?? null,
		};
	}
	return {
		message: error instanceof Error ? error.message : "Failed to start chat",
		pendingQuestionId: null,
		completionId: null,
	};
}

export function createConversationDomain(args: CreateConversationDomainArgs) {
	const app = useAppContext();
	let messages = $state<ChatMessage[]>([]);
	let historyReplayVersion = $state(0);
	let streamError = $state<string | null>(null);
	let fatalStreamError = $state(false);
	let completionRunning = $state(false);
	let pendingQuestionId = $state<string | null>(null);
	let loadStatus = $state<"idle" | "loading" | "ready" | "error">("idle");
	let activeSource: EventSource | null = null;
	let unbindStream: (() => void) | null = null;
	let activeStreamKey: string | null = null;
	let pendingLoadPromise: Promise<void> | null = null;
	let resolvePendingLoad: (() => void) | null = null;
	let rejectPendingLoad: ((error?: unknown) => void) | null = null;

	const status = $derived.by(() => {
		if (completionRunning) {
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
	const pendingQuestionState = $derived.by(() =>
		getPendingQuestionState(messages, pendingQuestionId),
	);

	const streamState = createChatStreamState({
		getMessages: () => messages,
		setMessages: (nextMessages) => {
			messages = nextMessages;
		},
		onCompletionStatus: ({ isRunning }) => {
			const wasRunning = completionRunning;
			streamError = null;
			completionRunning = isRunning;
			if (isRunning) {
				pendingQuestionId = null;
				return;
			}
			if (wasRunning) {
				return args.afterTurn?.();
			}
		},
		onHistoryReplayEnd: () => {
			fatalStreamError = false;
			historyReplayVersion += 1;
			pendingQuestionId = getPendingQuestionApprovalId(messages);
			if (loadStatus === "loading") {
				loadStatus = "ready";
			}
			resolvePendingLoad?.();
			pendingLoadPromise = null;
			resolvePendingLoad = null;
			rejectPendingLoad = null;
		},
		onChunkError: (errorText) => {
			fatalStreamError = true;
			completionRunning = false;
			streamError = errorText;
			if (loadStatus === "loading") {
				loadStatus = "error";
			}
			rejectPendingLoad?.(new Error(errorText));
			pendingLoadPromise = null;
			resolvePendingLoad = null;
			rejectPendingLoad = null;
		},
		onThreadUpdate: (thread) => {
			args.applyThreadUpdate?.(thread);
		},
	});

	function disconnectStream() {
		unbindStream?.();
		unbindStream = null;
		activeSource?.close();
		activeSource = null;
		activeStreamKey = null;
	}

	function resetLoadPromise() {
		pendingLoadPromise = null;
		resolvePendingLoad = null;
		rejectPendingLoad = null;
	}

	function beginLoadPromise() {
		if (pendingLoadPromise) {
			return pendingLoadPromise;
		}
		pendingLoadPromise = new Promise<void>((resolve, reject) => {
			resolvePendingLoad = resolve;
			rejectPendingLoad = reject;
		});
		return pendingLoadPromise;
	}

	function rejectLoad(error: unknown, fallbackMessage: string) {
		loadStatus = "error";
		streamError = error instanceof Error ? error.message : fallbackMessage;
		rejectPendingLoad?.(error);
		resetLoadPromise();
	}

	function streamKey(sessionId: string) {
		return `${sessionId}:${args.threadId}`;
	}

	function ensureStream() {
		if (typeof window === "undefined") {
			return;
		}
		if (fatalStreamError) {
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
			void Promise.all([
				args.refreshThread(),
				args.refreshSessionState?.(),
			]).catch((error) => {
				console.error(
					"Failed to refresh thread state after chat stream connected",
					error,
				);
			});
		};
		unbindStream = bindChatStreamEventSource(source, streamState, {
			onError: (error) => {
				fatalStreamError = true;
				streamError =
					error instanceof Error
						? error.message
						: "Failed to process chat stream";
				disconnectStream();
				if (loadStatus === "loading") {
					rejectLoad(error, "Failed to load messages");
				}
			},
		});
		source.onerror = () => {
			if (activeSource !== source) {
				return;
			}

			if (source.readyState === EventSource.CLOSED) {
				completionRunning = false;
				const error = new Error("Lost chat stream connection");
				streamError = error.message;
				disconnectStream();
				if (loadStatus === "loading") {
					rejectLoad(error, "Lost chat stream connection");
					return;
				}
				if (args.hasSession() && !fatalStreamError) {
					ensureStream();
				}
			}
		};
	}

	async function loadFromStream(forceReconnect = false) {
		if (!forceReconnect && pendingLoadPromise) {
			return pendingLoadPromise;
		}
		if (fatalStreamError && !forceReconnect) {
			throw new Error(streamError ?? "Failed to process chat stream");
		}
		if (forceReconnect) {
			fatalStreamError = false;
		}
		if (forceReconnect || activeStreamKey === streamKey(args.sessionId)) {
			disconnectStream();
		}
		loadStatus = "loading";
		streamError = null;
		ensureStream();
		return beginLoadPromise();
	}

	async function load() {
		if (!args.hasSession()) {
			loadStatus = "idle";
			streamError = null;
			completionRunning = false;
			pendingQuestionId = null;
			disconnectStream();
			resetLoadPromise();
			return;
		}
		if (loadStatus === "ready") {
			ensureStream();
			return;
		}
		await loadFromStream();
	}

	async function refresh() {
		if (!args.hasSession()) {
			loadStatus = "idle";
			streamError = null;
			completionRunning = false;
			pendingQuestionId = null;
			disconnectStream();
			resetLoadPromise();
			return;
		}
		await loadFromStream(true);
	}

	return {
		get messages() {
			return messages;
		},
		get historyReplayVersion() {
			return historyReplayVersion;
		},
		get status() {
			return status;
		},
		get error() {
			return error;
		},
		get hasPendingQuestion() {
			return pendingQuestionState.hasPendingQuestion;
		},
		get pendingQuestionId() {
			return pendingQuestionState.pendingQuestionId;
		},
		load,
		submit: async ({
			parts,
			mode,
			modelId,
			reasoning,
			workspaceId,
			workspaceType,
			workspacePath,
			allowEmptyPendingMessage,
		}: {
			parts: ChatMessage["parts"];
			mode: "build" | "plan";
			modelId: string | null;
			reasoning: string | undefined;
			workspaceId?: string;
			workspaceType?: "local" | "git" | null;
			workspacePath?: string | null;
			allowEmptyPendingMessage?: boolean;
		}) => {
			const hasMessageContent = hasUserMessageContent(parts);
			if (
				!hasMessageContent &&
				!(allowEmptyPendingMessage && !args.hasSession())
			) {
				return;
			}

			streamError = null;
			const nextModel = normalizeModelId(modelId ?? null) ?? "";
			const nextReasoning = reasoning ?? "";
			const nextMode = mode === "plan" ? "plan" : "build";
			const userMessage = hasMessageContent
				? createUserMessageFromParts(parts, {
						provisional: true,
					})
				: null;

			if (userMessage) {
				messages = [...messages, userMessage];
			}

			try {
				if (!args.hasSession()) {
					const response = await app.chat({
						sessionId: args.sessionId,
						threadId: args.threadId,
						messages: userMessage ? getSubmitMessages(userMessage) : [],
						...(workspaceId ? { workspaceId } : {}),
						...(workspaceType && workspacePath
							? {
									workspaceType,
									workspacePath,
								}
							: {}),
						model: nextModel,
						reasoning: nextReasoning,
						mode: nextMode,
					});
					return {
						sessionId: response.sessionId,
						threadId: response.threadId,
						materialized: true,
					};
				}

				ensureStream();
				await app.chat({
					sessionId: args.sessionId,
					threadId: args.threadId,
					messages: userMessage ? getSubmitMessages(userMessage) : [],
					model: nextModel,
					reasoning: nextReasoning,
					mode: nextMode,
				});
				return {
					sessionId: args.sessionId,
					threadId: args.threadId,
					materialized: false,
				};
			} catch (error) {
				completionRunning = false;
				const errorDetails = getStartChatErrorDetails(error);
				if (args.hasSession()) {
					await refresh();
				} else if (userMessage) {
					messages = removeProvisionalSubmitMessage(messages, userMessage.id);
				}
				pendingQuestionId =
					getPendingQuestionApprovalId(messages) ??
					errorDetails.pendingQuestionId;
				streamError = errorDetails.message;
				throw error;
			}
		},
		cancel: async () => {
			if (!args.hasSession()) {
				return;
			}
			await api.cancelThreadChat(args.sessionId, args.threadId);
			completionRunning = false;
			await refresh();
		},
		refresh,
		addToolApprovalResponse: ({
			id,
			approved,
			reason,
		}: {
			id: string;
			approved: boolean;
			reason?: string;
		}) => {
			addToolApprovalResponse(messages, { id, approved, reason });
		},
		dispose: () => {
			loadStatus = "idle";
			completionRunning = false;
			pendingQuestionId = null;
			resetLoadPromise();
			disconnectStream();
		},
	};
}
