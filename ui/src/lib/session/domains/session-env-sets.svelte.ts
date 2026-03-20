import { api } from "$lib/api-client";
import type { Session } from "$lib/api-types";
import { getActiveEnvSets } from "$lib/session/domains/session-domain.helpers";
import type { SessionEnvSetsDomain } from "$lib/session/session-context.types";
import type { EnvSetStore } from "$lib/store/env-sets.store.svelte";

type CreateSessionEnvSetsDomainArgs = {
	store: EnvSetStore;
	sessionId: string;
	hasSession: () => boolean;
	getSession: () => Session | null;
	reloadSession: () => Promise<void>;
};

export function createSessionEnvSetsDomain(
	args: CreateSessionEnvSetsDomainArgs,
): SessionEnvSetsDomain {
	const { store } = args;

	const activeIds = $derived(args.getSession()?.activeEnvSetIds ?? []);
	const active = $derived(getActiveEnvSets(store.list, activeIds));

	return {
		get list() {
			return store.list;
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
			void store.create(name, envVars);
		},
		update: (envSetId: string, name: string, envVars: Record<string, string>) => {
			if (!name.trim()) {
				return;
			}
			void store.update(envSetId, name, envVars);
		},
		remove: (envSetId: string) => {
			void store.remove(envSetId).then(() => {
				const session = args.getSession();
				if (session?.activeEnvSetIds?.includes(envSetId)) {
					void args.reloadSession();
				}
			});
		},
		toggle: (envSetId: string) => {
			if (!store.list.some((envSet) => envSet.id === envSetId)) {
				return;
			}
			if (!args.hasSession()) {
				return;
			}
			const nextIds = activeIds.includes(envSetId)
				? activeIds.filter((id) => id !== envSetId)
				: [...activeIds, envSetId];
			void api.setSessionActiveEnvSets(args.sessionId, nextIds).then(() => {
				void args.reloadSession();
			});
		},
		refresh: async () => {
			if (!args.hasSession()) {
				return;
			}
			await store.fetch();
		},
	};
}
