import type { IdeOption } from "$lib/shell-types";

export const windowControls = ["_", "□", "×"];

export const ideOptions: IdeOption[] = [
	{ id: "cursor", label: "Cursor" },
	{ id: "vscode", label: "VS Code" },
	{ id: "zed", label: "Zed" },
];

export const workflowActions = ["Commit", "Rebase", "Create PR", "Merge"];
