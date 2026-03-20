import type {
	ChatMessage,
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
	update: (envSetId: string, name: string, envVars: Record<string, string>) => void;
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

export type SessionFilesDomain = {
	list: string[];
	searchable: string[];
	diff: SessionDiffFileEntry[];
	diffStats: SessionDiffStats;
	contents: Record<string, string>;
	selected: string;
	open: (file?: string) => Promise<void>;
	refresh: () => Promise<void>;
};

export type SessionServicesDomain = {
	list: ServiceItem[];
	active: ServiceItem | null;
	open: (serviceId: string) => void;
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
	cancel: () => Promise<void>;
	refresh: () => Promise<void>;
};

export type ThreadContextValue = {
	threadId: string;
	thread: ThreadSummary | null;
	messages: ChatMessage[];
	planEntries: PlanEntry[];
	status: AsyncStatus | "streaming";
	error: string | null;
	submit: (payload: {
		text: string;
		mode: "build" | "plan";
		modelId: string | null;
		reasoning: boolean;
	}) => Promise<void>;
	cancel: () => Promise<void>;
	load: () => Promise<void>;
	refresh: () => Promise<void>;
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
