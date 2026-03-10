import { getContext, hasContext, setContext } from "svelte";

import type { ThreadRuntime } from "$lib/session/runtime/session-runtime.types";

const THREAD_CONTEXT_KEY = Symbol.for("discobot-ui-thread-context");

type ThreadContextGetter = () => ThreadRuntime;

export function setThreadContext(getThreadRuntime: ThreadContextGetter): ThreadRuntime {
	setContext(THREAD_CONTEXT_KEY, getThreadRuntime);
	return getThreadRuntime();
}

export function useThreadContext(): ThreadRuntime {
	const context = getContext<ThreadContextGetter | undefined>(THREAD_CONTEXT_KEY);
	if (!context) {
		throw new Error("useThreadContext must be used within ThreadContext provider");
	}
	return context();
}

export function getThreadContextIfPresent(): ThreadRuntime | undefined {
	if (!hasContext(THREAD_CONTEXT_KEY)) {
		return undefined;
	}
	const context = getContext<ThreadContextGetter | undefined>(THREAD_CONTEXT_KEY);
	return context?.();
}
