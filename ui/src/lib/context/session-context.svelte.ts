import { getContext, setContext } from "svelte";
import { SvelteMap } from "svelte/reactivity";

import { useAppContext } from "$lib/context/app-context.svelte";
import { createSessionEnvSetsDomain } from "$lib/session/domains/session-env-sets.svelte";
import { createSessionFilesDomain } from "$lib/session/domains/session-files.svelte";
import { createSessionHooksDomain } from "$lib/session/domains/session-hooks.svelte";
import { createSessionServicesDomain } from "$lib/session/domains/session-services.svelte";
import { createSessionThreadsDomain } from "$lib/session/domains/session-threads.svelte";
import type {
	SessionContextValue,
	SessionStores,
	ThreadContextValue,
} from "$lib/session/session-context.types";
import { DESKTOP_SERVICE_ID } from "$lib/shell-types";
import { createSessionViewState } from "$lib/session/view/create-session-view-state.svelte";
import { EnvSetStore } from "$lib/store/env-sets.store.svelte";
import { ThreadStore } from "$lib/store/threads.store.svelte";

const SESSION_CONTEXT_KEY = Symbol.for("discobot-ui-session-context");

function createSessionContext(sessionId: string): SessionContextValue {
	const app = useAppContext();
	let loaded = $state(false);
	const initialSelectedThreadId = app.sessions.takeRequestedThreadId(sessionId);

	const current = $derived.by(() => {
		return app.sessions.sessions.find((s) => s.id === sessionId) ?? null;
	});

	const hasSession = $derived.by(() => current !== null);
	const isPending = $derived.by(() => !hasSession);
	const getSessionName = () =>
		current?.displayName || current?.name || "New Session";

	const ui = createSessionViewState({
		getFiles: () => filesDomain.list,
		getServices: () =>
			services.list
				.filter((service) => service.id !== DESKTOP_SERVICE_ID)
				.map((service) => service.id),
		initialSelectedThreadId,
	});

	const stores: SessionStores = {
		threads: new ThreadStore(),
		envSets: new EnvSetStore(),
	};

	const filesDomain = createSessionFilesDomain({
		sessionId,
		hasSession: () => hasSession,
		getSelectedFile: () => ui.selectedFile,
		openFile: ui.openFile,
	});

	const threads = createSessionThreadsDomain({
		store: stores.threads,
		sessionId,
		hasSession: () => hasSession,
		getSession: () => current,
		getSelectedId: () => ui.selectedThreadId,
		setSelectedId: (threadId) => {
			ui.selectThread(threadId);
		},
		onThreadActivated: (thread) => {
			app.sessions.recordRecentThread({
				sessionId,
				sessionName: getSessionName(),
				threadId: thread.id,
				threadName: thread.name,
				state: thread.state,
				lastMessage: thread.lastMessage || "",
			});
		},
		onThreadRenamed: (thread) => {
			app.sessions.refreshRecentThread({
				sessionId,
				sessionName: getSessionName(),
				threadId: thread.id,
				threadName: thread.name,
				state: thread.state,
				lastMessage: thread.lastMessage || "",
			});
		},
		onThreadUpdated: (thread) => {
			app.sessions.refreshRecentThread({
				sessionId,
				sessionName: getSessionName(),
				threadId: thread.id,
				threadName: thread.name,
				state: thread.state,
				lastMessage: thread.lastMessage || "",
			});
		},
		onThreadRemoved: (threadId) => {
			app.sessions.removeRecentThread(sessionId, threadId);
		},
		onThreadListChanged: (threadIds) => {
			app.sessions.reconcileRecentThreadsForSession(sessionId, threadIds);
		},
	});

	const envSets = createSessionEnvSetsDomain({
		store: stores.envSets,
		sessionId,
		hasSession: () => hasSession,
		getSession: () => current,
		reloadSession: () => app.sessions.reloadSession(sessionId),
	});

	const hooks = createSessionHooksDomain({
		sessionId,
		hasSession: () => hasSession,
	});

	const services = createSessionServicesDomain({
		sessionId,
		hasSession: () => hasSession,
		getActiveServiceId: () => ui.activeServiceId,
		openService: ui.openService,
	});

	const threadContexts = new SvelteMap<string, ThreadContextValue>();

	async function load() {
		if (!hasSession) {
			loaded = false;
			return;
		}
		if (!loaded) {
			await threads.load();
			await Promise.all([
				filesDomain.refresh(),
				services.refresh(),
				envSets.refresh(),
				hooks.refresh(),
			]);
			loaded = true;
		}

		const activeThreadId = threads.selectedId ?? sessionId;
		await threadContexts.get(activeThreadId)?.load();
	}

	function dispose() {
		filesDomain.dispose();
		for (const context of threadContexts.values()) {
			context.dispose();
		}
		threadContexts.clear();
	}

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
		load,
		dispose,
		stores,
		ui,
		threads,
		envSets,
		hooks,
		files: filesDomain,
		services,
		threadContexts,
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
	const context = getContext<SessionContextValue | undefined>(
		SESSION_CONTEXT_KEY,
	);
	if (!context) {
		throw new Error(
			"useSessionContext must be used within SessionContext provider",
		);
	}
	return context;
}

export function getSessionContextIfPresent(): SessionContextValue | undefined {
	return getContext<SessionContextValue | undefined>(SESSION_CONTEXT_KEY);
}
