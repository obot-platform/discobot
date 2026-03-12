import { createQuery, queryOptions } from "@tanstack/svelte-query";

import { api } from "$lib/api-client";
import type { AppModels } from "$lib/app/app-context.types";
import { appQueryKeys } from "$lib/app/query/app-query-keys";
import type { ModelInfo } from "$lib/api-types";

function modelsQueryOptions() {
	return queryOptions({
		queryKey: appQueryKeys.models(),
		queryFn: async (): Promise<ModelInfo[]> => {
			const { models } = await api.getProjectModels();
			return models;
		},
	});
}

export function createAppModelsDomain(): AppModels {
	const modelsQuery = createQuery(() => modelsQueryOptions());
	const list = $derived.by(() => modelsQuery.data ?? []);

	return {
		get list() {
			return list;
		},
		refresh: async () => {
			await modelsQuery.refetch();
		},
	};
}
