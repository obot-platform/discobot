import { getContext, hasContext, setContext } from "svelte";

import type { Thread } from "$lib/api-types";
import {
	clearComposerDraft,
	readComposerDraft,
	resolveComposerDraftStorageKey,
	writeComposerDraft,
} from "$lib/composer-draft-storage";
import { useAppContext } from "$lib/context/app-context.svelte";
import { useSessionContext } from "$lib/context/session-context.svelte";
import { createConversationDomain } from "$lib/thread/conversation.svelte";
import { getPlanEntries } from "$lib/session/domains/session-domain.helpers";
import type {
	SessionContextValue,
	ThreadContextValue,
} from "$lib/session/session-context.types";
import type { ThreadSummary } from "$lib/shell-types";

const THREAD_CONTEXT_KEY = Symbol.for("discobot-ui-thread-context");
const COMPOSER_DRAFT_PERSIST_DELAY_MS = 300;

export function normalizeThreadComposerMode(
	mode: string | null | undefined,
): "build" | "plan" {
	if (!mode || mode === "" || mode === "build") {
		return "build";
	}
	return "plan";
}

export function normalizeThreadComposerReasoning(
	reasoning: string | null | undefined,
): string | undefined {
	return reasoning && reasoning.length > 0 ? reasoning : undefined;
}

export function parseComposerModelSelection(
	modelId: string | null | undefined,
): { modelId: string | null } {
	return {
		modelId: modelId && modelId.length > 0 ? modelId : null,
	};
}

export function getThreadComposerValues(
	thread: ThreadSummary | null,
	defaultModel: string | null,
): {
	mode: "build" | "plan";
	modelId: string | null;
	reasoning: string | undefined;
} {
	return {
		mode: normalizeThreadComposerMode(thread?.mode),
		modelId: thread?.model ?? defaultModel,
		reasoning: normalizeThreadComposerReasoning(thread?.reasoning),
	};
}

export function getThreadComposerValuesKey(values: {
	mode: "build" | "plan";
	modelId: string | null;
	reasoning: string | undefined;
}): string {
	return JSON.stringify(values);
}

export function resolveThreadComposerSubmitValues({
	mode,
	modelId,
	reasoning,
	nextMode,
	nextModelId,
	nextReasoning,
}: {
	mode: "build" | "plan";
	modelId: string | null;
	reasoning: string | undefined;
	nextMode: "build" | "plan" | undefined;
	nextModelId: string | null | undefined;
	nextReasoning: string | undefined;
}): {
	mode: "build" | "plan";
	modelId: string | null;
	reasoning: string | undefined;
} {
	const resolvedModelId = nextModelId !== undefined ? nextModelId : modelId;
	return {
		mode: nextMode ?? mode,
		modelId: resolvedModelId,
		reasoning: resolvedModelId
			? normalizeThreadComposerReasoning(nextReasoning ?? reasoning)
			: undefined,
	};
}

export function clearComposerDraftState({
	cancelPersist,
	clearStoredDraft,
	clearInMemoryDraft,
}: {
	cancelPersist: () => void;
	clearStoredDraft: () => void;
	clearInMemoryDraft: () => void;
}): void {
	cancelPersist();
	clearStoredDraft();
	clearInMemoryDraft();
}

export function applyStreamedThreadUpdate({
	sessionId,
	sessionName,
	sessionDisplayName,
	previousThreadName,
	thread,
	upsertThread,
	refreshRecentThread,
	reloadSession,
}: {
	sessionId: string;
	sessionName: string;
	sessionDisplayName?: string | null;
	previousThreadName?: string | null;
	thread: Thread;
	upsertThread: (thread: Thread) => void;
	refreshRecentThread: (payload: {
		sessionId: string;
		sessionName: string;
		threadId: string;
		threadName: string;
		state?: Thread["state"];
		lastMessage: string;
	}) => void;
	reloadSession: () => void | Promise<void>;
}): void {
	upsertThread(thread);
	refreshRecentThread({
		sessionId,
		sessionName,
		threadId: thread.id,
		threadName: thread.name,
		state: thread.state,
		lastMessage: thread.lastMessage || "",
	});

	if (
		thread.id !== sessionId ||
		sessionDisplayName ||
		previousThreadName === thread.name ||
		thread.name.trim().length === 0
	) {
		return;
	}

	void reloadSession();
}

function createThreadContext(
	threadId: string,
	session: SessionContextValue,
): ThreadContextValue {
	const app = useAppContext();
	const hasSession = $derived.by(() => session.current !== null);
	const refreshSessionState = async () => {
		await Promise.all([
			session.files.refresh(),
			session.services.refresh(),
			session.envSets.refresh(),
			session.hooks.refresh(),
		]);
		await app.sessions.reloadSession(session.sessionId);
	};

	const conversation = createConversationDomain({
		sessionId: session.sessionId,
		hasSession: () => hasSession,
		getSessionStatus: () => session.current?.status ?? null,
		threadId,
		refreshThread: async () => {
			await session.threads.refreshThread(threadId);
		},
		applyThreadUpdate: (thread) => {
			const previousThread = session.stores.threads.get(thread.id);
			applyStreamedThreadUpdate({
				sessionId: session.sessionId,
				sessionName:
					session.current?.displayName ||
					session.current?.name ||
					"New Session",
				sessionDisplayName: session.current?.displayName ?? null,
				previousThreadName: previousThread?.name ?? null,
				thread,
				upsertThread: (nextThread) => {
					session.stores.threads.upsert(nextThread);
				},
				refreshRecentThread: app.sessions.refreshRecentThread,
				reloadSession: () => app.sessions.reloadSession(session.sessionId),
			});
		},
		refreshSessionState,
		afterTurn: async () => {
			await session.threads.refreshThread(threadId);
			await refreshSessionState();
		},
	});
	const threadSummary = $derived.by(
		() => session.threads.list.find((t) => t.id === threadId) ?? null,
	);
	const sourceComposerValues = $derived.by(() =>
		getThreadComposerValues(
			threadSummary,
			app.preferences.defaultModel || null,
		),
	);
	const initialComposerValues = getThreadComposerValues(
		session.threads.list.find((t) => t.id === threadId) ?? null,
		app.preferences.defaultModel || null,
	);
	let sourceComposerValuesKey = $state(
		getThreadComposerValuesKey(initialComposerValues),
	);
	let mode = $state<"build" | "plan">(initialComposerValues.mode);
	let modelId = $state<string | null>(initialComposerValues.modelId);
	let reasoning = $state<string | undefined>(initialComposerValues.reasoning);
	let nextMode = $state<"build" | "plan" | undefined>(undefined);
	let nextModelId = $state<string | null | undefined>(undefined);
	let nextReasoning = $state<string | undefined>(undefined);
	let loadedComposerDraftStorageKey = $state<string | null>(null);
	let lastStoredComposerDraft = $state("");
	let composerDraftPersistTimer: ReturnType<typeof setTimeout> | null = null;

	$effect(() => {
		const nextSourceComposerValues = sourceComposerValues;
		const nextSourceComposerValuesKey = getThreadComposerValuesKey(
			nextSourceComposerValues,
		);
		if (nextSourceComposerValuesKey === sourceComposerValuesKey) {
			return;
		}
		sourceComposerValuesKey = nextSourceComposerValuesKey;
		mode = nextSourceComposerValues.mode;
		modelId = nextSourceComposerValues.modelId;
		reasoning = nextSourceComposerValues.reasoning;
	});

	const composerDraftStorageKey = $derived.by(() =>
		resolveComposerDraftStorageKey({
			isPending: session.isPending,
			threadId,
		}),
	);

	$effect(() => {
		const storageKey = composerDraftStorageKey;
		if (loadedComposerDraftStorageKey !== storageKey) {
			loadedComposerDraftStorageKey = storageKey;
			lastStoredComposerDraft = readComposerDraft(storageKey);
			if (session.ui.composerDraft !== lastStoredComposerDraft) {
				session.ui.setComposerDraft(lastStoredComposerDraft);
			}
			return;
		}

		const draft = session.ui.composerDraft;
		if (draft === lastStoredComposerDraft) {
			return;
		}

		if (composerDraftPersistTimer !== null) {
			clearTimeout(composerDraftPersistTimer);
		}

		composerDraftPersistTimer = setTimeout(() => {
			writeComposerDraft(storageKey, draft);
			lastStoredComposerDraft = draft;
			composerDraftPersistTimer = null;
		}, COMPOSER_DRAFT_PERSIST_DELAY_MS);

		return () => {
			if (composerDraftPersistTimer !== null) {
				clearTimeout(composerDraftPersistTimer);
				composerDraftPersistTimer = null;
			}
		};
	});

	const clearStoredComposerDraft = (storageKey = composerDraftStorageKey) => {
		clearComposerDraftState({
			cancelPersist: () => {
				if (composerDraftPersistTimer !== null) {
					clearTimeout(composerDraftPersistTimer);
					composerDraftPersistTimer = null;
				}
			},
			clearStoredDraft: () => {
				clearComposerDraft(storageKey);
				if (storageKey === composerDraftStorageKey) {
					lastStoredComposerDraft = "";
				}
			},
			clearInMemoryDraft: () => {
				session.ui.setComposerDraft("");
			},
		});
	};

	const clearNextComposerValues = () => {
		nextMode = undefined;
		nextModelId = undefined;
		nextReasoning = undefined;
	};

	const submit: ThreadContextValue["submit"] = async ({
		parts,
		workspaceId,
		workspaceType,
		workspacePath,
		allowEmptyPendingMessage,
	}) => {
		const submitValues = resolveThreadComposerSubmitValues({
			mode,
			modelId,
			reasoning,
			nextMode,
			nextModelId,
			nextReasoning,
		});
		const result = await conversation.submit({
			parts,
			mode: submitValues.mode,
			modelId: submitValues.modelId,
			reasoning: submitValues.reasoning,
			workspaceId,
			workspaceType,
			workspacePath,
			allowEmptyPendingMessage,
		});
		mode = submitValues.mode;
		modelId = submitValues.modelId;
		reasoning = submitValues.reasoning;
		clearNextComposerValues();
		return result;
	};

	return {
		get threadId() {
			return threadId;
		},
		get thread() {
			return threadSummary;
		},
		get mode() {
			return mode;
		},
		get modelId() {
			return modelId;
		},
		get reasoning() {
			return reasoning;
		},
		get nextMode() {
			return nextMode;
		},
		get nextModelId() {
			return nextModelId;
		},
		get nextReasoning() {
			return nextReasoning;
		},
		setNextMode: (value) => {
			nextMode = value;
		},
		setNextModelId: (value) => {
			nextModelId = value;
		},
		setNextReasoning: (value) => {
			nextReasoning = value;
		},
		clearNextComposerValues,
		get messages() {
			return conversation.messages;
		},
		get planEntries() {
			return getPlanEntries(conversation.messages);
		},
		get status() {
			return conversation.status;
		},
		get error() {
			return conversation.error;
		},
		get hasPendingQuestion() {
			return conversation.hasPendingQuestion;
		},
		get pendingQuestionId() {
			return conversation.pendingQuestionId;
		},
		clearComposerDraft: clearStoredComposerDraft,
		submit,
		cancel: conversation.cancel,
		load: conversation.load,
		refresh: conversation.refresh,
		addToolApprovalResponse: conversation.addToolApprovalResponse,
		dispose: () => {
			if (composerDraftPersistTimer !== null) {
				clearTimeout(composerDraftPersistTimer);
				composerDraftPersistTimer = null;
			}
			conversation.dispose();
		},
		get editorFiles() {
			return session.files.list;
		},
		get fileContents() {
			return session.files.contents;
		},
		get activeEnvSetIds() {
			return session.envSets.activeIds;
		},
		get activeEnvSets() {
			return session.envSets.active;
		},
		envSets: {
			get activeIds() {
				return session.envSets.activeIds;
			},
			get active() {
				return session.envSets.active;
			},
			toggle: session.envSets.toggle,
		},
	};
}

export function setThreadContext(threadId: string): ThreadContextValue {
	const session = useSessionContext();

	const existing = session.threadContexts.get(threadId);
	if (existing) {
		setContext(THREAD_CONTEXT_KEY, existing);
		return existing;
	}

	const ctx = createThreadContext(threadId, session);
	session.threadContexts.set(threadId, ctx);
	setContext(THREAD_CONTEXT_KEY, ctx);
	return ctx;
}

export function useThreadContext(): ThreadContextValue {
	const context = getContext<ThreadContextValue | undefined>(
		THREAD_CONTEXT_KEY,
	);
	if (!context) {
		throw new Error(
			"useThreadContext must be used within a ThreadContext provider",
		);
	}
	return context;
}

export function getThreadContextIfPresent(): ThreadContextValue | undefined {
	if (!hasContext(THREAD_CONTEXT_KEY)) {
		return undefined;
	}
	return getContext<ThreadContextValue | undefined>(THREAD_CONTEXT_KEY);
}
