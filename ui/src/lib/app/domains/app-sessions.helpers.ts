import type { Session } from "$lib/api-types";
import type { SessionSummary } from "$lib/shell-types";

export function upsertSession(sessions: Session[], nextSession: Session): Session[] {
	const existingIndex = sessions.findIndex((session) => session.id === nextSession.id);
	if (existingIndex === -1) {
		return [...sessions, nextSession];
	}

	return sessions.map((session, index) =>
		index === existingIndex ? nextSession : session,
	);
}
