import { SvelteMap } from "svelte/reactivity";

import type { AppWorkspaces } from "$lib/app/app-context.types";
import {
	createBackgroundRefresh,
	createCoalescedReload,
} from "$lib/context/create-coalesced-reload";
import type { WorkspaceStore } from "$lib/store/workspaces.store.svelte";

type CreateAppWorkspacesDomainArgs = {
	store: WorkspaceStore;
};

export function createAppWorkspacesDomain(
	args: CreateAppWorkspacesDomainArgs,
): AppWorkspaces {
	const { store } = args;
	const reloadWorkspaceById = new SvelteMap<string, () => Promise<void>>();
	const refreshNow = createCoalescedReload(async () => {
		await store.fetch();
	});
	const refresh = createBackgroundRefresh(
		refreshNow,
		"[AppWorkspaces] Failed to refresh workspaces",
	);
	const reloadWorkspace = async (workspaceId: string) => {
		let reload = reloadWorkspaceById.get(workspaceId);
		if (!reload) {
			reload = createCoalescedReload(async () => {
				await store.fetchOne(workspaceId);
			});
			reloadWorkspaceById.set(workspaceId, reload);
		}
		await reload();
	};

	return {
		get list() {
			return store.list;
		},
		get status() {
			return store.status;
		},
		get: (workspaceId) => store.get(workspaceId),
		refresh,
		refreshNow,
		reloadWorkspace,
		validate: (path, sourceType) => store.validate(path, sourceType),
		create: (data) => store.create(data),
	};
}
