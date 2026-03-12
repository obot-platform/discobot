import { getSessionContextIfPresent, useSessionContext } from "$lib/context/session-context.svelte";
import { getPlanEntries } from "$lib/session/domains/session-domain.helpers";
import type { ThreadContextValue } from "$lib/session/session-context.types";

function toThreadContextValue(): ThreadContextValue {
	const session = useSessionContext();
	return {
		get threadId() {
			return session.threads.selectedId;
		},
		get thread() {
			return session.threads.selected;
		},
		get conversation() {
			return session.conversation.messages;
		},
		get planEntries() {
			return getPlanEntries(session.conversation.messages);
		},
		get editorFiles() {
			return session.files.list;
		},
		get fileContents() {
			return session.files.contents;
		},
		get activeEnvSetIds() {
			return session.envSets.activeIds;
		},
		get activeEnvSets() {
			return session.envSets.active;
		},
		envSets: {
			get activeIds() {
				return session.envSets.activeIds;
			},
			get active() {
				return session.envSets.active;
			},
			toggle: session.envSets.toggle,
		},
	};
}

export function useThreadContext(): ThreadContextValue {
	return toThreadContextValue();
}

export function getThreadContextIfPresent(): ThreadContextValue | undefined {
	if (!getSessionContextIfPresent()) {
		return undefined;
	}
	return toThreadContextValue();
}
