import type { AppModels } from "$lib/app/app-context.types";
import type { ModelStore } from "$lib/store/models.store.svelte";

type CreateAppModelsDomainArgs = {
	store: ModelStore;
};

export function createAppModelsDomain(
	args: CreateAppModelsDomainArgs,
): AppModels {
	const { store } = args;

	return {
		get list() {
			return store.list;
		},
		refresh: () => store.fetch(),
	};
}
