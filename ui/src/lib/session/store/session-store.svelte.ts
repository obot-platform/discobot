import type { QueryClient } from "@tanstack/svelte-query";

import { getQueryClient } from "$lib/query/query-client";
import { createSessionQueryCache } from "$lib/session/cache/query-cache.svelte";
import type { SessionQueryCache } from "$lib/session/cache/query-cache.svelte";
import type { EnvSetWithVars, SessionData } from "$lib/shell-types";
import {
	getActiveEnvSetIds,
	getActiveEnvSets,
	getConversation,
	getHookOutputById,
	getHooksStatus,
	getPlanEntries,
} from "$lib/session/store/session-store.helpers";

type SessionStoreBootstrap = {
	sessionDataById: Record<string, SessionData>;
	envSets: EnvSetWithVars[];
	selectedSessionId?: string;
};

export type SessionStore = {
	sessionId: string | null;
	sessionDataById: Record<string, SessionData>;
	envSets: EnvSetWithVars[];
	current: SessionData | null;
	threads: SessionData["threads"];
	files: string[];
	fileContents: Record<string, string>;
	services: SessionData["services"];
	hooksStatus: NonNullable<SessionData["hooksStatus"]>;
	hookOutputById: Record<string, string>;
	conversation: NonNullable<SessionData["conversation"]>;
	planEntries: NonNullable<SessionData["planEntries"]>;
	activeEnvSetIds: string[];
	activeEnvSets: EnvSetWithVars[];
	queryClient: QueryClient;
	cache: SessionQueryCache;
	setSessionDataById: (value: Record<string, SessionData>) => void;
	setEnvSets: (value: EnvSetWithVars[]) => void;
	loadSession: (nextSessionId: string) => SessionData | null;
	dispose: () => void;
};

export function createSessionStore(bootstrap: SessionStoreBootstrap): SessionStore {
	let sessionId = $state<string | null>(null);
	let sessionDataById = $state<Record<string, SessionData>>(bootstrap.sessionDataById);
	let envSets = $state(bootstrap.envSets);

	const current = $derived.by(() => (sessionId ? sessionDataById[sessionId] ?? null : null));
	const threads = $derived.by(() => current?.threads ?? []);
	const files = $derived.by(() => current?.editorFiles ?? []);
	const fileContents = $derived.by(() => current?.fileContents ?? {});
	const services = $derived.by(() => current?.services ?? []);
	const hooksStatus = $derived.by(() => getHooksStatus(current));
	const hookOutputById = $derived.by(() => getHookOutputById(current));
	const conversation = $derived.by(() => getConversation(current));
	const planEntries = $derived.by(() => getPlanEntries(current));
	const activeEnvSetIds = $derived.by(() => getActiveEnvSetIds(current));
	const activeEnvSets = $derived.by(() => getActiveEnvSets(envSets, activeEnvSetIds));

	const queryClient = getQueryClient();
	let cache = createSessionQueryCache(queryClient, bootstrap.selectedSessionId ?? "session");

	const loadSession = (nextSessionId: string): SessionData | null => {
		const target = sessionDataById[nextSessionId];
		if (!target) {
			return null;
		}

		sessionId = nextSessionId;

		const previousCache = cache;
		void previousCache.cancelAll();
		previousCache.removeAll();
		cache = createSessionQueryCache(queryClient, nextSessionId);

		return target;
	};

	if (bootstrap.selectedSessionId) {
		loadSession(bootstrap.selectedSessionId);
	}

	return {
		get sessionId() {
			return sessionId;
		},
		get sessionDataById() {
			return sessionDataById;
		},
		get envSets() {
			return envSets;
		},
		get current() {
			return current;
		},
		get threads() {
			return threads;
		},
		get files() {
			return files;
		},
		get fileContents() {
			return fileContents;
		},
		get services() {
			return services;
		},
		get hooksStatus() {
			return hooksStatus;
		},
		get hookOutputById() {
			return hookOutputById;
		},
		get conversation() {
			return conversation;
		},
		get planEntries() {
			return planEntries;
		},
		get activeEnvSetIds() {
			return activeEnvSetIds;
		},
		get activeEnvSets() {
			return activeEnvSets;
		},
		queryClient,
		get cache() {
			return cache;
		},
		setSessionDataById: (value) => {
			sessionDataById = value;
		},
		setEnvSets: (value) => {
			envSets = value;
		},
		loadSession,
		dispose: () => {
			void cache.cancelAll();
			cache.removeAll();
		},
	};
}
