import type { SessionData } from "$lib/shell-types";
import type { Getter, Setter } from "$lib/session/runtime/modules/module-context";
import type { SessionThreadsModule } from "$lib/session/runtime/session-runtime.types";

type CreateSessionThreadsModuleArgs = {
	getSessionId: Getter<string | null>;
	getSessionDataById: Getter<Record<string, SessionData>>;
	setSessionDataById: Setter<Record<string, SessionData>>;
	getList: Getter<SessionData["threads"]>;
	getSelectedId: Getter<string | null>;
	setSelectedId: Setter<string | null>;
	createThreadId: () => string;
};

export function createSessionThreadsModule(
	args: CreateSessionThreadsModuleArgs,
): SessionThreadsModule {
	const select = (threadId: string) => {
		if (args.getList().some((thread) => thread.id === threadId)) {
			args.setSelectedId(threadId);
		}
	};

	const create = (name?: string) => {
		const sessionId = args.getSessionId();
		if (!sessionId) {
			return;
		}

		const sessionDataById = args.getSessionDataById();
		const currentSession = sessionDataById[sessionId];
		if (!currentSession) {
			return;
		}

		const threadId = args.createThreadId();
		const nextName = name?.trim() || `Thread ${currentSession.threads.length + 1}`;

		args.setSessionDataById({
			...sessionDataById,
			[sessionId]: {
				...currentSession,
				threads: [...currentSession.threads, { id: threadId, name: nextName }],
			},
		});

		args.setSelectedId(threadId);
	};

	const rename = (threadId: string, nextName: string) => {
		const trimmedName = nextName.trim();
		const sessionId = args.getSessionId();
		if (!trimmedName || !sessionId) {
			return;
		}

		const sessionDataById = args.getSessionDataById();
		const currentSession = sessionDataById[sessionId];
		if (!currentSession || !currentSession.threads.some((thread) => thread.id === threadId)) {
			return;
		}

		args.setSessionDataById({
			...sessionDataById,
			[sessionId]: {
				...currentSession,
				threads: currentSession.threads.map((thread) =>
					thread.id === threadId ? { ...thread, name: trimmedName } : thread,
				),
			},
		});
	};

	const remove = (threadId: string) => {
		const sessionId = args.getSessionId();
		if (!sessionId) {
			return;
		}

		const sessionDataById = args.getSessionDataById();
		const currentSession = sessionDataById[sessionId];
		if (!currentSession) {
			return;
		}

		const removedIndex = currentSession.threads.findIndex((thread) => thread.id === threadId);
		if (removedIndex === -1) {
			return;
		}

		const remainingThreads = currentSession.threads.filter((thread) => thread.id !== threadId);
		args.setSessionDataById({
			...sessionDataById,
			[sessionId]: {
				...currentSession,
				threads: remainingThreads,
			},
		});

		if (args.getSelectedId() !== threadId) {
			return;
		}

		args.setSelectedId(
			remainingThreads[removedIndex]?.id ?? remainingThreads[removedIndex - 1]?.id ?? null,
		);
	};

	return {
		get list() {
			return args.getList();
		},
		get selectedId() {
			return args.getSelectedId();
		},
		get selected() {
			return args.getList().find((thread) => thread.id === args.getSelectedId()) ?? null;
		},
		select,
		create,
		rename,
		remove,
	};
}
