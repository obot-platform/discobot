import type { EnvSetWithVars, SessionData } from "$lib/shell-types";
import { getActiveEnvSets } from "$lib/session/store/session-store.helpers";

export type SessionEnvSetsService = {
	list: EnvSetWithVars[];
	create: (name: string, envVars: Record<string, string>) => void;
	update: (envSetId: string, name: string, envVars: Record<string, string>) => void;
	remove: (envSetId: string) => void;
};

export type ThreadEnvSetsService = {
	activeIds: string[];
	active: EnvSetWithVars[];
	toggle: (envSetId: string) => void;
};

type SessionEnvSetsServiceArgs = {
	getSessionDataById: () => Record<string, SessionData>;
	setSessionDataById: (value: Record<string, SessionData>) => void;
	getList: () => EnvSetWithVars[];
	setList: (value: EnvSetWithVars[]) => void;
	createEnvSetId: () => string;
	nowIsoString: () => string;
};

type ThreadEnvSetsServiceArgs = {
	getSessionId: () => string | null;
	getSessionDataById: () => Record<string, SessionData>;
	setSessionDataById: (value: Record<string, SessionData>) => void;
	getList: () => EnvSetWithVars[];
};

export function createSessionEnvSetsService(
	args: SessionEnvSetsServiceArgs,
): SessionEnvSetsService {
	const create = (name: string, envVars: Record<string, string>) => {
		const trimmedName = name.trim();
		if (!trimmedName) {
			return;
		}

		const now = args.nowIsoString();
		args.setList([
			...args.getList(),
			{
				id: args.createEnvSetId(),
				projectId: "local",
				name: trimmedName,
				createdAt: now,
				updatedAt: now,
				envVars,
			},
		]);
	};

	const update = (envSetId: string, name: string, envVars: Record<string, string>) => {
		const trimmedName = name.trim();
		if (!trimmedName) {
			return;
		}

		const now = args.nowIsoString();
		args.setList(
			args.getList().map((envSet) =>
				envSet.id === envSetId
					? {
						...envSet,
						name: trimmedName,
						envVars,
						updatedAt: now,
					}
					: envSet,
			),
		);
	};

	const remove = (envSetId: string) => {
		const list = args.getList();
		if (!list.some((envSet) => envSet.id === envSetId)) {
			return;
		}

		args.setList(list.filter((envSet) => envSet.id !== envSetId));
		const sessionDataById = args.getSessionDataById();
		args.setSessionDataById(
			Object.fromEntries(
				Object.entries(sessionDataById).map(([sessionId, sessionData]) => [
					sessionId,
					{
						...sessionData,
						activeEnvSetIds: (sessionData.activeEnvSetIds ?? []).filter(
							(activeEnvSetId) => activeEnvSetId !== envSetId,
						),
					},
				]),
			),
		);
	};

	return {
		get list() {
			return args.getList();
		},
		create,
		update,
		remove,
	};
}

export function createThreadEnvSetsService(
	args: ThreadEnvSetsServiceArgs,
): ThreadEnvSetsService {
	const getActiveIds = () => {
		const sessionId = args.getSessionId();
		if (!sessionId) {
			return [];
		}
		return args.getSessionDataById()[sessionId]?.activeEnvSetIds ?? [];
	};

	const toggle = (envSetId: string) => {
		const sessionId = args.getSessionId();
		const list = args.getList();
		if (!sessionId || !list.some((envSet) => envSet.id === envSetId)) {
			return;
		}

		const sessionDataById = args.getSessionDataById();
		const currentSession = sessionDataById[sessionId];
		if (!currentSession) {
			return;
		}

		const currentIds = currentSession.activeEnvSetIds ?? [];
		const nextIds = currentIds.includes(envSetId)
			? currentIds.filter((id) => id !== envSetId)
			: [...currentIds, envSetId];

		args.setSessionDataById({
			...sessionDataById,
			[sessionId]: {
				...currentSession,
				activeEnvSetIds: nextIds,
			},
		});
	};

	return {
		get activeIds() {
			return getActiveIds();
		},
		get active() {
			return getActiveEnvSets(args.getList(), getActiveIds());
		},
		toggle,
	};
}
