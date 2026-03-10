import { getContext, hasContext, setContext } from "svelte";

import { createSessionRuntime } from "$lib/session/runtime/create-session-runtime.svelte";
import type {
	SessionRuntime,
	SessionRuntimeBundle,
	SessionRuntimeBootstrap,
} from "$lib/session/runtime/session-runtime.types";

const SESSION_CONTEXT_KEY = Symbol.for("discobot-ui-session-context");

export type SessionContextBootstrap = SessionRuntimeBootstrap;

export function setSessionContext(bootstrap: SessionContextBootstrap): SessionRuntimeBundle {
	const runtimeBundle = createSessionRuntime(bootstrap);
	setContext(SESSION_CONTEXT_KEY, runtimeBundle.session);
	return runtimeBundle;
}

export function useSessionContext(): SessionRuntime {
	const context = getContext<SessionRuntime | undefined>(SESSION_CONTEXT_KEY);
	if (!context) {
		throw new Error("useSessionContext must be used within SessionContext provider");
	}
	return context;
}

export function getSessionContextIfPresent(): SessionRuntime | undefined {
	if (!hasContext(SESSION_CONTEXT_KEY)) {
		return undefined;
	}
	return getContext<SessionRuntime | undefined>(SESSION_CONTEXT_KEY);
}
