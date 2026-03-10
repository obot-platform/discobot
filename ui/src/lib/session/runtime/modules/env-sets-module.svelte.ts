import type { EnvSetWithVars, SessionData } from "$lib/shell-types";
import type { Getter, Setter } from "$lib/session/runtime/modules/module-context";
import type {
	SessionEnvSetsModule,
	ThreadEnvSetsModule,
} from "$lib/session/runtime/session-runtime.types";

type SessionEnvSetsModuleArgs = {
	getSessionDataById: Getter<Record<string, SessionData>>;
	setSessionDataById: Setter<Record<string, SessionData>>;
	getList: Getter<EnvSetWithVars[]>;
	setList: Setter<EnvSetWithVars[]>;
	createEnvSetId: () => string;
	nowIsoString: () => string;
};

type ThreadEnvSetsModuleArgs = {
	getSessionId: Getter<string | null>;
	getSessionDataById: Getter<Record<string, SessionData>>;
	setSessionDataById: Setter<Record<string, SessionData>>;
	getList: Getter<EnvSetWithVars[]>;
};

export function createSessionEnvSetsModule(
	args: SessionEnvSetsModuleArgs,
): SessionEnvSetsModule {
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

export function createThreadEnvSetsModule(args: ThreadEnvSetsModuleArgs): ThreadEnvSetsModule {
	const getActiveIds = () => {
		const sessionId = args.getSessionId();
		if (!sessionId) {
			return [];
		}
		return args.getSessionDataById()[sessionId]?.activeEnvSetIds ?? [];
	};

	const getActive = () => {
		const activeIds = getActiveIds();
		const list = args.getList();
		return activeIds
			.map((id) => list.find((envSet) => envSet.id === id))
			.filter((envSet) => !!envSet);
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
			return getActive();
		},
		toggle,
	};
}
