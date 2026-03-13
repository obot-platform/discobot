import { getContext, setContext } from "svelte";
import { SvelteMap } from "svelte/reactivity";

import { useAppContext } from "$lib/context/app-context.svelte";
import { appQueryKeys } from "$lib/app/query/app-query-keys";
import { getQueryClient } from "$lib/query/query-client";
import { createSessionQueryCache } from "$lib/session/cache/query-cache.svelte";
import { createSessionEnvSetsDomain } from "$lib/session/domains/session-env-sets.svelte";
import { createSessionFilesDomain } from "$lib/session/domains/session-files.svelte";
import { createSessionHooksDomain } from "$lib/session/domains/session-hooks.svelte";
import { createSessionServicesDomain } from "$lib/session/domains/session-services.svelte";
import { createSessionThreadsDomain } from "$lib/session/domains/session-threads.svelte";
import type { SessionContextValue, ThreadContextValue } from "$lib/session/session-context.types";
import { createSessionViewState } from "$lib/session/view/create-session-view-state.svelte";
import type { Session } from "$lib/api-types";

const SESSION_CONTEXT_KEY = Symbol.for("discobot-ui-session-context");

function createSessionContext(sessionId: string): SessionContextValue {
	const app = useAppContext();
	const queryClient = getQueryClient();

	const current = $derived.by(() => {
		return app.sessions.sessions.find((s) => s.id === sessionId) ?? null;
	});

	const isPending = $derived.by(() => current === null);

	const cache = createSessionQueryCache(queryClient, sessionId);

	function updateCurrent(updater: (session: Session) => Session) {
		queryClient.setQueryData<Session[]>(appQueryKeys.sessions(), (previous) =>
			(previous ?? []).map((candidate) =>
				candidate.id === sessionId ? updater(candidate) : candidate,
			),
		);
	}

	const ui = createSessionViewState({
		getFiles: () => filesDomain.list,
	});

	const filesDomain = createSessionFilesDomain({
		queryClient,
		getSession: () => current,
		key: (domain, ...parts) => cache.key(domain, ...parts),
		getSelectedFile: () => ui.selectedFile,
		openFile: ui.openFile,
	});

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
		updateSession: updateCurrent,
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

	const threadContexts = new SvelteMap<string, ThreadContextValue>();

	$effect(() => {
		if (ui.activeView.kind !== "file") {
			return;
		}
		void filesDomain.open(ui.activeView.path);
	});

	return {
		get sessionId() {
			return sessionId;
		},
		get isPending() {
			return isPending;
		},
		get current() {
			return current;
		},
		queryClient,
		cache,
		ui,
		threads,
		envSets,
		hooks,
		files: filesDomain,
		services,
		threadContexts,
		updateCurrent,
		dispose: () => {
			void cache.cancelAll();
			cache.removeAll();
		},
	};
}

export function setSessionContext(): SessionContextValue {
	const app = useAppContext();
	const sessionId = app.sessions.selectedId ?? app.sessions.pendingId;

	let context = app.sessions.sessionContexts.get(sessionId);
	if (!context) {
		context = createSessionContext(sessionId);
		app.sessions.sessionContexts.set(sessionId, context);
	}
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
	return getContext<SessionContextValue | undefined>(SESSION_CONTEXT_KEY);
}
