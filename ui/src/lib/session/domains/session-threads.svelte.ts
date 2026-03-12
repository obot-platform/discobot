import { generateId } from "ai";
import { createMutation, createQuery, queryOptions } from "@tanstack/svelte-query";
import type { QueryClient } from "@tanstack/svelte-query";

import { api } from "$lib/api-client";
import type { Session } from "$lib/api-types";
import {
	buildImplicitThread,
	getNextSelectedThreadId,
} from "$lib/session/domains/session-domain.helpers";
import type { SessionThreadsService } from "$lib/session/services/threads-service";

const THREADS_DOMAIN = "threads";

type CreateSessionThreadsDomainArgs = {
	queryClient: QueryClient;
	getSession: () => Session | null;
	key: (...parts: string[]) => readonly unknown[];
	getSelectedId: () => string | null;
	setSelectedId: (threadId: string | null) => void;
};

function threadsQueryOptions(args: CreateSessionThreadsDomainArgs, sessionId: string) {
	return queryOptions({
		queryKey: args.key(THREADS_DOMAIN),
		queryFn: async () => {
			const { threads } = await api.getThreads(sessionId);
			return threads;
		},
		initialData: [],
	});
}

export function createSessionThreadsDomain(
	args: CreateSessionThreadsDomainArgs,
): SessionThreadsService & { refresh: () => Promise<void> } {
	const threadsQuery = createQuery(() => {
		const sessionId = args.getSession()?.id;
		return queryOptions({
			queryKey: args.key(THREADS_DOMAIN),
			queryFn: async () => {
				if (!sessionId) {
					return [];
				}
				const { threads } = await api.getThreads(sessionId);
				return threads;
			},
			initialData: [],
		});
	});

	const rawList = $derived.by(() =>
		(threadsQuery.data ?? []).map((thread) => ({ id: thread.id, name: thread.name })),
	);
	const list = $derived.by(() => {
		const session = args.getSession();
		return rawList.length > 0 ? rawList : buildImplicitThread(session);
	});

	const createThreadMutation = createMutation(() => ({
		mutationFn: async (name?: string) => {
			const session = args.getSession();
			if (!session) {
				return null;
			}

			const trimmedName = name?.trim();
			const threadId = rawList.length === 0 ? session.id : generateId();
			return api.createThread(session.id, {
				id: threadId,
				name: trimmedName && trimmedName.length > 0 ? trimmedName : undefined,
			});
		},
		onSuccess: (created) => {
			if (!created) {
				return;
			}
			args.queryClient.setQueryData(args.key(THREADS_DOMAIN), (previous: Array<{ id: string; name: string }> | undefined) => {
				const next = previous ? [...previous] : [];
				const index = next.findIndex((thread) => thread.id === created.id);
				if (index === -1) {
					next.push(created);
				} else {
					next[index] = created;
				}
				return next;
			});
			args.setSelectedId(created.id);
		},
	}));

	const renameThreadMutation = createMutation(() => ({
		mutationFn: async ({ threadId, nextName }: { threadId: string; nextName: string }) => {
			const session = args.getSession();
			if (!session) {
				return null;
			}

			if (rawList.length === 0 && threadId === session.id) {
				return api.createThread(session.id, { id: threadId, name: nextName });
			}

			return api.updateThread(session.id, threadId, { name: nextName });
		},
		onSuccess: (updated) => {
			if (!updated) {
				return;
			}
			args.queryClient.setQueryData(args.key(THREADS_DOMAIN), (previous: Array<{ id: string; name: string }> | undefined) => {
				const next = previous ? [...previous] : [];
				const index = next.findIndex((thread) => thread.id === updated.id);
				if (index === -1) {
					next.push(updated);
				} else {
					next[index] = updated;
				}
				return next;
			});
		},
	}));

	const removeThreadMutation = createMutation(() => ({
		mutationFn: async (threadId: string) => {
			const session = args.getSession();
			if (!session) {
				return null;
			}
			await api.deleteThread(session.id, threadId);
			return threadId;
		},
		onSuccess: (removedThreadId) => {
			if (!removedThreadId) {
				return;
			}
			args.queryClient.setQueryData(args.key(THREADS_DOMAIN), (previous: Array<{ id: string; name: string }> | undefined) =>
				(previous ?? []).filter((thread) => thread.id !== removedThreadId),
			);
			args.setSelectedId(getNextSelectedThreadId(list, removedThreadId, args.getSelectedId()));
		},
	}));

	$effect(() => {
		if (list.length === 0) {
			args.setSelectedId(null);
			return;
		}

		const selectedId = args.getSelectedId();
		if (selectedId && list.some((thread) => thread.id === selectedId)) {
			return;
		}

		args.setSelectedId(list[0]?.id ?? null);
	});

	return {
		get list() {
			return list;
		},
		get selectedId() {
			return args.getSelectedId();
		},
		get selected() {
			return list.find((thread) => thread.id === args.getSelectedId()) ?? null;
		},
		select: (threadId: string) => {
			if (list.some((thread) => thread.id === threadId)) {
				args.setSelectedId(threadId);
			}
		},
		create: (name?: string) => {
			void createThreadMutation.mutateAsync(name);
		},
		rename: (threadId: string, nextName: string) => {
			const trimmedName = nextName.trim();
			if (!trimmedName || !list.some((thread) => thread.id === threadId)) {
				return;
			}
			void renameThreadMutation.mutateAsync({ threadId, nextName: trimmedName });
		},
		remove: (threadId: string) => {
			const session = args.getSession();
			if (!session) {
				return;
			}
			if (rawList.length === 0 && threadId === session.id) {
				return;
			}
			if (!rawList.some((thread) => thread.id === threadId)) {
				return;
			}
			void removeThreadMutation.mutateAsync(threadId);
		},
		refresh: async () => {
			const sessionId = args.getSession()?.id;
			if (!sessionId) {
				return;
			}
			await args.queryClient.fetchQuery(threadsQueryOptions(args, sessionId));
		},
	};
}
