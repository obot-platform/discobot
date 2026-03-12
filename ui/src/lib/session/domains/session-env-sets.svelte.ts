import { createMutation, createQuery, queryOptions } from "@tanstack/svelte-query";
import type { QueryClient } from "@tanstack/svelte-query";

import { api } from "$lib/api-client";
import type { Session } from "$lib/api-types";
import { getActiveEnvSets } from "$lib/session/domains/session-domain.helpers";
import type {
	SessionEnvSetsService,
	ThreadEnvSetsService,
} from "$lib/session/services/env-sets-service";

const ENV_SETS_DOMAIN = "env-sets";

type CreateSessionEnvSetsDomainArgs = {
	queryClient: QueryClient;
	getSession: () => Session | null;
	key: (...parts: string[]) => readonly unknown[];
	updateSession: (updater: (session: Session) => Session) => void;
};

function envSetsQueryOptions(args: CreateSessionEnvSetsDomainArgs) {
	return queryOptions({
		queryKey: args.key(ENV_SETS_DOMAIN),
		queryFn: async () => {
			const { envSets } = await api.listEnvSets();
			return Promise.all(envSets.map((envSet) => api.getEnvSet(envSet.id)));
		},
		initialData: [],
	});
}

export function createSessionEnvSetsDomain(
	args: CreateSessionEnvSetsDomainArgs,
): SessionEnvSetsService & ThreadEnvSetsService & { refresh: () => Promise<void> } {
	const envSetsQuery = createQuery(() => envSetsQueryOptions(args));
	const list = $derived.by(() => envSetsQuery.data ?? []);
	const activeIds = $derived.by(() => args.getSession()?.activeEnvSetIds ?? []);
	const active = $derived.by(() => getActiveEnvSets(list, activeIds));

	const createEnvSetMutation = createMutation(() => ({
		mutationFn: async ({ name, envVars }: { name: string; envVars: Record<string, string> }) =>
			api.createEnvSet(name, envVars),
		onSuccess: (created) => {
			args.queryClient.setQueryData(args.key(ENV_SETS_DOMAIN), (previous: typeof list | undefined) => [
				...(previous ?? []),
				created,
			]);
		},
	}));

	const updateEnvSetMutation = createMutation(() => ({
		mutationFn: async ({
			envSetId,
			name,
			envVars,
		}: {
			envSetId: string;
			name: string;
			envVars: Record<string, string>;
		}) => api.updateEnvSet(envSetId, name, envVars),
		onSuccess: (updated) => {
			args.queryClient.setQueryData(args.key(ENV_SETS_DOMAIN), (previous: typeof list | undefined) =>
				(previous ?? []).map((envSet) => (envSet.id === updated.id ? updated : envSet)),
			);
		},
	}));

	const removeEnvSetMutation = createMutation(() => ({
		mutationFn: async (envSetId: string) => {
			await api.deleteEnvSet(envSetId);
			return envSetId;
		},
		onSuccess: (removedId) => {
			args.queryClient.setQueryData(args.key(ENV_SETS_DOMAIN), (previous: typeof list | undefined) =>
				(previous ?? []).filter((envSet) => envSet.id !== removedId),
			);
			const session = args.getSession();
			if (!session || !session.activeEnvSetIds?.includes(removedId)) {
				return;
			}
			args.updateSession((currentSession) => ({
				...currentSession,
				activeEnvSetIds: (currentSession.activeEnvSetIds ?? []).filter((envSetId) => envSetId !== removedId),
			}));
		},
	}));

	const toggleEnvSetMutation = createMutation(() => ({
		mutationFn: async (envSetId: string) => {
			const session = args.getSession();
			if (!session) {
				return null;
			}
			const nextIds = activeIds.includes(envSetId)
				? activeIds.filter((id) => id !== envSetId)
				: [...activeIds, envSetId];
			await api.setSessionActiveEnvSets(session.id, nextIds);
			return nextIds;
		},
		onSuccess: (nextIds) => {
			if (!nextIds) {
				return;
			}
			args.updateSession((currentSession) => ({
				...currentSession,
				activeEnvSetIds: nextIds,
			}));
		},
	}));

	return {
		get list() {
			return list;
		},
		get activeIds() {
			return activeIds;
		},
		get active() {
			return active;
		},
		create: (name: string, envVars: Record<string, string>) => {
			if (!name.trim()) {
				return;
			}
			void createEnvSetMutation.mutateAsync({ name: name.trim(), envVars });
		},
		update: (envSetId: string, name: string, envVars: Record<string, string>) => {
			if (!name.trim()) {
				return;
			}
			void updateEnvSetMutation.mutateAsync({ envSetId, name: name.trim(), envVars });
		},
		remove: (envSetId: string) => {
			void removeEnvSetMutation.mutateAsync(envSetId);
		},
		toggle: (envSetId: string) => {
			if (!list.some((envSet) => envSet.id === envSetId)) {
				return;
			}
			void toggleEnvSetMutation.mutateAsync(envSetId);
		},
		refresh: async () => {
			await args.queryClient.fetchQuery(envSetsQueryOptions(args));
		},
	};
}
