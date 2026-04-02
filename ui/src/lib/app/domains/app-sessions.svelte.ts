import { generateId } from "ai";
import { SvelteMap } from "svelte/reactivity";

import {
	readRecentThreadEntries,
	reconcileRecentThreadsForSession,
	reconcileRecentThreadsWithSessions,
	refreshRecentSessionName,
	refreshRecentThread,
	removeRecentThread,
	removeRecentThreadsForSession,
	toRecentThreadSummaries,
	toSessionSummaries,
	touchRecentThread,
	writeRecentThreadEntries,
} from "$lib/app/app-helpers";
import type { AppSessions } from "$lib/app/app-context.types";
import type { SessionContextValue } from "$lib/session/session-context.types";
import type { SessionStore } from "$lib/store/sessions.store.svelte";

type CreateAppSessionsDomainArgs = {
	store: SessionStore;
	initialSelectedSessionId?: string;
};

export function createAppSessionsDomain(
	args: CreateAppSessionsDomainArgs,
): AppSessions {
	const { store } = args;
	let currentSelectedSessionId = $state<string | null>(
		args.initialSelectedSessionId ?? null,
	);
	let pendingSessionId = $state<string>(generateId());
	let awaitingInitialStatusId = $state<string | null>(null);
	let recentThreadEntries = $state(readRecentThreadEntries());
	const requestedThreadIdBySession = new SvelteMap<string, string>();

	const persistRecentThreadEntries = (entries: typeof recentThreadEntries) => {
		recentThreadEntries = entries;
		writeRecentThreadEntries(entries);
	};

	const selectSession = (sessionId: string) => {
		currentSelectedSessionId = sessionId;
		store.get(sessionId);
	};

	const recordRecentThread = (payload: {
		sessionId: string;
		sessionName: string;
		threadId: string;
		threadName: string;
		state?: import("$lib/api-types").ThreadState;
		lastMessage: string;
	}) => {
		persistRecentThreadEntries(touchRecentThread(recentThreadEntries, payload));
	};

	const updateRecentThread = (payload: {
		sessionId: string;
		sessionName: string;
		threadId: string;
		threadName: string;
		state?: import("$lib/api-types").ThreadState;
		lastMessage: string;
	}) => {
		const nextEntries = refreshRecentThread(recentThreadEntries, payload);
		if (nextEntries === recentThreadEntries) {
			return;
		}
		persistRecentThreadEntries(nextEntries);
	};

	const dropRecentThread = (sessionId: string, threadId: string) => {
		const nextEntries = removeRecentThread(
			recentThreadEntries,
			sessionId,
			threadId,
		);
		if (nextEntries.length === recentThreadEntries.length) {
			return;
		}
		persistRecentThreadEntries(nextEntries);
	};

	const dropRecentThreadsForSession = (sessionId: string) => {
		const nextEntries = removeRecentThreadsForSession(
			recentThreadEntries,
			sessionId,
		);
		if (nextEntries.length === recentThreadEntries.length) {
			return;
		}
		persistRecentThreadEntries(nextEntries);
	};

	const list = $derived.by(() => toSessionSummaries(store.list));
	const recentThreads = $derived.by(() =>
		toRecentThreadSummaries(list, recentThreadEntries),
	);
	const selected = $derived.by(
		() =>
			list.find((session) => session.id === currentSelectedSessionId) ?? null,
	);

	$effect(() => {
		if (store.status !== "ready") {
			return;
		}

		const nextEntries = reconcileRecentThreadsWithSessions(
			recentThreadEntries,
			store.list.map((session) => session.id),
		);
		if (nextEntries !== recentThreadEntries) {
			persistRecentThreadEntries(nextEntries);
		}
	});

	const sessionContexts = new SvelteMap<string, SessionContextValue>();

	const removeFromMemory = (sessionId: string): boolean => {
		sessionContexts.get(sessionId)?.dispose();
		sessionContexts.delete(sessionId);
		requestedThreadIdBySession.delete(sessionId);
		dropRecentThreadsForSession(sessionId);

		if (sessionId === currentSelectedSessionId) {
			currentSelectedSessionId = null;
		}

		if (sessionId === pendingSessionId) {
			pendingSessionId = generateId();
		}

		if (sessionId === awaitingInitialStatusId) {
			awaitingInitialStatusId = null;
		}

		if (!store.list.some((session) => session.id === sessionId)) {
			return false;
		}

		store.evict(sessionId);
		return true;
	};

	const refresh = async () => {
		await store.fetch();
		for (const session of store.list) {
			void sessionContexts.get(session.id)?.load();
		}
	};

	const reloadSession = async (sessionId: string) => {
		await store.fetchOne(sessionId);
		if (awaitingInitialStatusId === sessionId) {
			const session = store.list.find((item) => item.id === sessionId) ?? null;
			if (session?.status) {
				awaitingInitialStatusId = null;
			}
		}
		void sessionContexts.get(sessionId)?.load();
	};

	return {
		get sessions() {
			return store.list;
		},
		get list() {
			return list;
		},
		get recentThreads() {
			return recentThreads;
		},
		get selectedId() {
			return currentSelectedSessionId;
		},
		get pendingId() {
			return pendingSessionId;
		},
		get awaitingInitialStatusId() {
			return awaitingInitialStatusId;
		},
		get selected() {
			return selected;
		},
		sessionContexts,
		select: selectSession,
		openThread: (sessionId, threadId) => {
			const sessionContext = sessionContexts.get(sessionId);
			if (sessionContext) {
				sessionContext.ui.selectThread(threadId);
				sessionContext.threads.select(threadId);
			} else {
				requestedThreadIdBySession.set(sessionId, threadId);
			}
			selectSession(sessionId);
		},
		startNew: () => {
			pendingSessionId = generateId();
			currentSelectedSessionId = null;
			awaitingInitialStatusId = null;
		},
		setAwaitingInitialStatus: (sessionId) => {
			awaitingInitialStatusId = sessionId;
		},
		refresh,
		reloadSession,
		create: async (workspaceId) => {
			const session = await store.create(
				workspaceId ? { workspaceId } : undefined,
			);
			currentSelectedSessionId = session.id;
			await refresh();
			return session.id;
		},
		rename: async (sessionId, nextName) => {
			const trimmedName = nextName.trim();
			if (!trimmedName || !list.some((session) => session.id === sessionId)) {
				return false;
			}
			const updatedSession = await store.update(sessionId, {
				displayName: trimmedName,
			});
			persistRecentThreadEntries(
				refreshRecentSessionName(
					recentThreadEntries,
					sessionId,
					updatedSession.displayName || updatedSession.name,
				),
			);
			return true;
		},
		remove: async (sessionId) => {
			if (!list.some((session) => session.id === sessionId)) {
				return false;
			}
			await store.remove(sessionId);
			// remove already evicts from the store; clean up context lifecycle
			sessionContexts.get(sessionId)?.dispose();
			sessionContexts.delete(sessionId);
			requestedThreadIdBySession.delete(sessionId);
			dropRecentThreadsForSession(sessionId);
			if (sessionId === currentSelectedSessionId) {
				currentSelectedSessionId = null;
			}
			return true;
		},
		removeFromMemory,
		recordRecentThread,
		refreshRecentThread: updateRecentThread,
		removeRecentThread: dropRecentThread,
		reconcileRecentThreadsForSession: (sessionId, threadIds) => {
			const nextEntries = reconcileRecentThreadsForSession(
				recentThreadEntries,
				sessionId,
				threadIds,
			);
			if (nextEntries !== recentThreadEntries) {
				persistRecentThreadEntries(nextEntries);
			}
		},
		takeRequestedThreadId: (sessionId) => {
			const threadId = requestedThreadIdBySession.get(sessionId) ?? null;
			requestedThreadIdBySession.delete(sessionId);
			return threadId;
		},
	};
}
