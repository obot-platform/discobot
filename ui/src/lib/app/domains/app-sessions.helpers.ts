import type { Session } from "$lib/api-types";
import type { SessionSummary } from "$lib/shell-types";

export function getNextSelectedSessionId(
	sessions: SessionSummary[],
	removedSessionId: string,
	currentSelectedSessionId: string | null,
): string | null {
	if (currentSelectedSessionId !== removedSessionId) {
		return currentSelectedSessionId;
	}

	return sessions.find((session) => session.id !== removedSessionId)?.id ?? null;
}

export function getReconciledSelectedSessionId(
	sessions: SessionSummary[],
	currentSelectedSessionId: string | null,
	explicitSelectedSessionId?: string | null,
): string | null {
	if (explicitSelectedSessionId && sessions.some((session) => session.id === explicitSelectedSessionId)) {
		return explicitSelectedSessionId;
	}

	if (currentSelectedSessionId && sessions.some((session) => session.id === currentSelectedSessionId)) {
		return currentSelectedSessionId;
	}

	return null;
}

export function upsertSession(sessions: Session[], nextSession: Session): Session[] {
	const existingIndex = sessions.findIndex((session) => session.id === nextSession.id);
	if (existingIndex === -1) {
		return [...sessions, nextSession];
	}

	return sessions.map((session, index) =>
		index === existingIndex ? nextSession : session,
	);
}
