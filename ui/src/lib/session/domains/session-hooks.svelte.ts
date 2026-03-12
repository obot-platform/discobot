import { createMutation, createQuery, queryOptions } from "@tanstack/svelte-query";
import type { QueryClient } from "@tanstack/svelte-query";

import { api } from "$lib/api-client";
import type { Session } from "$lib/api-types";
import { mergeHookOutput, toHooksStatus } from "$lib/session/domains/session-domain.helpers";
import type { SessionHooksService } from "$lib/session/services/hooks-service";

const HOOKS_DOMAIN = "hooks";

type CreateSessionHooksDomainArgs = {
	queryClient: QueryClient;
	getSession: () => Session | null;
	key: (...parts: string[]) => readonly unknown[];
};

function hooksStatusQueryOptions(args: CreateSessionHooksDomainArgs, sessionId: string) {
	return queryOptions({
		queryKey: args.key(HOOKS_DOMAIN, "status"),
		queryFn: async () => api.getHooksStatus(sessionId),
		initialData: null,
	});
}

export function createSessionHooksDomain(
	args: CreateSessionHooksDomainArgs,
): SessionHooksService & { refresh: () => Promise<void> } {
	let outputById = $state<Record<string, string>>({});
	let previousSessionId = $state<string | null>(args.getSession()?.id ?? null);

	const hooksQuery = createQuery(() => {
		const sessionId = args.getSession()?.id;
		return queryOptions({
			queryKey: args.key(HOOKS_DOMAIN, "status"),
			queryFn: async () => {
				if (!sessionId) {
					return null;
				}
				return api.getHooksStatus(sessionId);
			},
			initialData: null,
		});
	});

	const status = $derived.by(() => toHooksStatus(hooksQuery.data));

	$effect(() => {
		const nextSessionId = args.getSession()?.id ?? null;
		if (nextSessionId === previousSessionId) {
			return;
		}
		previousSessionId = nextSessionId;
		outputById = {};
	});

	$effect(() => {
		const sessionId = args.getSession()?.id;
		if (!sessionId || !hooksQuery.data) {
			outputById = {};
			return;
		}

		for (const hookId of Object.keys(hooksQuery.data.hooks)) {
			if (hookId in outputById) {
				continue;
			}

			void api.getHookOutput(sessionId, hookId).then((response) => {
				outputById = mergeHookOutput(outputById, hookId, response);
			});
		}
	});

	const rerunMutation = createMutation(() => ({
		mutationFn: async (hookId: string) => {
			const session = args.getSession();
			if (!session) {
				return null;
			}
			await api.rerunHook(session.id, hookId);
			const [nextStatus, nextOutput] = await Promise.all([
				api.getHooksStatus(session.id),
				api.getHookOutput(session.id, hookId),
			]);
			return { hookId, nextStatus, nextOutput };
		},
		onSuccess: (result) => {
			if (!result) {
				return;
			}
			args.queryClient.setQueryData(args.key(HOOKS_DOMAIN, "status"), result.nextStatus);
			outputById = mergeHookOutput(outputById, result.hookId, result.nextOutput);
		},
	}));

	return {
		get status() {
			return status;
		},
		get outputById() {
			return outputById;
		},
		rerun: (hookId: string) => {
			void rerunMutation.mutateAsync(hookId);
		},
		refresh: async () => {
			const sessionId = args.getSession()?.id;
			if (!sessionId) {
				return;
			}
			const nextStatus = await args.queryClient.fetchQuery(hooksStatusQueryOptions(args, sessionId));
			if (!nextStatus) {
				outputById = {};
				return;
			}
			outputById = {};
			for (const hookId of Object.keys(nextStatus.hooks)) {
				const response = await api.getHookOutput(sessionId, hookId);
				outputById = mergeHookOutput(outputById, hookId, response);
			}
		},
	};
}
