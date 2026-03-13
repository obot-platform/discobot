import { getContext, hasContext, setContext } from "svelte";

import { api } from "$lib/api-client";
import type {
	AppChatRequest,
	AppContext,
	AppContextBootstrap,
} from "$lib/app/app-context.types";
import { createAppCredentialsDomain } from "$lib/app/domains/app-credentials.svelte";
import { createAppEnvironmentDomain } from "$lib/app/domains/app-environment";
import { createAppModelsDomain } from "$lib/app/domains/app-models.svelte";
import { createAppPreferencesDomain } from "$lib/app/domains/app-preferences.svelte";
import { createAppSessionsDomain } from "$lib/app/domains/app-sessions.svelte";
import { createAppSupportInfoDomain } from "$lib/app/domains/app-support-info.svelte";
import { createAppUpdatesDomain } from "$lib/app/domains/app-updates.svelte";
import { createAppWorkspacesDomain } from "$lib/app/domains/app-workspaces.svelte";
import { createAppViewState } from "$lib/app/view/create-app-view-state.svelte";
import { getQueryClient } from "$lib/query/query-client";

export type {
	AppContext,
	AppContextBootstrap,
	AppCredential,
	ChatWidthMode,
	SettingsDialogTab,
	UpdateStatus,
} from "$lib/app/app-context.types";

const APP_CONTEXT_KEY = Symbol.for("discobot-ui-app-context");

function createAppContext(bootstrap: AppContextBootstrap): AppContext {
	const queryClient = getQueryClient();

	const ui = createAppViewState();
	const preferences = createAppPreferencesDomain({ bootstrap });
	const environment = createAppEnvironmentDomain({ bootstrap });
	const updates = createAppUpdatesDomain();
	const workspaces = createAppWorkspacesDomain({ queryClient });
	const models = createAppModelsDomain();
	const supportInfo = createAppSupportInfoDomain();
	const credentials = createAppCredentialsDomain({ queryClient });
	const sessions = createAppSessionsDomain({
		queryClient,
		initialSelectedSessionId: bootstrap.selectedSessionId,
	});

	void sessions.refresh();
	void workspaces.refresh();
	void models.refresh();

	const findWorkspaceBySourceAndPath = (path: string, sourceType: "local" | "git") => {
		const normalizedPath = path.trim();
		if (!normalizedPath) {
			return null;
		}

		return workspaces.list.find((workspace) => {
			return workspace.sourceType === sourceType && workspace.path.trim() === normalizedPath;
		}) ?? null;
	};

	const chat = async ({
		sessionId,
		threadId,
		workspaceId,
		workspaceType,
		workspacePath,
		...rest
	}: AppChatRequest) => {
		const resolvedSessionId = sessionId ?? sessions.pendingId;
		let resolvedWorkspaceId = workspaceId ?? undefined;

		if (!resolvedWorkspaceId && resolvedSessionId === sessions.pendingId && workspaceType && workspacePath) {
			const normalizedWorkspacePath = workspacePath.trim();
			if (normalizedWorkspacePath) {
				let workspace = findWorkspaceBySourceAndPath(normalizedWorkspacePath, workspaceType);
				if (!workspace) {
					await workspaces.refresh();
					workspace = findWorkspaceBySourceAndPath(normalizedWorkspacePath, workspaceType);
				}

				if (!workspace) {
					workspace = await workspaces.create({
						path: normalizedWorkspacePath,
						sourceType: workspaceType,
					});
				}

				resolvedWorkspaceId = workspace.id;
			}
		}

		const response = await api.startChat({
			...rest,
			sessionId: resolvedSessionId,
			...(threadId ? { threadId } : {}),
			...(resolvedWorkspaceId ? { workspaceId: resolvedWorkspaceId } : {}),
		});

		await sessions.refresh();
		return response;
	};

	return {
		ui,
		preferences,
		environment,
		sessions,
		workspaces,
		models,
		credentials,
		supportInfo,
		chat,
		updates,
	};
}

export function setAppContext(bootstrap: AppContextBootstrap): AppContext {
	const context = createAppContext(bootstrap);
	setContext(APP_CONTEXT_KEY, context);
	return context;
}

export function useAppContext(): AppContext {
	const context = getContext<AppContext | undefined>(APP_CONTEXT_KEY);
	if (!context) {
		throw new Error("useAppContext must be used within AppContext provider");
	}
	return context;
}

export function getAppContextIfPresent(): AppContext | undefined {
	if (!hasContext(APP_CONTEXT_KEY)) {
		return undefined;
	}
	return getContext<AppContext | undefined>(APP_CONTEXT_KEY);
}
