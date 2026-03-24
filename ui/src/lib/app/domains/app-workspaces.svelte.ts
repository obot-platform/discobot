import type { AppWorkspaces } from "$lib/app/app-context.types";
import type { WorkspaceStore } from "$lib/store/workspaces.store.svelte";

type CreateAppWorkspacesDomainArgs = {
	store: WorkspaceStore;
};

export function createAppWorkspacesDomain(
	args: CreateAppWorkspacesDomainArgs,
): AppWorkspaces {
	const { store } = args;

	return {
		get list() {
			return store.list;
		},
		get status() {
			return store.status;
		},
		get: (workspaceId) => store.get(workspaceId),
		refresh: () => store.fetch(),
		reloadWorkspace: (workspaceId) => store.fetchOne(workspaceId),
		validate: (path, sourceType) => store.validate(path, sourceType),
		create: (data) => store.create(data),
	};
}
