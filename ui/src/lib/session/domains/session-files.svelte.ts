import { api } from "$lib/api-client";
import type { SessionDiffFileEntry, SessionDiffStats } from "$lib/api-types";
import type { SessionFilesDomain } from "$lib/session/session-context.types";

const EMPTY_DIFF_STATS: SessionDiffStats = {
	filesChanged: 0,
	additions: 0,
	deletions: 0,
};

type DiffState = { files: SessionDiffFileEntry[]; stats: SessionDiffStats };

type CreateSessionFilesDomainArgs = {
	sessionId: string;
	hasSession: () => boolean;
	getSelectedFile: () => string;
	openFile: (file?: string) => void;
};

function uniquePaths(paths: string[]): string[] {
	return [...new Set(paths.filter((path) => path.length > 0))];
}

export function createSessionFilesDomain(args: CreateSessionFilesDomainArgs): SessionFilesDomain {
	let openedPaths = $state<string[]>([]);
	let contents = $state<Record<string, string>>({});
	let diffData = $state<DiffState>({ files: [], stats: EMPTY_DIFF_STATS });
	let searchable = $state<string[]>([]);

	const diff = $derived(diffData.files);
	const diffStats = $derived(diffData.stats);
	const list = $derived(
		uniquePaths([...openedPaths, ...diff.map((file) => file.path), ...searchable.slice(0, 20)]),
	);

	function syncSelectedFile(nextList = list, nextSearchable = searchable) {
		const selectedFile = args.getSelectedFile();
		if (!selectedFile || nextList.includes(selectedFile)) {
			return;
		}

		args.openFile(nextList[0] ?? nextSearchable[0]);
	}

	async function loadFile(path: string) {
		if (!args.hasSession() || !path) {
			return;
		}
		if (contents[path] !== undefined) {
			return;
		}

		const diffEntry = diff.find((file) => file.path === path);
		const response = await api.readSessionFile(args.sessionId, path, {
			fromBase: diffEntry?.status === "deleted",
		});
		contents = {
			...contents,
			[path]: response.content,
		};
	}

	async function refresh() {
		openedPaths = [];
		contents = {};
		if (!args.hasSession()) {
			diffData = { files: [], stats: EMPTY_DIFF_STATS };
			searchable = [];
			syncSelectedFile([], []);
			return;
		}
		const [diffResponse, searchResponse] = await Promise.all([
			api.getSessionDiff(args.sessionId, { format: "files" }),
			api.searchSessionFiles(args.sessionId, "", 200),
		]);
		diffData =
			"files" in diffResponse && "stats" in diffResponse
				? (diffResponse as DiffState)
				: { files: [], stats: EMPTY_DIFF_STATS };
		searchable = searchResponse.results
			.filter((entry) => entry.type === "file")
			.map((entry) => entry.path);
		syncSelectedFile(
			uniquePaths([
				...openedPaths,
				...diffData.files.map((file) => file.path),
				...searchable.slice(0, 20),
			]),
			searchable,
		);
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
			if (list.length === 0 && searchable.length === 0) {
				await refresh();
			}
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
		refresh,
	};
}
