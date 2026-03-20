import { generateId } from "ai";

import type { Session } from "$lib/api-types";
import {
	buildImplicitThread,
	getNextSelectedThreadId,
} from "$lib/session/domains/session-domain.helpers";
import type { SessionThreadsService } from "$lib/session/session-context.types";
import type { ThreadStore } from "$lib/store/threads.store.svelte";

type CreateSessionThreadsDomainArgs = {
	store: ThreadStore;
	sessionId: string;
	hasSession: () => boolean;
	getSession: () => Session | null;
	getSelectedId: () => string | null;
	setSelectedId: (threadId: string | null) => void;
};

export function createSessionThreadsDomain(
	args: CreateSessionThreadsDomainArgs,
): SessionThreadsService {
	const { store } = args;

	function currentList() {
		return store.list.length > 0 ? store.list : buildImplicitThread(args.getSession());
	}

	function syncSelectedThread(nextList = currentList()) {
		if (nextList.length === 0) {
			args.setSelectedId(null);
			return;
		}

		const selectedId = args.getSelectedId();
		if (selectedId && nextList.some((thread) => thread.id === selectedId)) {
			return;
		}

		args.setSelectedId(nextList[0]?.id ?? null);
	}

	const list = $derived.by(() => currentList());

	return {
		get list() {
			return list;
		},
		get status() {
			return store.status;
		},
		get selectedId() {
			return args.getSelectedId();
		},
		get selected() {
			return list.find((thread) => thread.id === args.getSelectedId()) ?? null;
		},
		load: async () => {
			if (store.status === "loading" || store.status === "ready") {
				syncSelectedThread();
				return;
			}
			if (!args.hasSession()) {
				store.reset();
				syncSelectedThread([]);
				return;
			}
			try {
				await store.fetch(args.sessionId);
				syncSelectedThread(store.list.length > 0 ? store.list : buildImplicitThread(args.getSession()));
			} catch (error) {
				throw error;
			}
		},
		select: (threadId: string) => {
			if (list.some((thread) => thread.id === threadId)) {
				args.setSelectedId(threadId);
			}
		},
		create: (name?: string) => {
			void (async () => {
				if (!args.hasSession()) {
					return;
				}

				const trimmedName = name?.trim();
				const threadId = store.list.length === 0 ? args.sessionId : generateId();
				const created = await store.create(args.sessionId, {
					id: threadId,
					name: trimmedName && trimmedName.length > 0 ? trimmedName : undefined,
				});
				args.setSelectedId(created.id);
			})();
		},
		rename: (threadId: string, nextName: string) => {
			const trimmedName = nextName.trim();
			if (!trimmedName || !list.some((thread) => thread.id === threadId)) {
				return;
			}
			void (async () => {
				if (!args.hasSession()) {
					return;
				}

				if (store.list.length === 0 && threadId === args.sessionId) {
					await store.create(args.sessionId, { id: threadId, name: trimmedName });
				} else {
					await store.update(args.sessionId, threadId, { name: trimmedName });
				}
			})();
		},
		remove: (threadId: string) => {
			if (!args.hasSession()) {
				return;
			}
			if (store.list.length === 0 && threadId === args.sessionId) {
				return;
			}
			if (!store.list.some((thread) => thread.id === threadId)) {
				return;
			}
			void (async () => {
				await store.remove(args.sessionId, threadId);
				args.setSelectedId(getNextSelectedThreadId(currentList(), threadId, args.getSelectedId()));
				syncSelectedThread();
			})();
		},
		refreshThread: async (threadId: string) => {
			if (!args.hasSession()) {
				return;
			}
			await store.fetchOne(args.sessionId, threadId);
			syncSelectedThread();
		},
	};
}
