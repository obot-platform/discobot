import type { QueryClient } from "@tanstack/svelte-query";

import type {
	ChatMessage,
	Session,
	SessionDiffFileEntry,
	SessionDiffStats,
} from "$lib/api-types";
import type { SessionQueryCache } from "$lib/session/cache/query-cache.svelte";
import type {
	SessionEnvSetsService,
	SessionHooksService,
	SessionThreadsService,
	ThreadEnvSetsService,
} from "$lib/session/services";
import type { SessionViewState } from "$lib/session/view/create-session-view-state.svelte";
import type {
	AsyncStatus,
	EnvSetWithVars,
	PlanEntry,
	ServiceItem,
	ThreadSummary,
} from "$lib/shell-types";

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
	submit: (payload: {
		text: string;
		mode: "build" | "plan";
		modelId: string | null;
		reasoning: boolean;
	}) => Promise<void>;
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
	queryClient: QueryClient;
	cache: SessionQueryCache;
	ui: SessionViewState;
	threads: SessionThreadsService;
	envSets: SessionEnvSetsDomain;
	hooks: SessionHooksService;
	files: SessionFilesDomain;
	services: SessionServicesDomain;
	threadContexts: Map<string, ThreadContextValue>;
	updateCurrent: (updater: (session: Session) => Session) => void;
	dispose: () => void;
};
