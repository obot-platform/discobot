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
	creatingSessionSetup: boolean;
	setupMessage: string | null;
};

export type WorkspaceSelectorHandle = {
	ensureSessionReady: () => Promise<boolean>;
	resetForNewSession: () => void;
};

export type ConversationComposerSubmitPayload = {
	text: string;
	attachments: ComposerAttachment[];
	mode: ComposerMode;
	modelId: string | null;
	reasoning: boolean;
};

export type ConversationComposerTextareaHandle = {
	closeMentionDropdown: () => void;
};
