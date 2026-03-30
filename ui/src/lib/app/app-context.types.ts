import type { SessionStore } from "$lib/store/sessions.store.svelte";
import type { WorkspaceStore } from "$lib/store/workspaces.store.svelte";
import type { ModelStore } from "$lib/store/models.store.svelte";
import type { CredentialStore } from "$lib/store/credentials.store.svelte";
import type { StartupTaskStore } from "$lib/store/startup-tasks.store.svelte";
import type {
	ModelInfo,
	CodexAuthorizeResponse,
	CodexExchangeRequest,
	CodexExchangeResponse,
	CreateWorkspaceRequest,
	CredentialInfo,
	CredentialType,
	GitHubDeviceCodeRequest,
	GitHubDeviceCodeResponse,
	GitHubPollRequest,
	GitHubPollResponse,
	OAuthAuthorizeResponse,
	OAuthExchangeRequest,
	OAuthExchangeResponse,
	OAuthRefreshResponse,
	CredentialAuthType,
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
import type {
	AsyncStatus,
	IdeOption,
	PreferredIde,
	RecentThreadSummary,
	SessionSummary,
	WindowControlsSide,
} from "$lib/shell-types";
import type { ThemeMetadata, ThemeMode, ResolvedTheme } from "$lib/theme";

export type ChatWidthMode = "full" | "constrained";

export type SettingsDialogTab =
	| "appearance"
	| "chat"
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
	windowControls: string[];
};

export type AppUI = {
	credentialFlowIntent: "github-git" | null;
	supportInfoDialogOpen: boolean;
	settingsDialog: {
		open: boolean;
		tab: SettingsDialogTab;
	};
	openSettings: (tab?: SettingsDialogTab) => void;
	closeSettings: () => void;
	openGitHubCredentialFlow: () => void;
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
	sidebarRecentOpen: boolean;
	sidebarAllOpen: boolean;
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
	setSidebarRecentOpen: (value: boolean) => void;
	setSidebarAllOpen: (value: boolean) => void;
};

export type AppEnvironment = {
	apiBase: string;
	isTauri: boolean;
	windowControlsSide: WindowControlsSide;
	windowControls: string[];
};

export type AppSessions = {
	sessions: Session[];
	list: SessionSummary[];
	recentThreads: RecentThreadSummary[];
	selectedId: string | null;
	pendingId: string;
	selected: SessionSummary | null;
	sessionContexts: Map<string, SessionContextValue>;
	select: (sessionId: string) => void;
	openThread: (sessionId: string, threadId: string) => void;
	startNew: () => void;
	refresh: () => Promise<void>;
	reloadSession: (sessionId: string) => Promise<void>;
	create: (workspaceId?: string) => Promise<string | null>;
	rename: (sessionId: string, nextName: string) => Promise<boolean>;
	remove: (sessionId: string) => Promise<boolean>;
	removeFromMemory: (sessionId: string) => boolean;
	recordRecentThread: (payload: {
		sessionId: string;
		sessionName: string;
		threadId: string;
		threadName: string;
		state?: import("$lib/api-types").ThreadState;
		lastMessage: string;
	}) => void;
	refreshRecentThread: (payload: {
		sessionId: string;
		sessionName: string;
		threadId: string;
		threadName: string;
		state?: import("$lib/api-types").ThreadState;
		lastMessage: string;
	}) => void;
	removeRecentThread: (sessionId: string, threadId: string) => void;
	reconcileRecentThreadsForSession: (
		sessionId: string,
		threadIds: string[],
	) => void;
	takeRequestedThreadId: (sessionId: string) => string | null;
};

export type AppWorkspaces = {
	list: Workspace[];
	status: AsyncStatus;
	get: (workspaceId: string) => Workspace | null;
	refresh: () => Promise<void>;
	reloadWorkspace: (workspaceId: string) => Promise<void>;
	validate: (
		path: string,
		sourceType: "local" | "git",
	) => Promise<WorkspaceValidationResult>;
	create: (data: CreateWorkspaceRequest) => Promise<Workspace>;
};

export type AppStartupStatus = {
	tasks: StartupTask[];
	visibleTasks: StartupTask[];
	hasActiveTasks: boolean;
	refresh: () => Promise<void>;
};

export type AppChatRequest = Omit<StartChatRequest, "sessionId"> & {
	sessionId?: string | null;
	workspaceType?: CreateWorkspaceRequest["sourceType"] | null;
	workspacePath?: string | null;
};

export type AppModels = {
	list: ModelInfo[];
	refresh: () => Promise<void>;
};

export type AppCredentials = {
	list: CredentialInfo[];
	credentialTypes: CredentialType[];
	get: (idOrProvider: string) => CredentialInfo | null;
	refresh: () => Promise<void>;
	create: (
		provider: string,
		authType: CredentialAuthType,
		apiKey: string,
	) => Promise<CredentialInfo>;
	update: (
		provider: string,
		authType: CredentialAuthType,
		apiKey: string,
	) => Promise<CredentialInfo>;
	remove: (provider: string) => Promise<void>;
	refreshCredential: (provider: string) => Promise<OAuthRefreshResponse>;
	anthropicAuthorize: () => Promise<OAuthAuthorizeResponse>;
	anthropicExchange: (
		data: OAuthExchangeRequest,
	) => Promise<OAuthExchangeResponse>;
	githubDeviceCode: (
		data?: GitHubDeviceCodeRequest,
	) => Promise<GitHubDeviceCodeResponse>;
	githubPoll: (data: GitHubPollRequest) => Promise<GitHubPollResponse>;
	codexAuthorize: () => Promise<CodexAuthorizeResponse>;
	codexExchange: (data: CodexExchangeRequest) => Promise<CodexExchangeResponse>;
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
	check: () => Promise<void>;
	installAndRelaunch: () => Promise<void>;
	ignore: () => void;
};

export type AppStores = {
	sessions: SessionStore;
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
	chat: (data: AppChatRequest) => Promise<StartChatResponse>;
	refresh: () => Promise<void>;
	connectProjectEvents: () => () => void;
	updates: AppUpdates;
};
