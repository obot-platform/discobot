import { getContext, hasContext, setContext } from "svelte";

import type { AppContext, AppContextBootstrap, SettingsDialogTab } from "$lib/app/app-context.types";
import { createAppCredentialsDomain } from "$lib/app/domains/app-credentials.svelte";
import { createAppModelsDomain } from "$lib/app/domains/app-models.svelte";
import { createAppSessionsDomain } from "$lib/app/domains/app-sessions.svelte";
import { createAppSupportInfoDomain } from "$lib/app/domains/app-support-info.svelte";
import { createAppWorkspacesDomain } from "$lib/app/domains/app-workspaces.svelte";
import {
	createAppPreferencesService,
	createAppUpdateService,
} from "$lib/app/services";
import { createAppStore } from "$lib/app/store/app-store.svelte";
import { createAppViewState } from "$lib/app/view/create-app-view-state.svelte";
import { getQueryClient } from "$lib/query/query-client";

export type {
	AppContext,
	AppContextBootstrap,
	AppCredential,
	ChatWidthMode,
	SettingsDialogTab,
	UpdateStatus,
} from "$lib/app/app-context.types";

const APP_CONTEXT_KEY = Symbol.for("discobot-ui-app-context");

function createAppContext(bootstrap: AppContextBootstrap): AppContext {
	const queryClient = getQueryClient();
	const store = createAppStore(bootstrap);
	const view = createAppViewState(bootstrap.selectedSessionId);

	const preferencesService = createAppPreferencesService({ store });
	const updateService = createAppUpdateService({ store });
	const workspaces = createAppWorkspacesDomain({ queryClient });
	const models = createAppModelsDomain();
	const supportInfo = createAppSupportInfoDomain();
	const credentials = createAppCredentialsDomain({ queryClient });
	const sessions = createAppSessionsDomain({
		store,
		view,
		queryClient,
		setResolvedWorkspaceId: workspaces.select,
	});

	const ui = {
		get selectedSessionId() {
			return view.selectedSessionId;
		},
		set selectedSessionId(value: string | null) {
			view.selectedSessionId = value;
		},
		get credentialFlowIntent() {
			return view.credentialFlowIntent;
		},
		set credentialFlowIntent(value: "github-git" | null) {
			view.credentialFlowIntent = value;
		},
		get supportInfoDialogOpen() {
			return view.supportInfoDialogOpen;
		},
		set supportInfoDialogOpen(value: boolean) {
			view.supportInfoDialogOpen = value;
		},
		settingsDialog: {
			get open() {
				return view.settingsDialogOpen;
			},
			set open(value: boolean) {
				view.settingsDialogOpen = value;
			},
			get tab() {
				return view.settingsDialogTab;
			},
			set tab(value) {
				view.settingsDialogTab = value;
			},
		},
		openSettings: (tab?: SettingsDialogTab) => {
			if (tab) {
				view.openSettingsDialogAt(tab);
				return;
			}
			view.openSettingsDialog();
		},
		closeSettings: view.closeSettingsDialog,
		openGitHubCredentialFlow: view.openGitHubCredentialFlow,
		openSupportInfo: view.openSupportInfoDialog,
		closeSupportInfo: view.closeSupportInfoDialog,
	};

	const preferences = {
		get theme() {
			return store.theme;
		},
		get resolvedTheme() {
			return store.resolvedTheme;
		},
		get colorScheme() {
			return store.colorScheme;
		},
		get availableThemes() {
			return store.availableThemes;
		},
		get preferredIde() {
			return store.preferredIde;
		},
		get ideOptions() {
			return store.ideOptions;
		},
		get chatWidthMode() {
			return store.chatWidthMode;
		},
		get defaultModel() {
			return store.defaultModel;
		},
		setTheme: preferencesService.setTheme,
		setColorScheme: preferencesService.setColorScheme,
		toggleTheme: preferencesService.toggleTheme,
		setPreferredIde: preferencesService.setPreferredIde,
		setChatWidthMode: preferencesService.setChatWidthMode,
		setDefaultModel: preferencesService.setDefaultModel,
	};

	const environment = {
		apiBase: store.apiBase,
		isTauri: store.isTauri,
		windowControlsSide: store.windowControlsSide,
		windowControls: store.windowControls,
		workflowActions: store.workflowActions,
	};

	const updates = {
		get status() {
			return store.updateStatus;
		},
		get availableVersion() {
			return store.availableVersion;
		},
		get error() {
			return store.updateError;
		},
		get downloadedBytes() {
			return store.downloadedBytes;
		},
		get totalBytes() {
			return store.totalBytes;
		},
		get isIgnored() {
			return store.isUpdateIgnored;
		},
		get showBadge() {
			return store.showUpdateBadge;
		},
		check: updateService.checkForUpdate,
		installAndRelaunch: updateService.installAndRelaunch,
		ignore: updateService.ignoreVersion,
	};

	try {
		store.status = "loading";
		preferencesService.initialize();
		store.status = "ready";
		void sessions.refresh();
		void workspaces.refresh();
		void models.refresh();
	} catch (error) {
		store.status = "error";
		store.errorMessage =
			error instanceof Error ? error.message : "Failed to initialize app context";
	}

	return {
		ui,
		preferences,
		environment,
		sessions,
		workspaces,
		models,
		credentials,
		supportInfo,
		updates,
	};
}

export function setAppContext(bootstrap: AppContextBootstrap): AppContext {
	const context = createAppContext(bootstrap);
	setContext(APP_CONTEXT_KEY, context);
	return context;
}

export function useAppContext(): AppContext {
	const context = getContext<AppContext | undefined>(APP_CONTEXT_KEY);
	if (!context) {
		throw new Error("useAppContext must be used within AppContext provider");
	}
	return context;
}

export function getAppContextIfPresent(): AppContext | undefined {
	if (!hasContext(APP_CONTEXT_KEY)) {
		return undefined;
	}
	return getContext<AppContext | undefined>(APP_CONTEXT_KEY);
}
