import { generateId } from "ai";
import { SvelteMap } from "svelte/reactivity";

import {
	toRecentSessionSummaries,
	toSessionSummaries,
} from "$lib/app/app-helpers";
import type { AppSessions } from "$lib/app/app-context.types";
import {
	createBackgroundRefresh,
	createCoalescedReload,
} from "$lib/context/create-coalesced-reload";
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

	const list = $derived.by(() => toSessionSummaries(store.list));
	const recent = $derived.by(() => toRecentSessionSummaries(store.list));
	const selected = $derived.by(
		() =>
			list.find((session) => session.id === currentSelectedSessionId) ?? null,
	);

	const sessionContexts = new SvelteMap<string, SessionContextValue>();
	const reloadSessionById = new SvelteMap<string, () => Promise<void>>();

	const removeFromMemory = (sessionId: string): boolean => {
		sessionContexts.get(sessionId)?.dispose();
		sessionContexts.delete(sessionId);
		reloadSessionById.delete(sessionId);

		if (sessionId === currentSelectedSessionId) {
			currentSelectedSessionId = null;
		}

		if (sessionId === pendingSessionId) {
			pendingSessionId = generateId();
		}

		if (!store.list.some((session) => session.id === sessionId)) {
			return false;
		}

		store.evict(sessionId);
		return true;
	};

	const runRefresh = createCoalescedReload(async () => {
		await store.fetch();
		for (const session of store.list) {
			void sessionContexts.get(session.id)?.refresh();
		}
	});
	const refresh = createBackgroundRefresh(
		runRefresh,
		"[AppSessions] Failed to refresh sessions",
	);

	const reloadSession = async (sessionId: string) => {
		let reload = reloadSessionById.get(sessionId);
		if (!reload) {
			reload = createCoalescedReload(async () => {
				await store.fetchOne(sessionId);
				void sessionContexts.get(sessionId)?.refresh();
			});
			reloadSessionById.set(sessionId, reload);
		}
		await reload();
	};

	return {
		get sessions() {
			return store.list;
		},
		get list() {
			return list;
		},
		get recent() {
			return recent;
		},
		get selectedId() {
			return currentSelectedSessionId;
		},
		get pendingId() {
			return pendingSessionId;
		},
		get selected() {
			return selected;
		},
		sessionContexts,
		select: (sessionId) => {
			currentSelectedSessionId = sessionId;
			// Trigger a background fetchOne if this session isn't cached yet (SWR)
			store.get(sessionId);
		},
		startNew: () => {
			pendingSessionId = generateId();
			currentSelectedSessionId = null;
		},
		refresh,
		reloadSession,
		create: async (workspaceId) => {
			const session = await store.create(
				workspaceId ? { workspaceId } : undefined,
			);
			currentSelectedSessionId = session.id;
			void refresh();
			return session.id;
		},
		rename: async (sessionId, nextName) => {
			const trimmedName = nextName.trim();
			if (!trimmedName || !list.some((session) => session.id === sessionId)) {
				return false;
			}
			await store.update(sessionId, { displayName: trimmedName });
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
			reloadSessionById.delete(sessionId);
			if (sessionId === currentSelectedSessionId) {
				currentSelectedSessionId = null;
			}
			return true;
		},
		removeFromMemory,
	};
}
