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
	desktopThreadsOpen: boolean;
	mobileThreadsOpen: boolean;
	hooksExpanded: boolean;
	queueExpanded: boolean;
	hookDialogOpen: boolean;
	selectedHookId: string | null;
	envSetDialogOpen: boolean;
	envSetEditorMode: EnvSetEditorMode;
	editingEnvSetId: string | null;
	selectThread: (threadId: string | null) => void;
	openChat: () => void;
	openTerminal: () => void;
	openDesktop: () => void;
	openDiffReview: () => void;
	openFile: (file?: string) => void;
	openService: (serviceId: string) => void;
	toggleIdeMenu: () => void;
	setComposerDraft: (value: string) => void;
	setDesktopThreadsOpen: (value: boolean) => void;
	setMobileThreadsOpen: (value: boolean) => void;
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
	let desktopThreadsOpen = $state(false);
	let mobileThreadsOpen = $state(false);
	let hooksExpanded = $state(false);
	let queueExpanded = $state(false);
	let hookDialogOpen = $state(false);
	let selectedHookId = $state<string | null>(null);
	let envSetDialogOpen = $state(false);
	let envSetEditorMode = $state<EnvSetEditorMode>("list");
	let editingEnvSetId = $state<string | null>(null);

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
		get desktopThreadsOpen() {
			return desktopThreadsOpen;
		},
		set desktopThreadsOpen(value: boolean) {
			desktopThreadsOpen = value;
		},
		get mobileThreadsOpen() {
			return mobileThreadsOpen;
		},
		set mobileThreadsOpen(value: boolean) {
			mobileThreadsOpen = value;
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
		setDesktopThreadsOpen: (value) => {
			desktopThreadsOpen = value;
		},
		setMobileThreadsOpen: (value) => {
			mobileThreadsOpen = value;
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
			desktopThreadsOpen = false;
			mobileThreadsOpen = false;
			hooksExpanded = false;
			queueExpanded = false;
			closeHookDialog();
			closeEnvSetManager();
		},
	};
}

export function isChatView(activeView: SessionActiveView): boolean {
	return activeView.kind === "chat";
}

export function getSelectedFile(activeView: SessionActiveView): string {
	return getSelectedFileFromView(activeView);
}
