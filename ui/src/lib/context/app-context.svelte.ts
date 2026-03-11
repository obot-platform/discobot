import { getContext, hasContext, setContext } from "svelte";

import { api } from "$lib/api-client";
import type {
	AgentModel,
	AuthProvider,
	CredentialInfo,
	Session,
	SupportInfoResponse,
	ThemeColorScheme,
} from "$lib/api-types";
import { getApiBase, isTauriShell } from "$lib/environment";
import type {
	AsyncStatus,
	IdeOption,
	PreferredIde,
	SessionSummary,
	WindowControlsSide,
} from "$lib/shell-types";
import {
	applyColorScheme,
	applyTheme,
	getAvailableThemes,
	getColorScheme,
	getThemeMode,
	resolveThemeMode,
	type ResolvedTheme,
	type ThemeMetadata,
	type ThemeMode,
} from "$lib/theme";

const APP_CONTEXT_KEY = Symbol.for("discobot-ui-app-context");
const PREFERRED_IDE_STORAGE_KEY = "preferred.ide";
const CHAT_WIDTH_MODE_STORAGE_KEY = "chat.width.mode";
const DEFAULT_MODEL_STORAGE_KEY = "chat.default.model";
const IGNORED_UPDATE_VERSION_STORAGE_KEY = "update.ignored.version";
const RECENT_SESSIONS_LIMIT = 4;

export type ChatWidthMode = "full" | "constrained";

export type UpdateStatus =
	| "idle"
	| "checking"
	| "downloading"
	| "ready"
	| "installing"
	| "error";

export type AppCredential = CredentialInfo & {
	apiKey?: string;
};

export type AppContextBootstrap = {
	ideOptions: IdeOption[];
	sessions: SessionSummary[];
	selectedSessionId?: string;
	windowControls: string[];
	workflowActions: string[];
	models: AgentModel[];
	authProviders: AuthProvider[];
	credentials: AppCredential[];
	supportInfo: SupportInfoResponse;
};

function detectWindowControlsSide(): WindowControlsSide {
	if (typeof navigator === "undefined") {
		return "right";
	}

	const nav = navigator as Navigator & {
		userAgentData?: {
			platform?: string;
		};
	};
	const platform = nav.userAgentData?.platform || nav.platform || nav.userAgent;
	return /mac/i.test(platform) ? "left" : "right";
}

function readPreferredIde(): PreferredIde {
	if (typeof window === "undefined") {
		return "cursor";
	}

	const stored = window.localStorage.getItem(PREFERRED_IDE_STORAGE_KEY);
	return stored === "cursor" || stored === "vscode" || stored === "zed"
		? stored
		: "cursor";
}

function readChatWidthMode(): ChatWidthMode {
	if (typeof window === "undefined") {
		return "constrained";
	}

	const stored = window.localStorage.getItem(CHAT_WIDTH_MODE_STORAGE_KEY);
	return stored === "full" ? "full" : "constrained";
}

function readDefaultModel(): string {
	if (typeof window === "undefined") {
		return "";
	}

	return window.localStorage.getItem(DEFAULT_MODEL_STORAGE_KEY) ?? "";
}

function readIgnoredUpdateVersion(): string | null {
	if (typeof window === "undefined") {
		return null;
	}

	return window.localStorage.getItem(IGNORED_UPDATE_VERSION_STORAGE_KEY);
}

function writeStorage(key: string, value: string | null) {
	if (typeof window === "undefined") {
		return;
	}

	if (value === null) {
		window.localStorage.removeItem(key);
		return;
	}

	window.localStorage.setItem(key, value);
}

class AppContextState {
	status = $state<AsyncStatus>("idle");
	errorMessage = $state<string | undefined>(undefined);
	theme = $state<ThemeMode>("system");
	resolvedTheme = $state<ResolvedTheme>("dark");
	colorScheme = $state<ThemeColorScheme>("default");
	preferredIde = $state<PreferredIde>("cursor");
	sessions = $state<SessionSummary[]>([]);
	selectedSessionId = $state<string | null>(null);
	chatWidthMode = $state<ChatWidthMode>("constrained");
	defaultModel = $state("");
	settingsDialogOpen = $state(false);
	credentialsDialogOpen = $state(false);
	supportInfoDialogOpen = $state(false);
	credentials = $state<AppCredential[]>([]);
	supportInfo = $state<SupportInfoResponse | null>(null);
	supportInfoStatus = $state<AsyncStatus>("idle");
	supportInfoError = $state<string | null>(null);
	updateStatus = $state<UpdateStatus>("idle");
	availableVersion = $state<string | null>(null);
	updateError = $state<string | null>(null);
	downloadedBytes = $state(0);
	totalBytes = $state<number | null>(null);
	ignoredUpdateVersion = $state<string | null>(null);

	recentSessions = $derived.by(() => this.sessions.filter((session) => session.isRecent));
	selectedSession = $derived.by(
		() => this.sessions.find((session) => session.id === this.selectedSessionId) ?? null,
	);
	availableThemes = $derived.by(() => getAvailableThemes(this.resolvedTheme));
	isUpdateIgnored = $derived.by(
		() => this.availableVersion !== null && this.ignoredUpdateVersion === this.availableVersion,
	);
	showUpdateBadge = $derived.by(
		() => this.updateStatus === "ready" && this.availableVersion !== null && !this.isUpdateIgnored,
	);

	readonly apiBase = getApiBase();
	readonly isTauri = isTauriShell();
	readonly windowControlsSide = detectWindowControlsSide();
	readonly ideOptions: IdeOption[];
	readonly windowControls: string[];
	readonly workflowActions: string[];
	readonly models: AgentModel[];
	readonly authProviders: AuthProvider[];

	private readonly supportInfoSnapshot: SupportInfoResponse;
	private readonly updateVersion = "0.0.0-dev+1";
	private updateCheckInFlight = false;
	private workspaceId: string | null = null;
	private agentId: string | null = null;

	constructor(bootstrap: AppContextBootstrap) {
		this.ideOptions = bootstrap.ideOptions;
		this.sessions = bootstrap.sessions;
		this.selectedSessionId =
			bootstrap.selectedSessionId &&
			bootstrap.sessions.some((session) => session.id === bootstrap.selectedSessionId)
				? bootstrap.selectedSessionId
				: null;
		this.windowControls = bootstrap.windowControls;
		this.workflowActions = bootstrap.workflowActions;
		this.models = bootstrap.models;
		this.authProviders = bootstrap.authProviders;
		this.credentials = bootstrap.credentials;
		this.supportInfoSnapshot = bootstrap.supportInfo;
	}

	private ensureColorSchemeForMode = () => {
		if (!this.availableThemes.some((theme) => theme.id === this.colorScheme)) {
			this.colorScheme = "default";
		}
	};

	private applyThemeState = (theme: ThemeMode) => {
		this.theme = applyTheme(theme);
		this.resolvedTheme = resolveThemeMode(this.theme);
		this.ensureColorSchemeForMode();
		this.colorScheme = applyColorScheme(this.colorScheme);
	};

	private delay = async (ms: number) => {
		await new Promise((resolve) => window.setTimeout(resolve, ms));
	};

	private formatErrorMessage(error: unknown, fallback: string): string {
		return error instanceof Error ? error.message : fallback;
	}

	private toSessionSummaries = (sessions: Session[]): SessionSummary[] => {
		const sortedSessions = [...sessions].sort((a, b) => {
			const left = new Date(a.timestamp).getTime();
			const right = new Date(b.timestamp).getTime();
			if (Number.isNaN(left) || Number.isNaN(right)) {
				return 0;
			}
			return right - left;
		});

		const recentIds = new Set(
			sortedSessions.slice(0, RECENT_SESSIONS_LIMIT).map((session) => session.id),
		);

		return sortedSessions.map((session) => ({
			id: session.id,
			name: session.displayName || session.name,
			status: session.status,
			isRecent: recentIds.has(session.id),
		}));
	};

	private resolveWorkspaceId = async (): Promise<string | null> => {
		if (this.workspaceId) {
			return this.workspaceId;
		}

		const { workspaces } = await api.getWorkspaces();
		const workspace =
			workspaces.find((candidate) => candidate.status === "ready") || workspaces[0];
		if (!workspace) {
			return null;
		}

		this.workspaceId = workspace.id;
		return workspace.id;
	};

	private resolveAgentId = async (): Promise<string | null> => {
		if (this.agentId) {
			return this.agentId;
		}

		const { agents } = await api.getAgents();
		const agent = agents.find((candidate) => candidate.isDefault) || agents[0];
		if (!agent) {
			return null;
		}

		this.agentId = agent.id;
		return agent.id;
	};

	refreshSessions = async () => {
		try {
			const workspaceId = await this.resolveWorkspaceId();
			if (!workspaceId) {
				this.setSessions([]);
				return;
			}

			const { sessions } = await api.getSessions(workspaceId);
			this.setSessions(this.toSessionSummaries(sessions));
			this.errorMessage = undefined;
		} catch (error) {
			this.errorMessage = this.formatErrorMessage(error, "Failed to load sessions");
		}
	};

	initialize = () => {
		try {
			this.status = "loading";
			this.theme = getThemeMode();
			this.resolvedTheme = resolveThemeMode(this.theme);
			this.colorScheme = getColorScheme();
			this.ensureColorSchemeForMode();
			this.applyThemeState(this.theme);
			this.preferredIde = readPreferredIde();
			this.chatWidthMode = readChatWidthMode();
			this.defaultModel = readDefaultModel();
			this.ignoredUpdateVersion = readIgnoredUpdateVersion();
			this.status = "ready";
			void this.refreshSessions();

			if (typeof window !== "undefined") {
				const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
				mediaQuery.addEventListener("change", () => {
					if (this.theme !== "system") {
						return;
					}
					this.applyThemeState("system");
				});
			}
		} catch (error) {
			this.status = "error";
			this.errorMessage =
				error instanceof Error ? error.message : "Failed to initialize app context";
		}
	};

	setTheme = (theme: ThemeMode) => {
		this.applyThemeState(theme);
	};

	setColorScheme = (scheme: ThemeColorScheme) => {
		if (!this.availableThemes.some((theme) => theme.id === scheme)) {
			return;
		}
		this.colorScheme = applyColorScheme(scheme);
	};

	toggleTheme = () => {
		this.setTheme(this.resolvedTheme === "dark" ? "light" : "dark");
	};

	setPreferredIde = (ide: PreferredIde) => {
		this.preferredIde = ide;
		writeStorage(PREFERRED_IDE_STORAGE_KEY, ide);
	};

	setChatWidthMode = (mode: ChatWidthMode) => {
		this.chatWidthMode = mode;
		writeStorage(CHAT_WIDTH_MODE_STORAGE_KEY, mode);
	};

	setDefaultModel = (modelId: string) => {
		this.defaultModel = modelId;
		writeStorage(DEFAULT_MODEL_STORAGE_KEY, modelId || null);
	};

	setSessions = (sessions: SessionSummary[]) => {
		this.sessions = sessions;
		if (
			this.selectedSessionId &&
			!sessions.some((session) => session.id === this.selectedSessionId)
		) {
			this.selectedSessionId = null;
		}
	};

	selectSession = (sessionId: string) => {
		if (this.sessions.some((session) => session.id === sessionId)) {
			this.selectedSessionId = sessionId;
		}
	};

	createSessionForWorkspace = async (workspaceId: string): Promise<string | null> => {
		try {
			const agentId = await this.resolveAgentId();
			if (!agentId) {
				return null;
			}

			this.workspaceId = workspaceId;

			const generatedId =
				typeof crypto !== "undefined" && "randomUUID" in crypto
					? crypto.randomUUID()
					: `session-${Date.now()}-${Math.floor(Math.random() * 10_000)}`;

			const created = await api.createSession({
				id: generatedId,
				workspaceId,
				agentId,
			});

			await this.refreshSessions();
			this.selectSession(created.id);
			this.errorMessage = undefined;
			return created.id;
		} catch (error) {
			this.errorMessage = this.formatErrorMessage(error, "Failed to create session");
			return null;
		}
	};

	startNewSession = () => {
		this.selectedSessionId = null;
	};

	openSettingsDialog = () => {
		this.credentialsDialogOpen = false;
		this.supportInfoDialogOpen = false;
		this.settingsDialogOpen = true;
	};

	closeSettingsDialog = () => {
		this.settingsDialogOpen = false;
	};

	openCredentialsDialog = () => {
		this.settingsDialogOpen = false;
		this.supportInfoDialogOpen = false;
		this.credentialsDialogOpen = true;
	};

	closeCredentialsDialog = () => {
		this.credentialsDialogOpen = false;
	};

	openSupportInfoDialog = () => {
		this.settingsDialogOpen = false;
		this.credentialsDialogOpen = false;
		this.supportInfoDialogOpen = true;
	};

	closeSupportInfoDialog = () => {
		this.supportInfoDialogOpen = false;
	};

	saveCredential = (providerId: string, apiKey: string, credentialId?: string) => {
		const provider = this.authProviders.find((entry) => entry.id === providerId);
		if (!provider || apiKey.trim().length === 0) {
			return;
		}

		const now = new Date().toISOString();
		if (credentialId) {
			this.credentials = this.credentials.map((credential) =>
				credential.id === credentialId
					? {
							...credential,
							name: provider.name,
							provider: providerId,
							apiKey,
							updatedAt: now,
						}
					: credential,
			);
			return;
		}

		this.credentials = [
			...this.credentials,
			{
				id: `cred-${Date.now()}-${Math.floor(Math.random() * 10_000)}`,
				name: provider.name,
				provider: providerId,
				authType: "api_key",
				isConfigured: true,
				apiKey,
				updatedAt: now,
			},
		];
	};

	removeCredential = (credentialId: string) => {
		this.credentials = this.credentials.filter((credential) => credential.id !== credentialId);
	};

	fetchSupportInfo = async () => {
		this.supportInfoStatus = "loading";
		this.supportInfoError = null;
		try {
			await this.delay(120);
			this.supportInfo = this.supportInfoSnapshot;
			this.supportInfoStatus = "ready";
		} catch (error) {
			this.supportInfoStatus = "error";
			this.supportInfoError = error instanceof Error ? error.message : "Failed to load support info";
		}
	};

	checkForUpdate = async () => {
		if (this.updateCheckInFlight) {
			return;
		}
		if (this.updateStatus === "downloading" || this.updateStatus === "installing") {
			return;
		}

		this.updateCheckInFlight = true;
		this.updateStatus = "checking";
		this.updateError = null;

		try {
			await this.delay(300);
			this.availableVersion = this.updateVersion;

			if (this.isUpdateIgnored) {
				this.updateStatus = "ready";
				this.totalBytes = null;
				this.downloadedBytes = 0;
				return;
			}

			this.updateStatus = "downloading";
			this.totalBytes = 24 * 1024 * 1024;
			this.downloadedBytes = 0;
			for (let step = 1; step <= 8; step += 1) {
				await this.delay(90);
				if (this.updateStatus !== "downloading") {
					return;
				}
				this.downloadedBytes = Math.round((this.totalBytes / 8) * step);
			}

			this.updateStatus = "ready";
		} catch (error) {
			this.updateStatus = "error";
			this.updateError = error instanceof Error ? error.message : "Failed to check for updates";
		} finally {
			this.updateCheckInFlight = false;
		}
	};

	installAndRelaunch = async () => {
		if (this.updateStatus !== "ready") {
			return;
		}

		this.updateStatus = "installing";
		this.updateError = null;
		try {
			await this.delay(700);
			this.updateStatus = "idle";
			this.availableVersion = null;
			this.totalBytes = null;
			this.downloadedBytes = 0;
			this.ignoredUpdateVersion = null;
			writeStorage(IGNORED_UPDATE_VERSION_STORAGE_KEY, null);
		} catch (error) {
			this.updateStatus = "error";
			this.updateError = error instanceof Error ? error.message : "Install failed";
		}
	};

	ignoreVersion = () => {
		if (!this.availableVersion) {
			return;
		}
		this.ignoredUpdateVersion = this.availableVersion;
		writeStorage(IGNORED_UPDATE_VERSION_STORAGE_KEY, this.availableVersion);
	};
}

export function setAppContext(bootstrap: AppContextBootstrap): AppContextState {
	return setContext(APP_CONTEXT_KEY, new AppContextState(bootstrap));
}

export function useAppContext(): AppContextState {
	const context = getContext<AppContextState | undefined>(APP_CONTEXT_KEY);
	if (!context) {
		throw new Error("useAppContext must be used within AppContext provider");
	}
	return context;
}

export function getAppContextIfPresent(): AppContextState | undefined {
	if (!hasContext(APP_CONTEXT_KEY)) {
		return undefined;
	}
	return getContext<AppContextState | undefined>(APP_CONTEXT_KEY);
}
