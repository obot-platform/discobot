import { getContext, hasContext, setContext } from "svelte";

import {
	clearComposerDraft,
	readComposerDraft,
	resolveComposerDraftStorageKey,
	writeComposerDraft,
} from "$lib/composer-draft-storage";
import { useAppContext } from "$lib/context/app-context.svelte";
import {
	createBackgroundRefresh,
	createCoalescedReload,
} from "$lib/context/create-coalesced-reload";
import { useSessionContext } from "$lib/context/session-context.svelte";
import { createConversationDomain } from "$lib/thread/conversation.svelte";
import { getPlanEntries } from "$lib/session/domains/session-domain.helpers";
import type {
	SessionContextValue,
	ThreadContextValue,
} from "$lib/session/session-context.types";

const THREAD_CONTEXT_KEY = Symbol.for("discobot-ui-thread-context");
const COMPOSER_DRAFT_PERSIST_DELAY_MS = 300;

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

function createThreadContext(
	threadId: string,
	session: SessionContextValue,
): ThreadContextValue {
	const app = useAppContext();
	const hasSession = $derived.by(() => session.current !== null);
	const sessionStatus = $derived.by(() => session.current?.status ?? null);
	const refreshSessionState = createCoalescedReload(async () => {
		await app.sessions.reloadSession(session.sessionId);
	});
	const refreshThreadState = createCoalescedReload(async () => {
		await session.threads.refreshThread(threadId);
	});
	const refreshThreadAndSessionState = createCoalescedReload(async () => {
		await refreshThreadState();
		await refreshSessionState();
	});

	const conversation = createConversationDomain({
		sessionId: session.sessionId,
		hasSession: () => hasSession,
		getSessionStatus: () => sessionStatus,
		threadId,
		refreshThread: refreshThreadState,
		refreshSessionState,
		afterTurn: refreshThreadAndSessionState,
	});
	let loadedComposerDraftStorageKey = $state<string | null>(null);
	let lastStoredComposerDraft = $state("");
	let composerDraftPersistTimer: ReturnType<typeof setTimeout> | null = null;

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

	const clearStoredComposerDraft = () => {
		clearComposerDraftState({
			cancelPersist: () => {
				if (composerDraftPersistTimer !== null) {
					clearTimeout(composerDraftPersistTimer);
					composerDraftPersistTimer = null;
				}
			},
			clearStoredDraft: () => {
				clearComposerDraft(composerDraftStorageKey);
			},
			clearInMemoryDraft: () => {
				session.ui.setComposerDraft("");
			},
		});
	};
	const loadConversation = createCoalescedReload(async () => {
		await conversation.load();
	});
	const refreshConversationNow = createCoalescedReload(async () => {
		await conversation.refresh();
	});
	const refreshConversation = createBackgroundRefresh(
		refreshConversationNow,
		`[ThreadContext] Failed to refresh thread ${threadId}`,
	);

	return {
		get threadId() {
			return threadId;
		},
		get thread() {
			return session.threads.list.find((t) => t.id === threadId) ?? null;
		},
		get messages() {
			return conversation.messages;
		},
		get historyReplayVersion() {
			return conversation.historyReplayVersion;
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
		clearComposerDraft: clearStoredComposerDraft,
		submit: conversation.submit,
		cancel: conversation.cancel,
		load: loadConversation,
		refresh: refreshConversation,
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
