import type { SettingsDialogTab } from "$lib/app/app-context.types";
import { getDefaultSettingsTab } from "$lib/app/app-helpers";
import type { SessionSummary } from "$lib/shell-types";

export type AppViewState = {
	selectedSessionId: string | null;
	settingsDialogTab: SettingsDialogTab;
	credentialFlowIntent: "github-git" | null;
	settingsDialogOpen: boolean;
	credentialsDialogOpen: boolean;
	supportInfoDialogOpen: boolean;
	selectSession: (sessionId: string) => void;
	startNewSession: () => void;
	reconcileSelectedSession: (sessions: SessionSummary[], preferredId?: string | null) => void;
	openSettingsDialog: () => void;
	openSettingsDialogAt: (tab: SettingsDialogTab) => void;
	closeSettingsDialog: () => void;
	openCredentialsDialog: () => void;
	openGitHubCredentialFlow: () => void;
	closeCredentialsDialog: () => void;
	openSupportInfoDialog: () => void;
	closeSupportInfoDialog: () => void;
};

export function getReconciledSelectedSessionId(
	sessions: SessionSummary[],
	currentSelectedId: string | null,
	preferredId?: string | null,
): string | null {
	if (preferredId && sessions.some((session) => session.id === preferredId)) {
		return preferredId;
	}
	if (currentSelectedId && sessions.some((session) => session.id === currentSelectedId)) {
		return currentSelectedId;
	}
	return null;
}

export function createAppViewState(selectedSessionId?: string): AppViewState {
	let currentSelectedSessionId = $state<string | null>(selectedSessionId ?? null);
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

	return {
		get selectedSessionId() {
			return currentSelectedSessionId;
		},
		set selectedSessionId(value) {
			currentSelectedSessionId = value;
		},
		get settingsDialogTab() {
			return settingsDialogTab;
		},
		set settingsDialogTab(value) {
			settingsDialogTab = value;
		},
		get credentialFlowIntent() {
			return credentialFlowIntent;
		},
		set credentialFlowIntent(value) {
			credentialFlowIntent = value;
		},
		get settingsDialogOpen() {
			return settingsDialogOpen;
		},
		set settingsDialogOpen(value) {
			settingsDialogOpen = value;
		},
		get credentialsDialogOpen() {
			return credentialsDialogOpen;
		},
		set credentialsDialogOpen(value) {
			credentialsDialogOpen = value;
		},
		get supportInfoDialogOpen() {
			return supportInfoDialogOpen;
		},
		set supportInfoDialogOpen(value) {
			supportInfoDialogOpen = value;
		},
		selectSession: (sessionId) => {
			currentSelectedSessionId = sessionId;
		},
		startNewSession: () => {
			currentSelectedSessionId = null;
		},
		reconcileSelectedSession: (sessions, preferredId) => {
			currentSelectedSessionId = getReconciledSelectedSessionId(
				sessions,
				currentSelectedSessionId,
				preferredId,
			);
		},
		openSettingsDialog: () => {
			credentialFlowIntent = null;
			openSettingsDialogAt(getDefaultSettingsTab());
		},
		openSettingsDialogAt,
		closeSettingsDialog: () => {
			settingsDialogOpen = false;
			settingsDialogTab = getDefaultSettingsTab();
			credentialFlowIntent = null;
		},
		openCredentialsDialog: () => {
			credentialsDialogOpen = true;
			openSettingsDialogAt("credentials");
		},
		openGitHubCredentialFlow: () => {
			credentialFlowIntent = "github-git";
			openSettingsDialogAt("credentials");
		},
		closeCredentialsDialog: () => {
			credentialsDialogOpen = false;
		},
		openSupportInfoDialog: () => {
			settingsDialogOpen = false;
			credentialsDialogOpen = false;
			supportInfoDialogOpen = true;
		},
		closeSupportInfoDialog: () => {
			supportInfoDialogOpen = false;
		},
	};
}
