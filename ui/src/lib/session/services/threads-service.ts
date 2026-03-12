import type { SessionData } from "$lib/shell-types";

export type SessionThreadsService = {
	list: SessionData["threads"];
	selectedId: string | null;
	selected: SessionData["threads"][number] | null;
	select: (threadId: string) => void;
	create: (name?: string) => void;
	rename: (threadId: string, nextName: string) => void;
	remove: (threadId: string) => void;
};

type CreateSessionThreadServiceArgs = {
	getSessionId: () => string | null;
	getSessionDataById: () => Record<string, SessionData>;
	setSessionDataById: (value: Record<string, SessionData>) => void;
	getList: () => SessionData["threads"];
	getSelectedId: () => string | null;
	setSelectedId: (value: string | null) => void;
	createThreadId: () => string;
};

export function getNextSelectedThreadId(
	threads: SessionData["threads"],
	removedThreadId: string,
	currentSelectedId: string | null,
): string | null {
	const removedIndex = threads.findIndex((thread) => thread.id === removedThreadId);
	if (removedIndex === -1) {
		return currentSelectedId;
	}

	const remainingThreads = threads.filter((thread) => thread.id !== removedThreadId);
	if (currentSelectedId !== removedThreadId) {
		return currentSelectedId;
	}

	return remainingThreads[removedIndex]?.id ?? remainingThreads[removedIndex - 1]?.id ?? null;
}

export function createSessionThreadService(
	args: CreateSessionThreadServiceArgs,
): SessionThreadsService {
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

		const threadId =
			currentSession.threads.length === 0 ? sessionId : args.createThreadId();
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

		const nextSelectedId = getNextSelectedThreadId(
			currentSession.threads,
			threadId,
			args.getSelectedId(),
		);
		if (nextSelectedId === args.getSelectedId() && !currentSession.threads.some((thread) => thread.id === threadId)) {
			return;
		}

		args.setSessionDataById({
			...sessionDataById,
			[sessionId]: {
				...currentSession,
				threads: currentSession.threads.filter((thread) => thread.id !== threadId),
			},
		});
		args.setSelectedId(nextSelectedId);
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
