import type { AppUI, SettingsDialogTab } from "$lib/app/app-context.types";
import { getDefaultSettingsTab } from "$lib/app/app-helpers";

export type AppViewState = AppUI & {
	credentialsDialogOpen: boolean;
	openSettingsDialogAt: (tab: SettingsDialogTab) => void;
	openCredentialsDialog: () => void;
	closeCredentialsDialog: () => void;
};

export function createAppViewState(): AppViewState {
	let settingsDialogTab = $state<SettingsDialogTab>(getDefaultSettingsTab());
	let credentialFlowIntent = $state<"github-git" | null>(null);
	let settingsDialogOpen = $state(false);
	let credentialsDialogOpen = $state(false);
	let supportInfoDialogOpen = $state(false);

	const openSettingsDialogAt = (tab: SettingsDialogTab) => {
		credentialsDialogOpen = false;
		supportInfoDialogOpen = false;
		settingsDialogTab = tab;
		settingsDialogOpen = true;
	};

	const settingsDialog = {
		get open() {
			return settingsDialogOpen;
		},
		set open(value: boolean) {
			settingsDialogOpen = value;
		},
		get tab() {
			return settingsDialogTab;
		},
		set tab(value: SettingsDialogTab) {
			settingsDialogTab = value;
		},
	};

	return {
		get credentialFlowIntent() {
			return credentialFlowIntent;
		},
		set credentialFlowIntent(value) {
			credentialFlowIntent = value;
		},
		get supportInfoDialogOpen() {
			return supportInfoDialogOpen;
		},
		set supportInfoDialogOpen(value) {
			supportInfoDialogOpen = value;
		},
		get credentialsDialogOpen() {
			return credentialsDialogOpen;
		},
		set credentialsDialogOpen(value) {
			credentialsDialogOpen = value;
		},
		settingsDialog,
		openSettings: (tab?: SettingsDialogTab) => {
			credentialFlowIntent = null;
			openSettingsDialogAt(tab ?? getDefaultSettingsTab());
		},
		closeSettings: () => {
			settingsDialogOpen = false;
			settingsDialogTab = getDefaultSettingsTab();
			credentialFlowIntent = null;
		},
		openGitHubCredentialFlow: () => {
			credentialFlowIntent = "github-git";
			openSettingsDialogAt("credentials");
		},
		openSupportInfo: () => {
			settingsDialogOpen = false;
			credentialsDialogOpen = false;
			supportInfoDialogOpen = true;
		},
		closeSupportInfo: () => {
			supportInfoDialogOpen = false;
		},
		openSettingsDialogAt,
		openCredentialsDialog: () => {
			credentialsDialogOpen = true;
			openSettingsDialogAt("credentials");
		},
		closeCredentialsDialog: () => {
			credentialsDialogOpen = false;
		},
	};
}
