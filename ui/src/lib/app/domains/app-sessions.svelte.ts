import { generateId } from "ai";
import { createMutation, createQuery, queryOptions } from "@tanstack/svelte-query";
import type { QueryClient } from "@tanstack/svelte-query";

import { api } from "$lib/api-client";
import { formatErrorMessage, toSessionSummaries } from "$lib/app/app-helpers";
import type { AppSessions } from "$lib/app/app-context.types";
import { getNextSelectedSessionId } from "$lib/app/domains/app-sessions.helpers";
import { appQueryKeys } from "$lib/app/query/app-query-keys";
import type { AppStore } from "$lib/app/store/app-store.svelte";
import type { AppViewState } from "$lib/app/view/create-app-view-state.svelte";
import type { Session } from "$lib/api-types";

type CreateAppSessionsDomainArgs = {
	store: AppStore;
	view: AppViewState;
	queryClient: QueryClient;
	setResolvedWorkspaceId: (workspaceId: string | null) => void;
	resolveAgentId: () => Promise<string | null>;
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
	const sessionsQuery = createQuery(() => sessionsQueryOptions());

	const sessions = $derived.by(() => sessionsQuery.data ?? []);
	const list = $derived.by(() => toSessionSummaries(sessionsQuery.data ?? []));
	const recent = $derived.by(() => list.filter((session) => session.isRecent));
	const selected = $derived.by(
		() => list.find((session) => session.id === args.view.selectedSessionId) ?? null,
	);

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
			args.store.errorMessage = undefined;
		},
		onError: (error) => {
			args.store.errorMessage = formatErrorMessage(error, "Failed to rename session");
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
			args.view.reconcileSelectedSession(
				list,
				getNextSelectedSessionId(list, sessionId, args.view.selectedSessionId),
			);
			args.store.errorMessage = undefined;
		},
		onError: (error) => {
			args.store.errorMessage = formatErrorMessage(error, "Failed to delete session");
		},
	}));

	const createMutationResult = createMutation(() => ({
		mutationFn: async (workspaceId?: string) => {
			const agentId = await args.resolveAgentId();
			if (!agentId) {
				throw new Error("Failed to resolve agent");
			}

			if (workspaceId) {
				args.setResolvedWorkspaceId(workspaceId);
			}

			return api.createSession({
				id: generateId(),
				agentId,
				...(workspaceId ? { workspaceId } : {}),
			});
		},
		onSuccess: async (created) => {
			args.view.selectedSessionId = created.id;
			args.store.errorMessage = undefined;
			await args.queryClient.invalidateQueries({ queryKey: appQueryKeys.sessions() });
		},
		onError: (error) => {
			args.store.errorMessage = formatErrorMessage(error, "Failed to create session");
		},
	}));

	$effect(() => {
		args.view.reconcileSelectedSession(list, args.view.selectedSessionId);
	});

	return {
		get list() {
			return list;
		},
		get recent() {
			return recent;
		},
		get selectedId() {
			return args.view.selectedSessionId;
		},
		get selected() {
			return selected;
		},
		select: args.view.selectSession,
		startNew: args.view.startNewSession,
		refresh: async () => {
			await sessionsQuery.refetch();
			args.store.errorMessage = undefined;
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
