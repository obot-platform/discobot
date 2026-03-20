import type { Workspace } from "$lib/api-types";

export function upsertWorkspace(
	workspaces: Workspace[],
	nextWorkspace: Workspace,
): Workspace[] {
	const existingIndex = workspaces.findIndex(
		(workspace) => workspace.id === nextWorkspace.id,
	);
	if (existingIndex === -1) {
		return [...workspaces, nextWorkspace];
	}

	return workspaces.map((workspace, index) =>
		index === existingIndex ? nextWorkspace : workspace,
	);
}
