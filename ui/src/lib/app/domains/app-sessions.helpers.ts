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
