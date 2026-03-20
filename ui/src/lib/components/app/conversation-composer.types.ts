export type ComposerStatus = "ready" | "submitted" | "streaming" | "error";

export type ComposerAttachment = {
	id: string;
	file: File;
	filename: string;
	mediaType: string;
	url: string;
};

export type ComposerMode = "build" | "plan";

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
