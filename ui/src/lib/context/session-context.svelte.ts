import { createQuery, queryOptions } from "@tanstack/svelte-query";
import { getContext, hasContext, setContext } from "svelte";

import { api } from "$lib/api-client";
import { appQueryKeys } from "$lib/app/query/app-query-keys";
import { useAppContext } from "$lib/context/app-context.svelte";
import { getQueryClient } from "$lib/query/query-client";
import { createSessionQueryCache } from "$lib/session/cache/query-cache.svelte";
import { createSessionConversationDomain } from "$lib/session/domains/session-conversation.svelte";
import { createSessionEnvSetsDomain } from "$lib/session/domains/session-env-sets.svelte";
import { createSessionFilesDomain } from "$lib/session/domains/session-files.svelte";
import { createSessionHooksDomain } from "$lib/session/domains/session-hooks.svelte";
import { createSessionServicesDomain } from "$lib/session/domains/session-services.svelte";
import { createSessionThreadsDomain } from "$lib/session/domains/session-threads.svelte";
import type {
	SessionContextBootstrap,
	SessionContextValue,
} from "$lib/session/session-context.types";
import { createSessionViewState } from "$lib/session/view/create-session-view-state.svelte";
import type { Session } from "$lib/api-types";

const SESSION_CONTEXT_KEY = Symbol.for("discobot-ui-session-context");

function createSessionContext(_bootstrap?: SessionContextBootstrap): SessionContextValue {
	const app = useAppContext();
	const queryClient = getQueryClient();
	const sessionsQuery = createQuery(() =>
		queryOptions({
			queryKey: appQueryKeys.sessions(),
			queryFn: async () => {
				const { sessions } = await api.getSessions();
				return sessions;
			},
			initialData: () => queryClient.getQueryData<Session[]>(appQueryKeys.sessions()) ?? [],
		}),
	);

	const current = $derived.by(() => {
		const selectedSessionId = app.ui.selectedSessionId;
		if (!selectedSessionId) {
			return null;
		}
		return (sessionsQuery.data ?? []).find((session) => session.id === selectedSessionId) ?? null;
	});

	const ui = createSessionViewState({
		getFiles: () => filesDomain?.list ?? [],
	});

	let cache = $state(createSessionQueryCache(queryClient, current?.id ?? "session"));
	let previousSessionId = $state<string | null>(current?.id ?? null);

	const filesDomain = createSessionFilesDomain({
		queryClient,
		getSession: () => current,
		key: (domain, ...parts) => cache.key(domain, ...parts),
		getSelectedFile: () => ui.selectedFile,
		openFile: ui.openFile,
	});

	function updateSession(updater: (session: Session) => Session) {
		const session = current;
		if (!session) {
			return;
		}
		queryClient.setQueryData<Session[]>(appQueryKeys.sessions(), (previous) =>
			(previous ?? []).map((candidate) =>
				candidate.id === session.id ? updater(candidate) : candidate,
			),
		);
	}

	const threads = createSessionThreadsDomain({
		queryClient,
		getSession: () => current,
		key: (domain, ...parts) => cache.key(domain, ...parts),
		getSelectedId: () => ui.selectedThreadId,
		setSelectedId: (threadId) => {
			ui.selectThread(threadId);
		},
	});

	const envSets = createSessionEnvSetsDomain({
		queryClient,
		getSession: () => current,
		key: (domain, ...parts) => cache.key(domain, ...parts),
		updateSession,
	});

	const hooks = createSessionHooksDomain({
		queryClient,
		getSession: () => current,
		key: (domain, ...parts) => cache.key(domain, ...parts),
	});

	const services = createSessionServicesDomain({
		queryClient,
		getSession: () => current,
		key: (domain, ...parts) => cache.key(domain, ...parts),
		getActiveServiceId: () => ui.activeServiceId,
		openService: ui.openService,
	});

	const conversation = createSessionConversationDomain({
		queryClient,
		getSession: () => current,
		getThreadId: () => threads.selectedId,
		key: (domain, ...parts) => cache.key(domain, ...parts),
		updateSession,
		afterTurn: async () => {
			await Promise.all([
				filesDomain.refresh(),
				services.refresh(),
				envSets.refresh(),
				hooks.refresh(),
			]);
			await queryClient.invalidateQueries({ queryKey: appQueryKeys.sessions() });
		},
	});

	$effect(() => {
		const nextSessionId = current?.id ?? null;
		if (nextSessionId === previousSessionId) {
			return;
		}

		if (previousSessionId) {
			const previousCache = createSessionQueryCache(queryClient, previousSessionId);
			void previousCache.cancelAll();
			previousCache.removeAll();
		}

		cache = createSessionQueryCache(queryClient, nextSessionId ?? "session");
		previousSessionId = nextSessionId;
		ui.resetForSession(null, "");
	});

	$effect(() => {
		if (ui.activeView.kind !== "file") {
			return;
		}
		void filesDomain.open(ui.activeView.path);
	});

	return {
		get sessionId() {
			return current?.id ?? null;
		},
		get current() {
			return current;
		},
		queryClient,
		get cache() {
			return cache;
		},
		ui,
		threads,
		envSets,
		hooks,
		files: filesDomain,
		services,
		conversation,
		dispose: () => {
			conversation.dispose();
			void cache.cancelAll();
			cache.removeAll();
		},
	};
}

export function setSessionContext(bootstrap?: SessionContextBootstrap): SessionContextValue {
	const context = createSessionContext(bootstrap);
	setContext(SESSION_CONTEXT_KEY, context);
	return context;
}

export function useSessionContext(): SessionContextValue {
	const context = getContext<SessionContextValue | undefined>(SESSION_CONTEXT_KEY);
	if (!context) {
		throw new Error("useSessionContext must be used within SessionContext provider");
	}
	return context;
}

export function getSessionContextIfPresent(): SessionContextValue | undefined {
	if (!hasContext(SESSION_CONTEXT_KEY)) {
		return undefined;
	}
	return getContext<SessionContextValue | undefined>(SESSION_CONTEXT_KEY);
}
