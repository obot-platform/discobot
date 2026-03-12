import type { AppContextBootstrap, ChatWidthMode, UpdateStatus } from "$lib/app/app-context.types";
import { getAppEnvironment } from "$lib/app/app-helpers";
import type { ThemeColorScheme } from "$lib/api-types";
import type { AsyncStatus, PreferredIde } from "$lib/shell-types";
import {
	getAvailableThemes,
	type ResolvedTheme,
	type ThemeMode,
} from "$lib/theme";

export type AppStore = {
	status: AsyncStatus;
	errorMessage: string | undefined;
	theme: ThemeMode;
	resolvedTheme: ResolvedTheme;
	colorScheme: ThemeColorScheme;
	preferredIde: PreferredIde;
	chatWidthMode: ChatWidthMode;
	defaultModel: string;
	updateStatus: UpdateStatus;
	availableVersion: string | null;
	updateError: string | null;
	downloadedBytes: number;
	totalBytes: number | null;
	ignoredUpdateVersion: string | null;
	availableThemes: ReturnType<typeof getAvailableThemes>;
	isUpdateIgnored: boolean;
	showUpdateBadge: boolean;
	apiBase: string;
	isTauri: boolean;
	windowControlsSide: ReturnType<typeof getAppEnvironment>["windowControlsSide"];
	ideOptions: AppContextBootstrap["ideOptions"];
	windowControls: string[];
	workflowActions: string[];
};

export function createAppStore(bootstrap: AppContextBootstrap): AppStore {
	let status = $state<AsyncStatus>("idle");
	let errorMessage = $state<string | undefined>(undefined);
	let theme = $state<ThemeMode>("system");
	let resolvedTheme = $state<ResolvedTheme>("dark");
	let colorScheme = $state<ThemeColorScheme>("default");
	let preferredIde = $state<PreferredIde>(bootstrap.ideOptions[0]?.id ?? "cursor");
	let chatWidthMode = $state<ChatWidthMode>("constrained");
	let defaultModel = $state("");
	let updateStatus = $state<UpdateStatus>("idle");
	let availableVersion = $state<string | null>(null);
	let updateError = $state<string | null>(null);
	let downloadedBytes = $state(0);
	let totalBytes = $state<number | null>(null);
	let ignoredUpdateVersion = $state<string | null>(null);

	const availableThemes = $derived.by(() => getAvailableThemes(resolvedTheme));
	const isUpdateIgnored = $derived.by(
		() => availableVersion !== null && ignoredUpdateVersion === availableVersion,
	);
	const showUpdateBadge = $derived.by(
		() => updateStatus === "ready" && availableVersion !== null && !isUpdateIgnored,
	);

	const environment = getAppEnvironment();

	return {
		get status() {
			return status;
		},
		set status(value) {
			status = value;
		},
		get errorMessage() {
			return errorMessage;
		},
		set errorMessage(value) {
			errorMessage = value;
		},
		get theme() {
			return theme;
		},
		set theme(value) {
			theme = value;
		},
		get resolvedTheme() {
			return resolvedTheme;
		},
		set resolvedTheme(value) {
			resolvedTheme = value;
		},
		get colorScheme() {
			return colorScheme;
		},
		set colorScheme(value) {
			colorScheme = value;
		},
		get preferredIde() {
			return preferredIde;
		},
		set preferredIde(value) {
			preferredIde = value;
		},
		get chatWidthMode() {
			return chatWidthMode;
		},
		set chatWidthMode(value) {
			chatWidthMode = value;
		},
		get defaultModel() {
			return defaultModel;
		},
		set defaultModel(value) {
			defaultModel = value;
		},
		get updateStatus() {
			return updateStatus;
		},
		set updateStatus(value) {
			updateStatus = value;
		},
		get availableVersion() {
			return availableVersion;
		},
		set availableVersion(value) {
			availableVersion = value;
		},
		get updateError() {
			return updateError;
		},
		set updateError(value) {
			updateError = value;
		},
		get downloadedBytes() {
			return downloadedBytes;
		},
		set downloadedBytes(value) {
			downloadedBytes = value;
		},
		get totalBytes() {
			return totalBytes;
		},
		set totalBytes(value) {
			totalBytes = value;
		},
		get ignoredUpdateVersion() {
			return ignoredUpdateVersion;
		},
		set ignoredUpdateVersion(value) {
			ignoredUpdateVersion = value;
		},
		get availableThemes() {
			return availableThemes;
		},
		get isUpdateIgnored() {
			return isUpdateIgnored;
		},
		get showUpdateBadge() {
			return showUpdateBadge;
		},
		apiBase: environment.apiBase,
		isTauri: environment.isTauri,
		windowControlsSide: environment.windowControlsSide,
		ideOptions: bootstrap.ideOptions,
		windowControls: bootstrap.windowControls,
		workflowActions: bootstrap.workflowActions,
	};
}
