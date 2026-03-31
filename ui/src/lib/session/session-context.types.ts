import type {
	ChatMessage,
	FileStatus,
	Session,
	SessionDiffFileEntry,
	SessionDiffStats,
	Thread,
} from "$lib/api-types";
import type { EnvSetStore } from "$lib/store/env-sets.store.svelte";
import type { ThreadStore } from "$lib/store/threads.store.svelte";
import type { SessionViewState } from "$lib/session/view/create-session-view-state.svelte";
import type {
	AsyncStatus,
	EnvSetWithVars,
	HooksStatus,
	PlanEntry,
	ServiceItem,
	ThreadSummary,
} from "$lib/shell-types";

export type SessionStores = {
	threads: ThreadStore;
	envSets: EnvSetStore;
};

export type SessionEnvSetsService = {
	list: EnvSetWithVars[];
	create: (name: string, envVars: Record<string, string>) => void;
	update: (
		envSetId: string,
		name: string,
		envVars: Record<string, string>,
	) => void;
	remove: (envSetId: string) => void;
};

export type ThreadEnvSetsService = {
	activeIds: string[];
	active: EnvSetWithVars[];
	toggle: (envSetId: string) => void;
};

export type SessionHooksService = {
	status: HooksStatus;
	outputById: Record<string, string>;
	rerun: (hookId: string) => void;
	refresh: () => Promise<void>;
};

export type SessionThreadsService = {
	list: Thread[];
	status: AsyncStatus;
	selectedId: string | null;
	selected: Thread | null;
	load: () => Promise<void>;
	select: (threadId: string) => void;
	create: (name?: string) => void;
	rename: (threadId: string, nextName: string) => void;
	remove: (threadId: string) => void;
	refreshThread: (threadId: string) => Promise<void>;
};

export type SessionFileTreeNode = {
	name: string;
	path: string;
	type: "file" | "directory";
	size?: number;
	changed?: boolean;
	status?: FileStatus;
	children?: SessionFileTreeNode[];
};

export type SessionFileRecord = {
	path: string;
	content: string;
	encoding: "utf8" | "base64";
	size: number;
	fromBase: boolean;
};

export type SessionFileBufferState = {
	content: string;
	originalContent: string;
	encoding: "utf8" | "base64";
	isDirty: boolean;
	isSaving: boolean;
	saveError: string | null;
	hasConflict: boolean;
	conflictContent: string | null;
	fromBase: boolean;
};

export type SessionFilesDomain = {
	list: string[];
	searchable: string[];
	diff: SessionDiffFileEntry[];
	diffStats: SessionDiffStats;
	contents: Record<string, string>;
	selected: string;
	activePath: string;
	openPaths: string[];
	tree: SessionFileTreeNode[];
	showChangedOnly: boolean;
	expandedPaths: string[];
	getRecord: (path: string) => SessionFileRecord | null;
	getBuffer: (path: string) => SessionFileBufferState | null;
	isPathLoading: (path: string) => boolean;
	hasDirtyChanges: (path: string) => boolean;
	open: (file?: string) => Promise<void>;
	close: (file: string) => void;
	refresh: () => Promise<void>;
	toggleChangedOnly: () => Promise<void>;
	toggleDirectory: (path: string) => Promise<void>;
	expandAll: () => Promise<void>;
	collapseAll: () => void;
	rename: (path: string, nextName: string) => Promise<boolean>;
	remove: (path: string) => Promise<boolean>;
	updateBuffer: (path: string, content: string) => void;
	discard: (path: string) => void;
	save: (path: string) => Promise<boolean>;
	acceptConflict: (path: string) => void;
	forceSave: (path: string) => Promise<boolean>;
	getEditorModel: (path: string) => unknown | null;
	setEditorModel: (path: string, model: unknown | null) => void;
	getEditorViewState: (path: string) => unknown | null;
	setEditorViewState: (path: string, viewState: unknown | null) => void;
	dispose: () => void;
};

export type SessionServicesDomain = {
	list: ServiceItem[];
	active: ServiceItem | null;
	open: (serviceId: string) => void;
	start: (serviceId: string) => Promise<void>;
	stop: (serviceId: string) => Promise<void>;
	refresh: () => Promise<void>;
};

export type SessionEnvSetsDomain = SessionEnvSetsService & {
	activeIds: string[];
	active: EnvSetWithVars[];
	toggle: (envSetId: string) => void;
	refresh: () => Promise<void>;
};

export type SessionConversationDomain = {
	messages: ChatMessage[];
	status: AsyncStatus | "streaming";
	error: string | null;
	hasPendingQuestion: boolean;
	pendingQuestionId: string | null;
	cancel: () => Promise<void>;
	refresh: () => Promise<void>;
};

export type ThreadSubmitResult = {
	sessionId: string;
	threadId: string;
	materialized: boolean;
};

export type ThreadContextValue = {
	threadId: string;
	thread: ThreadSummary | null;
	mode: "build" | "plan";
	modelId: string | null;
	reasoning: string | undefined;
	nextMode: "build" | "plan" | undefined;
	nextModelId: string | null | undefined;
	nextReasoning: string | undefined;
	setNextMode: (mode: "build" | "plan" | undefined) => void;
	setNextModelId: (modelId: string | null | undefined) => void;
	setNextReasoning: (reasoning: string | undefined) => void;
	clearNextComposerValues: () => void;
	messages: ChatMessage[];
	planEntries: PlanEntry[];
	status: AsyncStatus | "streaming";
	error: string | null;
	hasPendingQuestion: boolean;
	pendingQuestionId: string | null;
	clearComposerDraft: (storageKey?: string) => void;
	submit: (payload: {
		parts: ChatMessage["parts"];
		workspaceId?: string;
		workspaceType?: "local" | "git" | null;
		workspacePath?: string | null;
		allowEmptyPendingMessage?: boolean;
	}) => Promise<ThreadSubmitResult | void>;
	cancel: () => Promise<void>;
	load: () => Promise<void>;
	refresh: () => Promise<void>;
	addToolApprovalResponse: (payload: {
		id: string;
		approved: boolean;
		reason?: string;
	}) => void;
	dispose: () => void;
	editorFiles: string[];
	fileContents: Record<string, string>;
	activeEnvSetIds: string[];
	activeEnvSets: EnvSetWithVars[];
	envSets: ThreadEnvSetsService;
};

export type SessionContextValue = {
	sessionId: string;
	isPending: boolean;
	current: Session | null;
	load: () => Promise<void>;
	dispose: () => void;
	stores: SessionStores;
	ui: SessionViewState;
	threads: SessionThreadsService;
	envSets: SessionEnvSetsDomain;
	hooks: SessionHooksService;
	files: SessionFilesDomain;
	services: SessionServicesDomain;
	threadContexts: Map<string, ThreadContextValue>;
};
