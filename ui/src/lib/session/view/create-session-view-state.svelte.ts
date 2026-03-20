import type { WorkspaceValidationResult } from "$lib/api-types";
import type { EnvSetEditorMode, SessionActiveView } from "$lib/session/session-view.types";
import { getDefaultActiveView, getSelectedFileFromView } from "$lib/session/session-view.types";

type CreateSessionViewStateArgs = {
	getFiles: () => string[];
};

export type SessionViewState = {
	activeView: SessionActiveView;
	selectedThreadId: string | null;
	selectedFile: string;
	activeServiceId: string | null;
	ideMenuOpen: boolean;
	composerDraft: string;
	desktopSidebarOpen: boolean;
	mobileSidebarOpen: boolean;
	hooksExpanded: boolean;
	queueExpanded: boolean;
	hookDialogOpen: boolean;
	selectedHookId: string | null;
	envSetDialogOpen: boolean;
	envSetEditorMode: EnvSetEditorMode;
	editingEnvSetId: string | null;
	pendingWorkspaceOption: string;
	pendingWorkspaceBranch: string;
	pendingWorkspaceSourceInput: string;
	pendingWorkspaceValidation: WorkspaceValidationResult | null;
	pendingWorkspaceValidating: boolean;
	pendingWorkspaceSetupMessage: string | null;
	pendingWorkspaceRequiresSourceInput: boolean;
	pendingWorkspaceSourceType: "local" | "git";
	pendingWorkspaceSourceIsValid: boolean;
	pendingWorkspaceValidationMessage: string | null;
	selectThread: (threadId: string | null) => void;
	openChat: () => void;
	openTerminal: () => void;
	openDesktop: () => void;
	openDiffReview: () => void;
	openFile: (file?: string) => void;
	openService: (serviceId: string) => void;
	toggleIdeMenu: () => void;
	setComposerDraft: (value: string) => void;
	setPendingWorkspaceOption: (value: string) => void;
	setPendingWorkspaceBranch: (value: string) => void;
	setPendingWorkspaceSourceInput: (value: string) => void;
	setPendingWorkspaceValidation: (value: WorkspaceValidationResult | null) => void;
	setPendingWorkspaceValidating: (value: boolean) => void;
	setPendingWorkspaceSetupMessage: (value: string | null) => void;
	resetPendingWorkspaceSetup: () => void;
	setDesktopSidebarOpen: (value: boolean) => void;
	setMobileSidebarOpen: (value: boolean) => void;
	setHooksExpanded: (value: boolean) => void;
	setQueueExpanded: (value: boolean) => void;
	openHookDialog: (hookId: string) => void;
	closeHookDialog: () => void;
	openEnvSetManager: () => void;
	startEnvSetCreate: () => void;
	startEnvSetEdit: (envSetId: string) => void;
	closeEnvSetManager: () => void;
	resetForSession: (selectedThreadId: string | null, selectedFile: string) => void;
};

export function createSessionViewState(args: CreateSessionViewStateArgs): SessionViewState {
	let activeView = $state<SessionActiveView>({ kind: "chat" });
	let selectedThreadId = $state<string | null>(null);
	let selectedFile = $state("");
	let ideMenuOpen = $state(false);
	let composerDraft = $state("");
	let desktopSidebarOpen = $state(false);
	let mobileSidebarOpen = $state(false);
	let hooksExpanded = $state(false);
	let queueExpanded = $state(false);
	let hookDialogOpen = $state(false);
	let selectedHookId = $state<string | null>(null);
	let envSetDialogOpen = $state(false);
	let envSetEditorMode = $state<EnvSetEditorMode>("list");
	let editingEnvSetId = $state<string | null>(null);
	let pendingWorkspaceOption = $state("new-workspace");
	let pendingWorkspaceBranch = $state("");
	let pendingWorkspaceSourceInput = $state("");
	let pendingWorkspaceValidation = $state<WorkspaceValidationResult | null>(null);
	let pendingWorkspaceValidating = $state(false);
	let pendingWorkspaceSetupMessage = $state<string | null>(null);

	const openChat = () => {
		activeView = { kind: "chat" };
	};

	const openTerminal = () => {
		activeView = { kind: "terminal" };
	};

	const openDesktop = () => {
		activeView = { kind: "desktop" };
	};

	const openDiffReview = () => {
		activeView = { kind: "diff-review" };
	};

	const openFile = (file?: string) => {
		const nextFile = (file ?? selectedFile) || (args.getFiles()[0] ?? "");
		selectedFile = nextFile;
		activeView = nextFile.length > 0 ? { kind: "file", path: nextFile } : getDefaultActiveView(args.getFiles());
	};

	const openService = (serviceId: string) => {
		activeView = { kind: "service", serviceId };
	};

	const closeHookDialog = () => {
		hookDialogOpen = false;
		selectedHookId = null;
	};

	const closeEnvSetManager = () => {
		envSetDialogOpen = false;
		envSetEditorMode = "list";
		editingEnvSetId = null;
	};

	const resetPendingWorkspaceSetup = () => {
		pendingWorkspaceOption = "new-workspace";
		pendingWorkspaceBranch = "";
		pendingWorkspaceSourceInput = "";
		pendingWorkspaceValidation = null;
		pendingWorkspaceValidating = false;
		pendingWorkspaceSetupMessage = null;
	};

	return {
		get activeView() {
			return activeView;
		},
		get selectedThreadId() {
			return selectedThreadId;
		},
		get selectedFile() {
			return activeView.kind === "file" ? activeView.path : selectedFile;
		},
		get activeServiceId() {
			return activeView.kind === "service" ? activeView.serviceId : null;
		},
		get ideMenuOpen() {
			return ideMenuOpen;
		},
		get composerDraft() {
			return composerDraft;
		},
		get desktopSidebarOpen() {
			return desktopSidebarOpen;
		},
		set desktopSidebarOpen(value: boolean) {
			desktopSidebarOpen = value;
		},
		get mobileSidebarOpen() {
			return mobileSidebarOpen;
		},
		set mobileSidebarOpen(value: boolean) {
			mobileSidebarOpen = value;
		},
		get hooksExpanded() {
			return hooksExpanded;
		},
		set hooksExpanded(value: boolean) {
			hooksExpanded = value;
		},
		get queueExpanded() {
			return queueExpanded;
		},
		set queueExpanded(value: boolean) {
			queueExpanded = value;
		},
		get hookDialogOpen() {
			return hookDialogOpen;
		},
		set hookDialogOpen(value: boolean) {
			hookDialogOpen = value;
			if (!value) {
				selectedHookId = null;
			}
		},
		get selectedHookId() {
			return selectedHookId;
		},
		get envSetDialogOpen() {
			return envSetDialogOpen;
		},
		set envSetDialogOpen(value: boolean) {
			envSetDialogOpen = value;
			if (!value) {
				envSetEditorMode = "list";
				editingEnvSetId = null;
			}
		},
		get envSetEditorMode() {
			return envSetEditorMode;
		},
		get editingEnvSetId() {
			return editingEnvSetId;
		},
		get pendingWorkspaceOption() {
			return pendingWorkspaceOption;
		},
		get pendingWorkspaceBranch() {
			return pendingWorkspaceBranch;
		},
		get pendingWorkspaceSourceInput() {
			return pendingWorkspaceSourceInput;
		},
		get pendingWorkspaceValidation() {
			return pendingWorkspaceValidation;
		},
		get pendingWorkspaceValidating() {
			return pendingWorkspaceValidating;
		},
		get pendingWorkspaceSetupMessage() {
			return pendingWorkspaceSetupMessage;
		},
		get pendingWorkspaceRequiresSourceInput() {
			return pendingWorkspaceOption === "local-directory" || pendingWorkspaceOption === "git-repo";
		},
		get pendingWorkspaceSourceType() {
			return pendingWorkspaceOption === "git-repo" ? "git" : "local";
		},
		get pendingWorkspaceSourceIsValid() {
			if (!(pendingWorkspaceOption === "local-directory" || pendingWorkspaceOption === "git-repo")) {
				return true;
			}

			if (pendingWorkspaceSourceInput.trim().length === 0 || pendingWorkspaceValidating) {
				return false;
			}

			return pendingWorkspaceValidation?.valid ?? false;
		},
		get pendingWorkspaceValidationMessage() {
			if (!(pendingWorkspaceOption === "local-directory" || pendingWorkspaceOption === "git-repo")) {
				return null;
			}

			if (pendingWorkspaceSourceInput.trim().length === 0) {
				return null;
			}

			if (pendingWorkspaceValidating) {
				return "Validating workspace...";
			}

			if (!pendingWorkspaceValidation) {
				return null;
			}

			if (!pendingWorkspaceValidation.valid) {
				return pendingWorkspaceValidation.error || "Enter a valid workspace path.";
			}

			switch (pendingWorkspaceValidation.classification) {
				case "new":
					return "A new directory will be created and initialized as a git repository.";
				case "empty":
					return "Empty directory is valid. It will be initialized as a git repository.";
				case "existing_git":
					return "Existing git repository detected.";
				case "cloneable":
					return "Repository is cloneable.";
				default:
					return null;
			}
		},
		selectThread: (threadId) => {
			selectedThreadId = threadId;
		},
		openChat,
		openTerminal,
		openDesktop,
		openDiffReview,
		openFile,
		openService,
		toggleIdeMenu: () => {
			ideMenuOpen = !ideMenuOpen;
		},
		setComposerDraft: (value) => {
			composerDraft = value;
		},
		setPendingWorkspaceOption: (value) => {
			pendingWorkspaceOption = value;
		},
		setPendingWorkspaceBranch: (value) => {
			pendingWorkspaceBranch = value;
		},
		setPendingWorkspaceSourceInput: (value) => {
			pendingWorkspaceSourceInput = value;
		},
		setPendingWorkspaceValidation: (value) => {
			pendingWorkspaceValidation = value;
		},
		setPendingWorkspaceValidating: (value) => {
			pendingWorkspaceValidating = value;
		},
		setPendingWorkspaceSetupMessage: (value) => {
			pendingWorkspaceSetupMessage = value;
		},
		resetPendingWorkspaceSetup,
		setDesktopSidebarOpen: (value) => {
			desktopSidebarOpen = value;
		},
		setMobileSidebarOpen: (value) => {
			mobileSidebarOpen = value;
		},
		setHooksExpanded: (value) => {
			hooksExpanded = value;
		},
		setQueueExpanded: (value) => {
			queueExpanded = value;
		},
		openHookDialog: (hookId) => {
			selectedHookId = hookId;
			hookDialogOpen = true;
		},
		closeHookDialog,
		openEnvSetManager: () => {
			envSetDialogOpen = true;
			envSetEditorMode = "list";
			editingEnvSetId = null;
		},
		startEnvSetCreate: () => {
			envSetDialogOpen = true;
			envSetEditorMode = "create";
			editingEnvSetId = null;
		},
		startEnvSetEdit: (envSetId) => {
			envSetDialogOpen = true;
			envSetEditorMode = "edit";
			editingEnvSetId = envSetId;
		},
		closeEnvSetManager,
		resetForSession: (nextSelectedThreadId, nextSelectedFile) => {
			selectedThreadId = nextSelectedThreadId;
			selectedFile = nextSelectedFile;
			activeView = { kind: "chat" };
			ideMenuOpen = false;
			composerDraft = "";
			desktopSidebarOpen = false;
			mobileSidebarOpen = false;
			hooksExpanded = false;
			queueExpanded = false;
			closeHookDialog();
			closeEnvSetManager();
			resetPendingWorkspaceSetup();
		},
	};
}

export function isChatView(activeView: SessionActiveView): boolean {
	return activeView.kind === "chat";
}

export function getSelectedFile(activeView: SessionActiveView): string {
	return getSelectedFileFromView(activeView);
}
