import { SvelteDate } from "svelte/reactivity";

import { getQueryClient } from "$lib/query/query-client";
import { createSessionQueryCache } from "$lib/session/cache/query-cache.svelte";
import {
	createSessionEnvSetsModule,
	createThreadEnvSetsModule,
} from "$lib/session/runtime/modules/env-sets-module.svelte";
import { createSessionHooksModule } from "$lib/session/runtime/modules/hooks-module.svelte";
import { createSessionThreadsModule } from "$lib/session/runtime/modules/threads-module.svelte";
import { createSessionUiModule } from "$lib/session/runtime/modules/ui-module.svelte";
import type { CenterPanel, SessionData } from "$lib/shell-types";
import type {
	SessionRuntimeBootstrap,
	SessionRuntimeBundle,
} from "$lib/session/runtime/session-runtime.types";

function makeThreadId() {
	return `thread-${Date.now()}-${Math.floor(Math.random() * 10_000)}`;
}

function makeEnvSetId() {
	return `env-${Date.now()}-${Math.floor(Math.random() * 10_000)}`;
}

function nowIsoString() {
	return new SvelteDate().toISOString();
}

export function createSessionRuntime(bootstrap: SessionRuntimeBootstrap): SessionRuntimeBundle {
	let sessionId = $state<string | null>(null);
	let centerPanel = $state<CenterPanel>("chat");
	let selectedFile = $state("");
	let selectedThreadId = $state<string | null>(null);
	let ideMenuOpen = $state(false);
	let composerDraft = $state(
		"Ask Discobot to refine the shell, inspect files, open a docked service, or compare the new Svelte UI with the current app...",
	);

	let sessionDataById = $state<Record<string, SessionData>>(bootstrap.sessionDataById);
	let envSetList = $state(bootstrap.envSets);

	const current = $derived.by(() => (sessionId ? sessionDataById[sessionId] ?? null : null));
	const threads = $derived.by(() => current?.threads ?? []);
	const selectedThread = $derived.by(
		() => threads.find((thread) => thread.id === selectedThreadId) ?? null,
	);
	const conversation = $derived.by(() => current?.conversation ?? []);
	const planEntries = $derived.by(() => current?.planEntries ?? []);
	const hooksStatus = $derived.by(() =>
		current?.hooksStatus ?? {
			hooks: [],
			pendingHookIds: [],
		},
	);
	const hookOutputById = $derived.by(() => current?.hookOutputById ?? {});
	const files = $derived.by(() => current?.editorFiles ?? []);
	const threadFileContents = $derived.by(() => current?.fileContents ?? {});
	const services = $derived.by(() => current?.services ?? []);
	const suggestedPrompts = $derived.by(() => current?.suggestedPrompts ?? []);
	const activeEnvSetIds = $derived.by(() => current?.activeEnvSetIds ?? []);
	const activeService = $derived.by(
		() => services.find((item) => centerPanel === `service:${item.id}`) ?? null,
	);

	const queryClient = getQueryClient();
	let cache = createSessionQueryCache(queryClient, bootstrap.selectedSessionId ?? "session");

	const loadSession = (nextSessionId: string) => {
		const target = sessionDataById[nextSessionId];
		if (!target) {
			return;
		}

		sessionId = nextSessionId;
		centerPanel = "chat";
		ideMenuOpen = false;
		selectedFile = target.editorFiles[0] ?? "";
		selectedThreadId = target.threads[0]?.id ?? null;

		const previousCache = cache;
		void previousCache.cancelAll();
		previousCache.removeAll();
		cache = createSessionQueryCache(queryClient, nextSessionId);
	};

	if (bootstrap.selectedSessionId) {
		loadSession(bootstrap.selectedSessionId);
	}

	const uiModule = createSessionUiModule({
		getCenterPanel: () => centerPanel,
		setCenterPanel: (value) => {
			centerPanel = value;
		},
		getSelectedFile: () => selectedFile,
		setSelectedFileState: (value) => {
			selectedFile = value;
		},
		getFiles: () => files,
		getIdeMenuOpen: () => ideMenuOpen,
		setIdeMenuOpen: (value) => {
			ideMenuOpen = value;
		},
		getComposerDraft: () => composerDraft,
		setComposerDraftState: (value) => {
			composerDraft = value;
		},
	});

	const threadsModule = createSessionThreadsModule({
		getSessionId: () => sessionId,
		getSessionDataById: () => sessionDataById,
		setSessionDataById: (value) => {
			sessionDataById = value;
		},
		getList: () => threads,
		getSelectedId: () => selectedThreadId,
		setSelectedId: (value) => {
			selectedThreadId = value;
		},
		createThreadId: makeThreadId,
	});

	const sessionEnvSetsModule = createSessionEnvSetsModule({
		getSessionDataById: () => sessionDataById,
		setSessionDataById: (value) => {
			sessionDataById = value;
		},
		getList: () => envSetList,
		setList: (value) => {
			envSetList = value;
		},
		createEnvSetId: makeEnvSetId,
		nowIsoString,
	});

	const threadEnvSetsModule = createThreadEnvSetsModule({
		getSessionId: () => sessionId,
		getSessionDataById: () => sessionDataById,
		setSessionDataById: (value) => {
			sessionDataById = value;
		},
		getList: () => envSetList,
	});

	const hooksModule = createSessionHooksModule({
		getSessionId: () => sessionId,
		getSessionDataById: () => sessionDataById,
		setSessionDataById: (value) => {
			sessionDataById = value;
		},
		getStatus: () => hooksStatus,
		getOutputById: () => hookOutputById,
		nowIsoString,
	});

	return {
		session: {
			get sessionId() {
				return sessionId;
			},
			get current() {
				return current;
			},
			get files() {
				return files;
			},
			get fileContents() {
				return threadFileContents;
			},
			get services() {
				return services;
			},
			get suggestedPrompts() {
				return suggestedPrompts;
			},
			get activeService() {
				return activeService;
			},
			queryClient,
			get cache() {
				return cache;
			},
			threads: threadsModule,
			envSets: sessionEnvSetsModule,
			hooks: hooksModule,
			dispose: () => {
				void cache.cancelAll();
				cache.removeAll();
			},
		},
		thread: {
			get threadId() {
				return selectedThreadId;
			},
			get thread() {
				return selectedThread;
			},
			get conversation() {
				return conversation;
			},
			get planEntries() {
				return planEntries;
			},
			get editorFiles() {
				return files;
			},
			get fileContents() {
				return threadFileContents;
			},
			get activeEnvSetIds() {
				return activeEnvSetIds;
			},
			ui: uiModule,
			envSets: threadEnvSetsModule,
		},
	};
}
