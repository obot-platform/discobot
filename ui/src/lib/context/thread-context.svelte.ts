import { getContext, hasContext, setContext } from "svelte";

import { useAppContext } from "$lib/context/app-context.svelte";
import { useSessionContext } from "$lib/context/session-context.svelte";
import { createConversationDomain } from "$lib/thread/conversation.svelte";
import { getPlanEntries } from "$lib/session/domains/session-domain.helpers";
import type { SessionContextValue, ThreadContextValue } from "$lib/session/session-context.types";

const THREAD_CONTEXT_KEY = Symbol.for("discobot-ui-thread-context");

function createThreadContext(
	threadId: string,
	session: SessionContextValue,
): ThreadContextValue {
	const app = useAppContext();
	const hasSession = $derived.by(() => session.current !== null);
	const sessionStatus = $derived.by(() => session.current?.status ?? null);

	const conversation = createConversationDomain({
		sessionId: session.sessionId,
		hasSession: () => hasSession,
		getSessionStatus: () => sessionStatus,
		threadId,
		refreshThread: async () => {
			await session.threads.refreshThread(threadId);
		},
		afterTurn: async () => {
			await session.threads.refreshThread(threadId);
			await Promise.all([
				session.files.refresh(),
				session.services.refresh(),
				session.envSets.refresh(),
				session.hooks.refresh(),
			]);
			await app.sessions.reloadSession(session.sessionId);
		},
	});

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
		get planEntries() {
			return getPlanEntries(conversation.messages);
		},
		get status() {
			return conversation.status;
		},
		get error() {
			return conversation.error;
		},
		submit: conversation.submit,
		cancel: conversation.cancel,
		load: conversation.load,
		refresh: conversation.refresh,
		addToolApprovalResponse: conversation.addToolApprovalResponse,
		dispose: conversation.dispose,
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
	const context = getContext<ThreadContextValue | undefined>(THREAD_CONTEXT_KEY);
	if (!context) {
		throw new Error("useThreadContext must be used within a ThreadContext provider");
	}
	return context;
}

export function getThreadContextIfPresent(): ThreadContextValue | undefined {
	if (!hasContext(THREAD_CONTEXT_KEY)) {
		return undefined;
	}
	return getContext<ThreadContextValue | undefined>(THREAD_CONTEXT_KEY);
}
