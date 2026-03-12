import { createQuery, queryOptions } from "@tanstack/svelte-query";
import type { QueryClient } from "@tanstack/svelte-query";

import { api } from "$lib/api-client";
import type { Session, SessionDiffStats } from "$lib/api-types";
import type { SessionFilesDomain } from "$lib/session/session-context.types";

const FILES_DOMAIN = "files";

const EMPTY_DIFF_STATS: SessionDiffStats = {
	filesChanged: 0,
	additions: 0,
	deletions: 0,
};

type CreateSessionFilesDomainArgs = {
	queryClient: QueryClient;
	getSession: () => Session | null;
	key: (...parts: string[]) => readonly unknown[];
	getSelectedFile: () => string;
	openFile: (file?: string) => void;
};

function uniquePaths(paths: string[]): string[] {
	return [...new Set(paths.filter((path) => path.length > 0))];
}

export function createSessionFilesDomain(args: CreateSessionFilesDomainArgs): SessionFilesDomain {
	let openedPaths = $state<string[]>([]);
	let contents = $state<Record<string, string>>({});
	let previousSessionId = $state<string | null>(args.getSession()?.id ?? null);

	const diffQuery = createQuery(() => {
		const sessionId = args.getSession()?.id;
		return queryOptions({
			queryKey: args.key(FILES_DOMAIN, "diff"),
			queryFn: async () => {
				if (!sessionId) {
					return { files: [], stats: EMPTY_DIFF_STATS };
				}
				const response = await api.getSessionDiff(sessionId, { format: "files" });
				return "files" in response && "stats" in response
					? response
					: { files: [], stats: EMPTY_DIFF_STATS };
			},
			initialData: { files: [], stats: EMPTY_DIFF_STATS },
		});
	});

	const searchQuery = createQuery(() => {
		const sessionId = args.getSession()?.id;
		return queryOptions({
			queryKey: args.key(FILES_DOMAIN, "search", "all"),
			queryFn: async () => {
				if (!sessionId) {
					return [];
				}
				const response = await api.searchSessionFiles(sessionId, "", 200);
				return response.results
					.filter((entry) => entry.type === "file")
					.map((entry) => entry.path);
			},
			initialData: [],
		});
	});

	const searchable = $derived.by(() => searchQuery.data ?? []);
	const diff = $derived.by(() => diffQuery.data?.files ?? []);
	const diffStats = $derived.by(() => diffQuery.data?.stats ?? EMPTY_DIFF_STATS);
	const list = $derived.by(() =>
		uniquePaths([...openedPaths, ...diff.map((file) => file.path), ...searchable.slice(0, 20)]),
	);

	$effect(() => {
		const nextSessionId = args.getSession()?.id ?? null;
		if (nextSessionId === previousSessionId) {
			return;
		}
		previousSessionId = nextSessionId;
		openedPaths = [];
		contents = {};
	});

	$effect(() => {
		const selectedFile = args.getSelectedFile();
		if (!selectedFile || list.includes(selectedFile)) {
			return;
		}

		args.openFile(list[0] ?? searchable[0]);
	});

	async function loadFile(path: string) {
		const session = args.getSession();
		if (!session || !path) {
			return;
		}
		if (contents[path] !== undefined) {
			return;
		}

		const diffEntry = diff.find((file) => file.path === path);
		const response = await api.readSessionFile(session.id, path, {
			fromBase: diffEntry?.status === "deleted",
		});
		contents = {
			...contents,
			[path]: response.content,
		};
	}

	return {
		get list() {
			return list;
		},
		get searchable() {
			return searchable;
		},
		get diff() {
			return diff;
		},
		get diffStats() {
			return diffStats;
		},
		get contents() {
			return contents;
		},
		get selected() {
			return args.getSelectedFile();
		},
		open: async (file?: string) => {
			const nextFile = (file ?? args.getSelectedFile()) || list[0] || searchable[0] || "";
			if (!nextFile) {
				args.openFile();
				return;
			}
			if (!openedPaths.includes(nextFile)) {
				openedPaths = uniquePaths([...openedPaths, nextFile]);
			}
			args.openFile(nextFile);
			await loadFile(nextFile);
		},
		refresh: async () => {
			openedPaths = [];
			contents = {};
			await Promise.all([diffQuery.refetch(), searchQuery.refetch()]);
		},
	};
}
