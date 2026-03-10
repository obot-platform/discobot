import type { QueryClient } from "@tanstack/svelte-query";

import type { CenterPanel, EnvSetWithVars, SessionData } from "$lib/shell-types";
import type { SessionQueryCache } from "$lib/session/cache/query-cache.svelte";

export type SessionRuntimeBootstrap = {
	sessionDataById: Record<string, SessionData>;
	envSets: EnvSetWithVars[];
	selectedSessionId?: string;
};

export type SessionUiModule = {
	centerPanel: CenterPanel;
	selectedFile: string;
	ideMenuOpen: boolean;
	composerDraft: string;
	openChat: () => void;
	openTerminal: () => void;
	openDesktop: () => void;
	openFiles: (file?: string) => void;
	openDiffReview: () => void;
	openService: (serviceId: string) => void;
	toggleIdeMenu: () => void;
	setComposerDraft: (value: string) => void;
};

export type SessionThreadsModule = {
	list: SessionData["threads"];
	selectedId: string | null;
	selected: SessionData["threads"][number] | null;
	select: (threadId: string) => void;
	create: (name?: string) => void;
	rename: (threadId: string, nextName: string) => void;
	remove: (threadId: string) => void;
};

export type SessionEnvSetsModule = {
	list: EnvSetWithVars[];
	create: (name: string, envVars: Record<string, string>) => void;
	update: (envSetId: string, name: string, envVars: Record<string, string>) => void;
	remove: (envSetId: string) => void;
};

export type ThreadEnvSetsModule = {
	activeIds: string[];
	active: EnvSetWithVars[];
	toggle: (envSetId: string) => void;
};

export type SessionHooksModule = {
	status: NonNullable<SessionData["hooksStatus"]>;
	outputById: Record<string, string>;
	rerun: (hookId: string) => void;
};

export type SessionRuntime = {
	sessionId: string | null;
	current: SessionData | null;
	files: string[];
	fileContents: Record<string, string>;
	services: SessionData["services"];
	suggestedPrompts: string[];
	activeService: SessionData["services"][number] | null;
	queryClient: QueryClient;
	cache: SessionQueryCache;
	threads: SessionThreadsModule;
	envSets: SessionEnvSetsModule;
	hooks: SessionHooksModule;
	dispose: () => void;
};

export type ThreadRuntime = {
	threadId: string | null;
	thread: SessionData["threads"][number] | null;
	conversation: NonNullable<SessionData["conversation"]>;
	planEntries: NonNullable<SessionData["planEntries"]>;
	editorFiles: string[];
	fileContents: Record<string, string>;
	activeEnvSetIds: string[];
	ui: SessionUiModule;
	envSets: ThreadEnvSetsModule;
};

export type SessionRuntimeBundle = {
	session: SessionRuntime;
	thread: ThreadRuntime;
};
