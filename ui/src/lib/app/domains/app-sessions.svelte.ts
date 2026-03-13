import { generateId } from "ai";
import { createMutation, createQuery, queryOptions } from "@tanstack/svelte-query";
import type { QueryClient } from "@tanstack/svelte-query";
import { SvelteMap } from "svelte/reactivity";

import { api } from "$lib/api-client";
import { toSessionSummaries } from "$lib/app/app-helpers";
import type { AppSessions } from "$lib/app/app-context.types";
import {
	getNextSelectedSessionId,
	getReconciledSelectedSessionId,
} from "$lib/app/domains/app-sessions.helpers";
import { appQueryKeys } from "$lib/app/query/app-query-keys";
import type { Session } from "$lib/api-types";
import type { SessionContextValue } from "$lib/session/session-context.types";

type CreateAppSessionsDomainArgs = {
	queryClient: QueryClient;
	initialSelectedSessionId?: string;
};

function sessionsQueryOptions() {
	return queryOptions({
		queryKey: appQueryKeys.sessions(),
		queryFn: async (): Promise<Session[]> => {
			const { sessions } = await api.getSessions();
			return sessions;
		},
		initialData: [],
	});
}

export function createAppSessionsDomain(args: CreateAppSessionsDomainArgs): AppSessions {
	let currentSelectedSessionId = $state<string | null>(args.initialSelectedSessionId ?? null);
	let pendingSessionId = $state<string>(generateId());

	const sessionsQuery = createQuery(() => sessionsQueryOptions());

	const sessions = $derived.by(() => sessionsQuery.data ?? []);
	const list = $derived.by(() => toSessionSummaries(sessionsQuery.data ?? []));
	const recent = $derived.by(() => list.filter((session) => session.isRecent));
	const selected = $derived.by(
		() => list.find((session) => session.id === currentSelectedSessionId) ?? null,
	);

	const sessionContexts = new SvelteMap<string, SessionContextValue>();

	$effect(() => {
		const next = getReconciledSelectedSessionId(list, currentSelectedSessionId);
		if (next === null && currentSelectedSessionId !== null) {
			pendingSessionId = generateId();
		}
		currentSelectedSessionId = next;
	});

	const renameMutation = createMutation(() => ({
		mutationFn: async ({
			sessionId,
			nextName,
		}: {
			sessionId: string;
			nextName: string;
		}) => api.updateSession(sessionId, { displayName: nextName }),
		onSuccess: (updatedSession) => {
			args.queryClient.setQueryData<Session[]>(appQueryKeys.sessions(), (previous) =>
				(previous ?? []).map((session) =>
					session.id === updatedSession.id ? updatedSession : session,
				),
			);
		},
	}));

	const deleteMutation = createMutation(() => ({
		mutationFn: async (sessionId: string) => {
			await api.deleteSession(sessionId);
			return sessionId;
		},
		onSuccess: (sessionId) => {
			args.queryClient.setQueryData<Session[]>(appQueryKeys.sessions(), (previous) =>
				(previous ?? []).filter((session) => session.id !== sessionId),
			);
			const next = getReconciledSelectedSessionId(
				list,
				getNextSelectedSessionId(list, sessionId, currentSelectedSessionId),
			);
			if (next === null && currentSelectedSessionId !== null) {
				pendingSessionId = generateId();
			}
			currentSelectedSessionId = next;
		},
	}));

	const createMutationResult = createMutation(() => ({
		mutationFn: async (workspaceId?: string) => {
			return api.createSession({
				id: generateId(),
				...(workspaceId ? { workspaceId } : {}),
			});
		},
		onSuccess: async (created) => {
			currentSelectedSessionId = created.id;
			await args.queryClient.invalidateQueries({ queryKey: appQueryKeys.sessions() });
		},
	}));

	return {
		get sessions() {
			return sessions;
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
		select: (sessionId: string) => {
			const found = sessions.find((s) => s.id === sessionId);
			if (!found) {
				throw new Error(`Session "${sessionId}" not found`);
			}
			currentSelectedSessionId = sessionId;
		},
		startNew: () => {
			pendingSessionId = generateId();
			currentSelectedSessionId = null;
		},
		refresh: async () => {
			await sessionsQuery.refetch();
		},
		create: async (workspaceId) => {
			const created = await createMutationResult.mutateAsync(workspaceId);
			return created.id;
		},
		rename: async (sessionId, nextName) => {
			const trimmedName = nextName.trim();
			if (!trimmedName || !list.some((session) => session.id === sessionId)) {
				return false;
			}

			await renameMutation.mutateAsync({ sessionId, nextName: trimmedName });
			return true;
		},
		remove: async (sessionId) => {
			if (!list.some((session) => session.id === sessionId)) {
				return false;
			}

			await deleteMutation.mutateAsync(sessionId);
			return true;
		},
	};
}
