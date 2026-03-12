import { createQuery, queryOptions } from "@tanstack/svelte-query";
import type { QueryClient } from "@tanstack/svelte-query";

import { api } from "$lib/api-client";
import type { Session } from "$lib/api-types";
import { toServiceItem } from "$lib/session/domains/session-domain.helpers";
import type { SessionServicesDomain } from "$lib/session/session-context.types";

const SERVICES_DOMAIN = "services";

type CreateSessionServicesDomainArgs = {
	queryClient: QueryClient;
	getSession: () => Session | null;
	key: (...parts: string[]) => readonly unknown[];
	getActiveServiceId: () => string | null;
	openService: (serviceId: string) => void;
};

function servicesQueryOptions(args: CreateSessionServicesDomainArgs, sessionId: string) {
	return queryOptions({
		queryKey: args.key(SERVICES_DOMAIN),
		queryFn: async () => {
			const { services } = await api.getServices(sessionId);
			return services;
		},
		initialData: [],
	});
}

export function createSessionServicesDomain(
	args: CreateSessionServicesDomainArgs,
): SessionServicesDomain {
	const servicesQuery = createQuery(() => {
		const sessionId = args.getSession()?.id;
		return queryOptions({
			queryKey: args.key(SERVICES_DOMAIN),
			queryFn: async () => {
				if (!sessionId) {
					return [];
				}
				const { services } = await api.getServices(sessionId);
				return services;
			},
			initialData: [],
		});
	});

	const list = $derived.by(() => (servicesQuery.data ?? []).map(toServiceItem));
	const active = $derived.by(
		() => list.find((service) => service.id === args.getActiveServiceId()) ?? null,
	);

	return {
		get list() {
			return list;
		},
		get active() {
			return active;
		},
		open: args.openService,
		refresh: async () => {
			const sessionId = args.getSession()?.id;
			if (!sessionId) {
				return;
			}
			await args.queryClient.fetchQuery(servicesQueryOptions(args, sessionId));
		},
	};
}
