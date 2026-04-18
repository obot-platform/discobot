import type { SessionStore } from "$lib/store/sessions.store.svelte";
import type { WorkspaceStore } from "$lib/store/workspaces.store.svelte";
import type { ModelStore } from "$lib/store/models.store.svelte";
import type { CredentialStore } from "$lib/store/credentials.store.svelte";
import type { StartupTaskStore } from "$lib/store/startup-tasks.store.svelte";
import type { RecentThreadStore } from "$lib/store/recent-threads.store.svelte";
import type { UIStateStore } from "$lib/store/ui-state.store.svelte";
import type {
	ModelInfo,
	CodexAuthorizeResponse,
	CodexCallbackStatusRequest,
	CodexCallbackStatusResponse,
	CodexDeviceCodeResponse,
	CodexExchangeRequest,
	CodexExchangeResponse,
	CodexPollRequest,
	CodexPollResponse,
	CreateWorkspaceRequest,
	CredentialInfo,
	CredentialType,
	GitHubAuthorizeRequest,
	GitHubAuthorizeResponse,
	GitHubCallbackStatusRequest,
	GitHubCallbackStatusResponse,
	GitHubDeviceCodeRequest,
	GitHubDeviceCodeResponse,
	GitHubExchangeRequest,
	GitHubExchangeResponse,
	GitHubPollRequest,
	GitHubPollResponse,
	OAuthAuthorizeResponse,
	OAuthExchangeRequest,
	OAuthExchangeResponse,
	OAuthRefreshResponse,
	CredentialAuthType,
	CredentialEnvVar,
	Session,
	StartChatRequest,
	StartChatResponse,
	StartupTask,
	SupportInfoResponse,
	ThemeColorScheme,
	Workspace,
	WorkspaceValidationResult,
} from "$lib/api-types";
import type { SessionContextValue } from "$lib/session/session-context.types";
import type { ChatStreamManager } from "$lib/thread/chat-stream-manager";
import type {
	AsyncStatus,
	IdeOption,
	PreferredIde,
	RecentThreadSummary,
	SessionSummary,
	WindowControlsSide,
} from "$lib/shell-types";
import type { ThemeMetadata, ThemeMode, ResolvedTheme } from "$lib/theme";
import type { DesktopRuntimeKind } from "$lib/desktop/types";

export type ChatWidthMode = "full" | "constrained";

export type SettingsDialogTab =
	| "appearance"
	| "chat"
	| "project"
	| "update"
	| "credentials";

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
	selectedSessionId?: string;
	selectedThreadId?: string;
	windowControls: string[];
};

export type AppUI = {
	credentialFlowIntent: "github-git" | "codex" | null;
	credentialsDialogTargetId: string | null;
	supportInfoDialogOpen: boolean;
	visibleRecentThreads: RecentThreadSummary[];
	mountedSessionIds: string[];
	settingsDialog: {
		open: boolean;
		tab: SettingsDialogTab;
	};
	openSettings: (tab?: SettingsDialogTab) => void;
	closeSettings: () => void;
	openGitHubCredentialFlow: () => void;
	openCredentialsDialog: (credentialId?: string | Event) => void;
	closeCredentialsDialog: () => void;
	openSupportInfo: () => void;
	closeSupportInfo: () => void;
};

export type AppPreferences = {
	theme: ThemeMode;
	resolvedTheme: ResolvedTheme;
	colorScheme: ThemeColorScheme;
	availableThemes: ThemeMetadata[];
	promptHistory: string[];
	pinnedPrompts: string[];
	preferredIde: PreferredIde;
	ideOptions: IdeOption[];
	chatWidthMode: ChatWidthMode;
	defaultModel: string;
	recentThreadsVisibleLimit: number;
	sidebarRecentOpen: boolean;
	sidebarAllOpen: boolean;
	sidebarAllGroupedByWorkspace: boolean;
	showRefreshButton: boolean;
	setTheme: (theme: ThemeMode) => void;
	setColorScheme: (scheme: ThemeColorScheme) => void;
	toggleTheme: () => void;
	addPromptToHistory: (prompt: string) => void;
	removePromptFromHistory: (prompt: string) => void;
	pinPrompt: (prompt: string) => void;
	unpinPrompt: (prompt: string) => void;
	isPromptPinned: (prompt: string) => boolean;
	setPreferredIde: (ide: PreferredIde) => void;
	setChatWidthMode: (mode: ChatWidthMode) => void;
	setDefaultModel: (modelId: string) => void;
	setRecentThreadsVisibleLimit: (value: number) => void;
	setSidebarRecentOpen: (value: boolean) => void;
	setSidebarAllOpen: (value: boolean) => void;
	setSidebarAllGroupedByWorkspace: (value: boolean) => void;
	setShowRefreshButton: (value: boolean) => void;
};

export type AppEnvironment = {
	apiBase: string;
	runtime: DesktopRuntimeKind;
	isDesktop: boolean;
	supportsNativeWindowControls: boolean;
	supportsAppUpdates: boolean;
	windowControlsSide: WindowControlsSide;
	windowControls: string[];
};

export type AppSessions = {
	sessions: Session[];
	list: SessionSummary[];
	recentThreads: RecentThreadSummary[];
	selectedId: string | null;
	pendingId: string;
	awaitingInitialStatusId: string | null;
	selected: SessionSummary | null;
	peek: (sessionId: string) => Session | null;
	sessionContexts: Map<string, SessionContextValue>;
	select: (sessionId: string) => void;
	openThread: (sessionId: string, threadId: string) => void;
	createThread: (sessionId: string) => Promise<string | null>;
	startNew: () => void;
	setAwaitingInitialStatus: (sessionId: string | null) => void;
	refresh: () => Promise<void>;
	reloadSession: (sessionId: string) => Promise<void>;
	create: (workspaceId?: string) => Promise<string | null>;
	rename: (sessionId: string, nextName: string) => Promise<boolean>;
	remove: (sessionId: string) => Promise<boolean>;
	removeFromMemory: (sessionId: string) => boolean;
	takeRequestedThreadId: (sessionId: string) => string | null;
};

export type AppWorkspaces = {
	list: Workspace[];
	status: AsyncStatus;
	peek: (workspaceId: string) => Workspace | null;
	ensure: (workspaceId: string) => Workspace | null;
	refresh: () => Promise<void>;
	reloadWorkspace: (workspaceId: string) => Promise<void>;
	validate: (
		path: string,
		sourceType: "local" | "git",
	) => Promise<WorkspaceValidationResult>;
	create: (data: CreateWorkspaceRequest) => Promise<Workspace>;
	update: (
		workspaceId: string,
		data: { path?: string; displayName?: string | null },
	) => Promise<Workspace>;
	remove: (workspaceId: string, deleteFiles?: boolean) => Promise<void>;
};

export type AppStartupStatus = {
	tasks: StartupTask[];
	visibleTasks: StartupTask[];
	hasActiveTasks: boolean;
	peek: (taskId: string) => StartupTask | null;
	ensure: (taskId: string) => StartupTask | null;
	refresh: () => Promise<void>;
};

export type AppChatRequest = Omit<StartChatRequest, "sessionId"> & {
	sessionId?: string | null;
	workspaceType?: CreateWorkspaceRequest["sourceType"] | null;
	workspacePath?: string | null;
};

export type AppModels = {
	list: ModelInfo[];
	peek: (modelId: string) => ModelInfo | null;
	ensure: (modelId: string) => ModelInfo | null;
	refresh: () => Promise<void>;
};

export type AppCredentials = {
	list: CredentialInfo[];
	credentialTypes: CredentialType[];
	peek: (idOrProvider: string) => CredentialInfo | null;
	ensure: (idOrProvider: string) => CredentialInfo | null;
	refresh: () => Promise<void>;
	create: (data: {
		provider?: string;
		credentialId?: string;
		name: string;
		description?: string;
		authType: CredentialAuthType;
		apiKey?: string;
		envVars?: CredentialEnvVar[];
		visibility?: import("$lib/api-types").CredentialVisibility;
		inactive?: boolean;
	}) => Promise<CredentialInfo>;
	update: (data: {
		provider?: string;
		credentialId?: string;
		name: string;
		description?: string;
		authType: CredentialAuthType;
		apiKey?: string;
		envVars?: CredentialEnvVar[];
		visibility?: import("$lib/api-types").CredentialVisibility;
		inactive?: boolean;
	}) => Promise<CredentialInfo>;
	remove: (identifier: string) => Promise<void>;
	refreshCredential: (provider: string) => Promise<OAuthRefreshResponse>;
	anthropicAuthorize: () => Promise<OAuthAuthorizeResponse>;
	anthropicExchange: (
		data: OAuthExchangeRequest,
	) => Promise<OAuthExchangeResponse>;
	githubAuthorize: (
		data?: GitHubAuthorizeRequest,
	) => Promise<GitHubAuthorizeResponse>;
	githubDeviceCode: (
		data?: GitHubDeviceCodeRequest,
	) => Promise<GitHubDeviceCodeResponse>;
	githubPoll: (data: GitHubPollRequest) => Promise<GitHubPollResponse>;
	githubExchange: (
		data: GitHubExchangeRequest,
	) => Promise<GitHubExchangeResponse>;
	githubCallbackStatus: (
		data: GitHubCallbackStatusRequest,
	) => Promise<GitHubCallbackStatusResponse>;
	codexAuthorize: () => Promise<CodexAuthorizeResponse>;
	codexExchange: (data: CodexExchangeRequest) => Promise<CodexExchangeResponse>;
	codexDeviceCode: () => Promise<CodexDeviceCodeResponse>;
	codexPoll: (data: CodexPollRequest) => Promise<CodexPollResponse>;
	codexCallbackStatus: (
		data: CodexCallbackStatusRequest,
	) => Promise<CodexCallbackStatusResponse>;
};

export type AppSupportInfo = {
	data: SupportInfoResponse | null;
	status: AsyncStatus;
	error: string | null;
	fetch: () => Promise<void>;
};

export type AppUpdates = {
	status: UpdateStatus;
	availableVersion: string | null;
	error: string | null;
	downloadedBytes: number;
	totalBytes: number | null;
	isIgnored: boolean;
	showBadge: boolean;
	canTrackPrereleases: boolean;
	trackPrereleases: boolean;
	check: () => Promise<void>;
	installAndRelaunch: () => Promise<void>;
	ignore: () => void;
	setTrackPrereleases: (value: boolean) => Promise<void>;
};

export type AppStores = {
	sessions: SessionStore;
	recentThreads: RecentThreadStore;
	uiState: UIStateStore;
	workspaces: WorkspaceStore;
	models: ModelStore;
	credentials: CredentialStore;
	startup: StartupTaskStore;
};

export type AppContext = {
	stores: AppStores;
	ui: AppUI;
	preferences: AppPreferences;
	environment: AppEnvironment;
	sessions: AppSessions;
	workspaces: AppWorkspaces;
	startup: AppStartupStatus;
	models: AppModels;
	credentials: AppCredentials;
	supportInfo: AppSupportInfo;
	chatStreams: ChatStreamManager;
	chat: (data: AppChatRequest) => Promise<StartChatResponse>;
	refresh: () => Promise<void>;
	connectProjectEvents: () => () => void;
	updates: AppUpdates;
};
