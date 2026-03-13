import { createMutation, createQuery, queryOptions } from "@tanstack/svelte-query";
import type { QueryClient } from "@tanstack/svelte-query";

import { api } from "$lib/api-client";
import type { AppWorkspaces } from "$lib/app/app-context.types";
import { appQueryKeys } from "$lib/app/query/app-query-keys";
import type { CreateWorkspaceRequest, Workspace, WorkspaceValidationResult } from "$lib/api-types";

type CreateAppWorkspacesDomainArgs = {
	queryClient: QueryClient;
};

function workspacesQueryOptions() {
	return queryOptions({
		queryKey: appQueryKeys.workspaces(),
		queryFn: async (): Promise<Workspace[]> => {
			const { workspaces } = await api.getWorkspaces();
			return workspaces;
		},
	});
}

export function createAppWorkspacesDomain(
	args: CreateAppWorkspacesDomainArgs,
): AppWorkspaces {
	const workspacesQuery = createQuery(() => workspacesQueryOptions());
	const list = $derived.by(() => workspacesQuery.data ?? []);
	const status = $derived.by(() => {
		if (workspacesQuery.isPending) {
			return "loading" as const;
		}
		if (workspacesQuery.isError) {
			return "error" as const;
		}
		if (workspacesQuery.isSuccess) {
			return "ready" as const;
		}
		return "idle" as const;
	});

	const createWorkspaceMutation = createMutation(() => ({
		mutationFn: async (data: CreateWorkspaceRequest) => api.createWorkspace(data),
		onSuccess: (workspace) => {
			args.queryClient.setQueryData<Workspace[]>(appQueryKeys.workspaces(), (previous) => {
				const next = previous ? [...previous] : [];
				if (!next.some((item) => item.id === workspace.id)) {
					next.push(workspace);
				}
				return next;
			});
		},
	}));

	return {
		get list() {
			return list;
		},
		get status() {
			return status;
		},
		get: (workspaceId: string) =>
			list.find((workspace) => workspace.id === workspaceId) ?? null,
		refresh: async () => {
			await workspacesQuery.refetch();
		},
		validate: async (path: string, sourceType: "local" | "git") => {
			return api.validateWorkspace({ path, sourceType });
		},
		create: async (data: CreateWorkspaceRequest) => {
			return createWorkspaceMutation.mutateAsync(data);
		},
	};
}
