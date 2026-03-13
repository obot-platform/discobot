import { getContext, hasContext, setContext } from "svelte";

import { appQueryKeys } from "$lib/app/query/app-query-keys";
import { useSessionContext } from "$lib/context/session-context.svelte";
import { createSessionConversationDomain } from "$lib/session/domains/session-conversation.svelte";
import { getPlanEntries } from "$lib/session/domains/session-domain.helpers";
import type { SessionContextValue, ThreadContextValue } from "$lib/session/session-context.types";

const THREAD_CONTEXT_KEY = Symbol.for("discobot-ui-thread-context");

function createThreadContext(
	threadId: string,
	session: SessionContextValue,
): ThreadContextValue {
	const conversation = createSessionConversationDomain({
		queryClient: session.queryClient,
		getSession: () => session.current,
		threadId,
		key: (domain, ...parts) => session.cache.key(domain, ...parts),
		updateSession: session.updateCurrent,
		afterTurn: async () => {
			await Promise.all([
				session.files.refresh(),
				session.services.refresh(),
				session.envSets.refresh(),
				session.hooks.refresh(),
			]);
			await session.queryClient.invalidateQueries({
				queryKey: appQueryKeys.sessions(),
			});
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
		refresh: conversation.refresh,
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
