import type { EnvSetWithVars, SessionData } from "$lib/shell-types";

export function getFirstThreadId(session: SessionData | null): string | null {
	return session?.threads[0]?.id ?? null;
}

export function getFirstFilePath(session: SessionData | null): string {
	return session?.editorFiles[0] ?? "";
}

export function getHooksStatus(session: SessionData | null): NonNullable<SessionData["hooksStatus"]> {
	return (
		session?.hooksStatus ?? {
			hooks: [],
			pendingHookIds: [],
		}
	);
}

export function getHookOutputById(session: SessionData | null): Record<string, string> {
	return session?.hookOutputById ?? {};
}

export function getConversation(session: SessionData | null): NonNullable<SessionData["conversation"]> {
	return session?.conversation ?? [];
}

export function getPlanEntries(session: SessionData | null): NonNullable<SessionData["planEntries"]> {
	return session?.planEntries ?? [];
}

export function getActiveEnvSetIds(session: SessionData | null): string[] {
	return session?.activeEnvSetIds ?? [];
}

export function getActiveEnvSets(
	envSets: EnvSetWithVars[],
	activeIds: string[],
): EnvSetWithVars[] {
	return activeIds
		.map((id) => envSets.find((envSet) => envSet.id === id))
		.filter((envSet): envSet is EnvSetWithVars => !!envSet);
}
