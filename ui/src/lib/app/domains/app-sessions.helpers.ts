import type { SessionSummary } from "$lib/shell-types";

export function getNextSelectedSessionId(
	sessions: SessionSummary[],
	removedSessionId: string,
	currentSelectedId: string | null,
): string | null {
	const remainingSessions = sessions.filter((session) => session.id !== removedSessionId);
	if (currentSelectedId !== removedSessionId) {
		return currentSelectedId;
	}
	return remainingSessions[0]?.id ?? null;
}

export function getReconciledSelectedSessionId(
	sessions: SessionSummary[],
	currentSelectedId: string | null,
	preferredId?: string | null,
): string | null {
	if (preferredId && sessions.some((session) => session.id === preferredId)) {
		return preferredId;
	}
	if (currentSelectedId && sessions.some((session) => session.id === currentSelectedId)) {
		return currentSelectedId;
	}
	return null;
}
