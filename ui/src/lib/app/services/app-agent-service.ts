import { queryOptions } from "@tanstack/svelte-query";
import type { QueryClient } from "@tanstack/svelte-query";

import { api } from "$lib/api-client";
import { appQueryKeys } from "$lib/app/query/app-query-keys";

export type AppAgentService = {
	resolveAgentId: () => Promise<string | null>;
};

type CreateAppAgentServiceArgs = {
	queryClient: QueryClient;
};

export function createAppAgentService(args: CreateAppAgentServiceArgs): AppAgentService {
	let agentId: string | null = null;

	return {
		resolveAgentId: async () => {
			if (agentId) {
				return agentId;
			}

			const agents = await args.queryClient.fetchQuery(
				queryOptions({
					queryKey: appQueryKeys.agents(),
					queryFn: async () => {
						const { agents } = await api.getAgents();
						return agents;
					},
				}),
			);
			const agent = agents.find((candidate) => candidate.isDefault) || agents[0];
			if (!agent) {
				return null;
			}

			agentId = agent.id;
			return agent.id;
		},
	};
}
