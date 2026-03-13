import type { WorkspaceValidationResult } from "$lib/api-types";

export type ComposerStatus = "ready" | "submitted" | "streaming" | "error";

export type ComposerAttachment = {
	id: string;
	file: File;
	filename: string;
	mediaType: string;
	url: string;
};

export type ComposerMode = "build" | "plan";

export type WorkspaceSelectorState = {
	selectedWorkspaceOption: string;
	selectedWorkspaceBranch: string;
	requiresSourceInput: boolean;
	workspaceSourceInput: string;
	workspaceSourceType: "local" | "git";
	workspaceValidation: WorkspaceValidationResult | null;
	workspaceSourceIsValid: boolean;
	workspaceValidationMessage: string | null;
	validatingWorkspaceSource: boolean;
	setupMessage: string | null;
};

export type WorkspaceSelectionResult = {
	ready: boolean;
	workspaceId: string | null;
	workspaceType: "local" | "git" | null;
	workspacePath: string | null;
};

export type WorkspaceSelectorHandle = {
	getWorkspaceSelection: () => Promise<WorkspaceSelectionResult>;
	resetForNewSession: () => void;
};

export type ConversationComposerTextareaHandle = {
	closeMentionDropdown: () => void;
};
