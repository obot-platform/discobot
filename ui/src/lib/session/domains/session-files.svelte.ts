import { SvelteMap, SvelteSet } from "svelte/reactivity";

import { api, FileConflictError } from "$lib/api-client";
import type {
	SessionDiffFileEntry,
	SessionDiffStats,
	SessionFileEntry,
} from "$lib/api-types";
import type {
	SessionFileBufferState,
	SessionFileRecord,
	SessionFilesDomain,
	SessionFileTreeNode,
} from "$lib/session/session-context.types";

const EMPTY_DIFF_STATS: SessionDiffStats = {
	filesChanged: 0,
	additions: 0,
	deletions: 0,
};

type DiffState = { files: SessionDiffFileEntry[]; stats: SessionDiffStats };

type EditorRuntimeState = {
	model: unknown | null;
	viewState: unknown | null;
};

type CreateSessionFilesDomainArgs = {
	sessionId: string;
	hasSession: () => boolean;
	getSelectedFile: () => string;
	openFile: (file?: string) => void;
};

function uniquePaths(paths: string[]): string[] {
	return paths.filter((path, index) => path.length > 0 && paths.indexOf(path) === index);
}

function hasChangedDescendant(
	dirPath: string,
	diffEntriesMap: SvelteMap<string, SessionDiffFileEntry>,
): boolean {
	const prefix = dirPath === "." ? "" : `${dirPath}/`;
	for (const path of diffEntriesMap.keys()) {
		if (path.startsWith(prefix)) return true;
	}
	return false;
}

function entriesToNodes(
	entries: SessionFileEntry[],
	parentPath: string,
	diffEntriesMap: SvelteMap<string, SessionDiffFileEntry>,
): SessionFileTreeNode[] {
	const existingPaths = new SvelteSet(
		entries.map((entry) =>
			parentPath === "." ? entry.name : `${parentPath}/${entry.name}`,
		),
	);

	const nodes: SessionFileTreeNode[] = entries.map((entry) => {
		const path = parentPath === "." ? entry.name : `${parentPath}/${entry.name}`;
		const isDirectory = entry.type === "directory";
		const diffEntry = diffEntriesMap.get(path);

		return {
			name: entry.name,
			path,
			type: entry.type,
			size: entry.size,
			children: isDirectory ? undefined : undefined,
			changed: isDirectory
				? hasChangedDescendant(path, diffEntriesMap)
				: diffEntry !== undefined,
			status: isDirectory ? undefined : diffEntry?.status,
		};
	});

	const addedPaths = new SvelteSet<string>();
	for (const [filePath, diffEntry] of diffEntriesMap) {
		if (diffEntry.status !== "deleted") continue;

		const parentDir = filePath.includes("/")
			? filePath.substring(0, filePath.lastIndexOf("/"))
			: ".";
		const isDirectChild = parentDir === parentPath;
		const isUnderParent = parentPath === "." ? true : filePath.startsWith(`${parentPath}/`);

		if (!isUnderParent) continue;

		if (isDirectChild) {
			if (existingPaths.has(filePath) || addedPaths.has(filePath)) continue;
			const name = filePath.split("/").at(-1) ?? filePath;
			nodes.push({
				name,
				path: filePath,
				type: "file",
				changed: true,
				status: "deleted",
			});
			addedPaths.add(filePath);
			continue;
		}

		const relativePath =
			parentPath === "." ? filePath : filePath.substring(parentPath.length + 1);
		const firstPart = relativePath.split("/")[0];
		const ghostDirPath = parentPath === "." ? firstPart : `${parentPath}/${firstPart}`;

		if (existingPaths.has(ghostDirPath) || addedPaths.has(ghostDirPath)) continue;

		nodes.push({
			name: firstPart,
			path: ghostDirPath,
			type: "directory",
			changed: true,
			children: undefined,
		});
		addedPaths.add(ghostDirPath);
	}

	return nodes.sort((a, b) => {
		if (a.type !== b.type) {
			return a.type === "directory" ? -1 : 1;
		}
		return a.name.localeCompare(b.name);
	});
}

function buildTreeFromCache(
	rootNodes: SessionFileTreeNode[],
	cache: SvelteMap<string, SessionFileTreeNode[]>,
	diffEntriesMap: SvelteMap<string, SessionDiffFileEntry>,
): SessionFileTreeNode[] {
	function attachChildren(node: SessionFileTreeNode): SessionFileTreeNode {
		if (node.type !== "directory") return node;

		const cachedChildren = cache.get(node.path);
		if (!cachedChildren) return node;

		return {
			...node,
			children: cachedChildren.map((child) => {
				const isDirectory = child.type === "directory";
				const diffEntry = diffEntriesMap.get(child.path);
				return {
					...attachChildren(child),
					changed: isDirectory
						? hasChangedDescendant(child.path, diffEntriesMap)
						: diffEntry !== undefined,
					status: isDirectory ? undefined : diffEntry?.status,
				};
			}),
		};
	}

	return rootNodes.map((node) => {
		const isDirectory = node.type === "directory";
		const diffEntry = diffEntriesMap.get(node.path);
		return {
			...attachChildren(node),
			changed: isDirectory
				? hasChangedDescendant(node.path, diffEntriesMap)
				: diffEntry !== undefined,
			status: isDirectory ? undefined : diffEntry?.status,
		};
	});
}

function buildTreeFromChangedFiles(diffEntries: SessionDiffFileEntry[]): SessionFileTreeNode[] {
	if (diffEntries.length === 0) return [];

	const statusMap = new SvelteMap<string, SessionDiffFileEntry["status"]>();
	for (const entry of diffEntries) {
		statusMap.set(entry.path, entry.status);
	}

	interface TreeNode {
		children: SvelteMap<string, TreeNode>;
		isFile: boolean;
	}

	const root: TreeNode = { children: new SvelteMap(), isFile: false };

	for (const entry of diffEntries) {
		const parts = entry.path.split("/");
		let current = root;
		for (let index = 0; index < parts.length; index += 1) {
			const part = parts[index];
			const isLast = index === parts.length - 1;
			if (!current.children.has(part)) {
				current.children.set(part, { children: new SvelteMap(), isFile: isLast });
			}
			const next = current.children.get(part);
			if (!next) break;
			current = next;
			if (isLast) {
				current.isFile = true;
			}
		}
	}

	function convertToNodes(node: TreeNode, parentPath: string): SessionFileTreeNode[] {
		const nodes: SessionFileTreeNode[] = [];
		for (const [name, child] of node.children) {
			const path = parentPath === "." ? name : `${parentPath}/${name}`;
			const isDirectory = !child.isFile || child.children.size > 0;
			nodes.push({
				name,
				path,
				type: isDirectory ? "directory" : "file",
				children: isDirectory ? convertToNodes(child, path) : undefined,
				changed: true,
				status: isDirectory ? undefined : statusMap.get(path),
			});
		}

		return nodes.sort((a, b) => {
			if (a.type !== b.type) {
				return a.type === "directory" ? -1 : 1;
			}
			return a.name.localeCompare(b.name);
		});
	}

	return convertToNodes(root, ".");
}

function createBufferState(record: SessionFileRecord): SessionFileBufferState {
	return {
		content: record.content,
		originalContent: record.content,
		encoding: record.encoding,
		isDirty: false,
		isSaving: false,
		saveError: null,
		hasConflict: false,
		conflictContent: null,
		fromBase: record.fromBase,
	};
}

function isAbortError(error: unknown): boolean {
	return error instanceof Error && error.name === "AbortError";
}

export function isPathAtOrWithin(path: string, targetPath: string): boolean {
	return path === targetPath || path.startsWith(`${targetPath}/`);
}

export function renamePath(path: string, oldPath: string, newPath: string): string {
	if (!isPathAtOrWithin(path, oldPath)) {
		return path;
	}
	return `${newPath}${path.slice(oldPath.length)}`;
}

export function remapRecordKeys<T>(
	records: Record<string, T>,
	oldPath: string,
	newPath: string,
): Record<string, T> {
	const next: Record<string, T> = {};
	for (const [path, value] of Object.entries(records)) {
		next[renamePath(path, oldPath, newPath)] = value;
	}
	return next;
}

export function removeRecordKeys<T>(records: Record<string, T>, targetPath: string): Record<string, T> {
	const next: Record<string, T> = {};
	for (const [path, value] of Object.entries(records)) {
		if (isPathAtOrWithin(path, targetPath)) {
			continue;
		}
		next[path] = value;
	}
	return next;
}

export function hasDirtyBufferAtOrWithinPath(
	records: Record<string, SessionFileBufferState>,
	targetPath: string,
): boolean {
	for (const [path, value] of Object.entries(records)) {
		if (value.isDirty && isPathAtOrWithin(path, targetPath)) {
			return true;
		}
	}
	return false;
}

function throwIfAborted(signal?: AbortSignal) {
	if (signal?.aborted) {
		throw signal.reason instanceof Error
			? signal.reason
			: new DOMException("Operation aborted", "AbortError");
	}
}

function getAllDirectoryPaths(tree: SessionFileTreeNode[]): string[] {
	const paths = ["."];
	function visit(nodes: SessionFileTreeNode[]) {
		for (const node of nodes) {
			if (node.type !== "directory") continue;
			paths.push(node.path);
			if (node.children) {
				visit(node.children);
			}
		}
	}
	visit(tree);
	return uniquePaths(paths);
}

function remapFileRecords(
	records: Record<string, SessionFileRecord>,
	oldPath: string,
	newPath: string,
): Record<string, SessionFileRecord> {
	const next: Record<string, SessionFileRecord> = {};
	for (const [path, value] of Object.entries(records)) {
		const remappedPath = renamePath(path, oldPath, newPath);
		next[remappedPath] =
			remappedPath === path
				? value
				: {
					...value,
					path: remappedPath,
				};
	}
	return next;
}

function remapPathList(paths: string[], oldPath: string, newPath: string): string[] {
	return uniquePaths(paths.map((path) => renamePath(path, oldPath, newPath)));
}

function removePathList(paths: string[], targetPath: string): string[] {
	return paths.filter((path) => !isPathAtOrWithin(path, targetPath));
}

export function createSessionFilesDomain(args: CreateSessionFilesDomainArgs): SessionFilesDomain {
	let openPaths = $state<string[]>([]);
	let fileRecords = $state<Record<string, SessionFileRecord>>({});
	let buffers = $state<Record<string, SessionFileBufferState>>({});
	let diffData = $state<DiffState>({ files: [], stats: EMPTY_DIFF_STATS });
	let searchable = $state<string[]>([]);
	let rootNodes = $state<SessionFileTreeNode[]>([]);
	const childrenCache = new SvelteMap<string, SessionFileTreeNode[]>();
	let expandedPaths = $state<string[]>(["."]);
	let loadingPaths = $state<string[]>([]);
	let showChangedOnly = $state(false);
	let expandAllController = $state<AbortController | null>(null);

	const editorRuntime = new SvelteMap<string, EditorRuntimeState>();

	const diff = $derived(diffData.files);
	const diffStats = $derived(diffData.stats);
	const diffEntriesMap = $derived.by(
		() => new SvelteMap(diff.map((entry) => [entry.path, entry] as const)),
	);
	const tree = $derived.by(() =>
		showChangedOnly
			? buildTreeFromChangedFiles(diff)
			: buildTreeFromCache(rootNodes, childrenCache, diffEntriesMap),
	);
	const list = $derived(
		uniquePaths([...openPaths, ...diff.map((file) => file.path), ...searchable.slice(0, 20)]),
	);
	const contents = $derived.by(() => {
		const next: Record<string, string> = {};
		for (const [path, record] of Object.entries(fileRecords)) {
			next[path] = buffers[path]?.content ?? record.content;
		}
		return next;
	});

	function syncSelectedFile(nextList = list, nextSearchable = searchable) {
		const selectedFile = args.getSelectedFile();
		if (!selectedFile || nextList.includes(selectedFile)) {
			return;
		}
		args.openFile(nextList[0] ?? nextSearchable[0]);
	}

	function getFileRecord(path: string): SessionFileRecord | null {
		return fileRecords[path] ?? null;
	}

	function getFileBuffer(path: string): SessionFileBufferState | null {
		return buffers[path] ?? null;
	}

	function setBuffer(path: string, value: SessionFileBufferState) {
		buffers = {
			...buffers,
			[path]: value,
		};
	}

	function disposeEditorRuntimePath(path: string) {
		const runtime = editorRuntime.get(path);
		if (runtime?.model && "dispose" in (runtime.model as object)) {
			(runtime.model as { dispose: () => void }).dispose();
		}
		editorRuntime.delete(path);
	}

	function removeFileRecord(path: string) {
		if (!(path in fileRecords)) {
			return;
		}
		const { [path]: _removed, ...nextRecords } = fileRecords;
		fileRecords = nextRecords;
	}

	function removeFileBuffer(path: string) {
		if (!(path in buffers)) {
			return;
		}
		const { [path]: _removed, ...nextBuffers } = buffers;
		buffers = nextBuffers;
	}

	function releaseClosedFileState(path: string) {
		disposeEditorRuntimePath(path);
		removeFileRecord(path);
		if (!buffers[path]?.isDirty) {
			removeFileBuffer(path);
		}
	}

	function cancelExpandAll() {
		expandAllController?.abort();
		expandAllController = null;
	}

	async function loadDirectory(
		path: string,
		options: { force?: boolean; signal?: AbortSignal } = {},
	) {
		const force = options.force ?? false;
		const signal = options.signal;
		if (!args.hasSession() || showChangedOnly) {
			return;
		}
		if (!force && (path === "." || childrenCache.has(path) || loadingPaths.includes(path))) {
			return;
		}

		throwIfAborted(signal);
		const currentDiffEntriesMap = new SvelteMap(diff.map((entry) => [entry.path, entry] as const));
		loadingPaths = uniquePaths([...loadingPaths, path]);
		try {
			const response = await api.listSessionFiles(args.sessionId, path, { signal });
			throwIfAborted(signal);
			childrenCache.set(path, entriesToNodes(response.entries, path, currentDiffEntriesMap));
		} catch (error) {
			if (isAbortError(error)) {
				throw error;
			}
			childrenCache.set(path, entriesToNodes([], path, currentDiffEntriesMap));
		} finally {
			loadingPaths = loadingPaths.filter((entry) => entry !== path);
		}
	}

	async function loadFile(path: string, force = false) {
		if (!args.hasSession() || !path) {
			return;
		}
		if (!force && fileRecords[path]) {
			return;
		}

		const diffEntry = diff.find((file) => file.path === path);
		const fromBase = diffEntry?.status === "deleted";
		const response = await api.readSessionFile(args.sessionId, path, { fromBase });
		const nextRecord: SessionFileRecord = {
			path,
			content: response.content,
			encoding: response.encoding,
			size: response.size,
			fromBase,
		};

		fileRecords = {
			...fileRecords,
			[path]: nextRecord,
		};

		const existingBuffer = buffers[path];
		if (!existingBuffer || !existingBuffer.isDirty) {
			setBuffer(path, createBufferState(nextRecord));
		}
	}

	async function refresh() {
		cancelExpandAll();
		if (!args.hasSession()) {
			openPaths = [];
			fileRecords = {};
			buffers = {};
			diffData = { files: [], stats: EMPTY_DIFF_STATS };
			searchable = [];
			rootNodes = [];
			childrenCache.clear();
			expandedPaths = ["."];
			loadingPaths = [];
			syncSelectedFile([], []);
			return;
		}

		const [diffResponse, searchResponse, rootResponse] = await Promise.all([
			api.getSessionDiff(args.sessionId, { format: "files" }),
			api.searchSessionFiles(args.sessionId, "", 200),
			api.listSessionFiles(args.sessionId, "."),
		]);

		const nextDiffData =
			"files" in diffResponse && "stats" in diffResponse
				? (diffResponse as DiffState)
				: { files: [], stats: EMPTY_DIFF_STATS };
		const nextDiffEntriesMap = new SvelteMap(
			nextDiffData.files.map((entry) => [entry.path, entry] as const),
		);

		diffData = nextDiffData;
		searchable = searchResponse.results
			.filter((entry) => entry.type === "file")
			.map((entry) => entry.path);
		rootNodes = entriesToNodes(rootResponse.entries, ".", nextDiffEntriesMap);
		childrenCache.clear();

		for (const path of expandedPaths.filter((entry) => entry !== ".")) {
			await loadDirectory(path, { force: true });
		}

		syncSelectedFile(
			uniquePaths([
				...openPaths,
				...nextDiffData.files.map((file) => file.path),
				...searchable.slice(0, 20),
			]),
			searchable,
		);
	}

	function discard(path: string) {
		const current = buffers[path];
		if (!current) {
			return;
		}

		setBuffer(path, {
			...current,
			content: current.originalContent,
			isDirty: false,
			saveError: null,
			hasConflict: false,
			conflictContent: null,
		});

		const runtime = editorRuntime.get(path);
		if (runtime?.model && "setValue" in (runtime.model as object)) {
			(runtime.model as { setValue: (value: string) => void }).setValue(current.originalContent);
		}
	}

	async function save(path: string): Promise<boolean> {
		const current = buffers[path];
		if (!current || !current.isDirty) {
			return true;
		}
		if (current.encoding !== "utf8" || current.fromBase) {
			return false;
		}

		setBuffer(path, {
			...current,
			isSaving: true,
			saveError: null,
		});

		try {
			await api.writeSessionFile(args.sessionId, {
				path,
				content: current.content,
				encoding: current.encoding,
				originalContent: current.originalContent,
			});

			const nextRecord: SessionFileRecord = {
				path,
				content: current.content,
				encoding: current.encoding,
				size: current.content.length,
				fromBase: false,
			};
			fileRecords = {
				...fileRecords,
				[path]: nextRecord,
			};
			setBuffer(path, {
				...current,
				originalContent: current.content,
				isDirty: false,
				isSaving: false,
				saveError: null,
				hasConflict: false,
				conflictContent: null,
				fromBase: false,
			});
			await refresh();
			return true;
		} catch (error) {
			if (error instanceof FileConflictError) {
				setBuffer(path, {
					...current,
					isSaving: false,
					saveError: "File has been modified by another process",
					hasConflict: true,
					conflictContent: error.currentContent,
				});
				return false;
			}

			setBuffer(path, {
				...current,
				isSaving: false,
				saveError: error instanceof Error ? error.message : "Save failed",
			});
			return false;
		}
	}

	function acceptConflict(path: string) {
		const current = buffers[path];
		if (!current?.conflictContent) {
			return;
		}

		setBuffer(path, {
			content: current.conflictContent,
			originalContent: current.conflictContent,
			encoding: current.encoding,
			isDirty: false,
			isSaving: false,
			saveError: null,
			hasConflict: false,
			conflictContent: null,
			fromBase: current.fromBase,
		});

		fileRecords = {
			...fileRecords,
			[path]: {
				...(fileRecords[path] ?? {
					path,
					encoding: current.encoding,
					size: current.conflictContent.length,
					fromBase: current.fromBase,
				}),
				content: current.conflictContent,
				size: current.conflictContent.length,
			},
		};

		const runtime = editorRuntime.get(path);
		if (runtime?.model && "setValue" in (runtime.model as object)) {
			(runtime.model as { setValue: (value: string) => void }).setValue(current.conflictContent);
		}
	}

	async function forceSave(path: string): Promise<boolean> {
		const current = buffers[path];
		if (!current || current.encoding !== "utf8" || current.fromBase) {
			return false;
		}

		setBuffer(path, {
			...current,
			isSaving: true,
			saveError: null,
		});

		try {
			await api.writeSessionFile(args.sessionId, {
				path,
				content: current.content,
				encoding: current.encoding,
			});

			const nextRecord: SessionFileRecord = {
				path,
				content: current.content,
				encoding: current.encoding,
				size: current.content.length,
				fromBase: false,
			};
			fileRecords = {
				...fileRecords,
				[path]: nextRecord,
			};
			setBuffer(path, {
				...current,
				originalContent: current.content,
				isDirty: false,
				isSaving: false,
				saveError: null,
				hasConflict: false,
				conflictContent: null,
				fromBase: false,
			});
			await refresh();
			return true;
		} catch (error) {
			setBuffer(path, {
				...current,
				isSaving: false,
				saveError: error instanceof Error ? error.message : "Save failed",
			});
			return false;
		}
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
		get activePath() {
			return args.getSelectedFile();
		},
		get openPaths() {
			return openPaths;
		},
		get tree() {
			return tree;
		},
		get showChangedOnly() {
			return showChangedOnly;
		},
		get expandedPaths() {
			return expandedPaths;
		},
		getRecord: getFileRecord,
		getBuffer: getFileBuffer,
		isPathLoading: (path) => loadingPaths.includes(path),
		hasDirtyChanges: (path) => hasDirtyBufferAtOrWithinPath(buffers, path),
		open: async (file?: string) => {
			if (list.length === 0 && searchable.length === 0) {
				await refresh();
			}
			const nextFile = (file ?? args.getSelectedFile()) || list[0] || searchable[0] || "";
			if (!nextFile) {
				args.openFile();
				return;
			}
			if (!openPaths.includes(nextFile)) {
				openPaths = uniquePaths([...openPaths, nextFile]);
			}
			args.openFile(nextFile);
			await loadFile(nextFile);
		},
		close: (file) => {
			const nextOpenPaths = openPaths.filter((path) => path !== file);
			const nextFile = nextOpenPaths.at(-1) ?? "";
			const closingActiveFile = args.getSelectedFile() === file;
			openPaths = nextOpenPaths;
			if (closingActiveFile) {
				args.openFile(nextFile);
			}
			releaseClosedFileState(file);
		},
		refresh,
		toggleChangedOnly: async () => {
			showChangedOnly = !showChangedOnly;
			await refresh();
		},
		toggleDirectory: async (path) => {
			if (expandedPaths.includes(path)) {
				expandedPaths = expandedPaths.filter((entry) => entry !== path);
				return;
			}

			expandedPaths = uniquePaths([...expandedPaths, path]);
			await loadDirectory(path, { force: false });
		},
		expandAll: async () => {
			cancelExpandAll();
			if (showChangedOnly) {
				expandedPaths = getAllDirectoryPaths(tree);
				return;
			}

			const controller = new AbortController();
			expandAllController = controller;

			async function loadAll(nodes: SessionFileTreeNode[], signal: AbortSignal): Promise<void> {
				throwIfAborted(signal);
				await Promise.all(
					nodes.map(async (node) => {
						if (node.type !== "directory") {
							return;
						}
						throwIfAborted(signal);
						expandedPaths = uniquePaths([...expandedPaths, node.path]);
						await loadDirectory(node.path, { force: true, signal });
						throwIfAborted(signal);
						const nextChildren = childrenCache.get(node.path) ?? node.children ?? [];
						if (nextChildren.length > 0) {
							await loadAll(nextChildren, signal);
						}
					}),
				);
			}

			try {
				await loadAll(tree, controller.signal);
				throwIfAborted(controller.signal);
				expandedPaths = getAllDirectoryPaths(
					buildTreeFromCache(rootNodes, childrenCache, diffEntriesMap),
				);
			} catch (error) {
				if (!isAbortError(error)) {
					throw error;
				}
			} finally {
				if (expandAllController === controller) {
					expandAllController = null;
				}
			}
		},
		collapseAll: () => {
			cancelExpandAll();
			expandedPaths = ["."];
		},
		rename: async (path, nextName) => {
			const trimmedName = nextName.trim();
			if (!args.hasSession() || !path || !trimmedName) {
				return false;
			}
			if (hasDirtyBufferAtOrWithinPath(buffers, path)) {
				return false;
			}

			const parentPath = path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : "";
			const newPath = parentPath ? `${parentPath}/${trimmedName}` : trimmedName;
			if (newPath === path) {
				return true;
			}

			await api.renameSessionFile(args.sessionId, { oldPath: path, newPath });

			const remappedRuntime = new SvelteMap<string, EditorRuntimeState>();
			for (const [runtimePath, runtime] of editorRuntime.entries()) {
				if (!isPathAtOrWithin(runtimePath, path)) {
					continue;
				}
				if (runtime.model && "dispose" in (runtime.model as object)) {
					(runtime.model as { dispose: () => void }).dispose();
				}
				remappedRuntime.set(renamePath(runtimePath, path, newPath), {
					model: null,
					viewState: runtime.viewState,
				});
				editorRuntime.delete(runtimePath);
			}
			for (const [runtimePath, runtime] of remappedRuntime.entries()) {
				editorRuntime.set(runtimePath, runtime);
			}

			openPaths = remapPathList(openPaths, path, newPath);
			expandedPaths = remapPathList(expandedPaths, path, newPath);
			fileRecords = remapFileRecords(fileRecords, path, newPath);
			buffers = remapRecordKeys(buffers, path, newPath);

			const selectedPath = args.getSelectedFile();
			if (selectedPath && isPathAtOrWithin(selectedPath, path)) {
				args.openFile(renamePath(selectedPath, path, newPath));
			}

			await refresh();
			return true;
		},
		remove: async (path) => {
			if (!args.hasSession() || !path) {
				return false;
			}
			if (hasDirtyBufferAtOrWithinPath(buffers, path)) {
				return false;
			}

			await api.deleteSessionFile(args.sessionId, { path });

			for (const runtimePath of [...editorRuntime.keys()]) {
				if (isPathAtOrWithin(runtimePath, path)) {
					disposeEditorRuntimePath(runtimePath);
				}
			}

			openPaths = removePathList(openPaths, path);
			expandedPaths = removePathList(expandedPaths, path);
			fileRecords = removeRecordKeys(fileRecords, path);
			buffers = removeRecordKeys(buffers, path);

			const selectedPath = args.getSelectedFile();
			if (selectedPath && isPathAtOrWithin(selectedPath, path)) {
				args.openFile(openPaths.at(-1) ?? "");
			}

			await refresh();
			return true;
		},
		updateBuffer: (path, content) => {
			const record = fileRecords[path];
			const current = buffers[path] ??
				(record
					? createBufferState(record)
					: {
						content,
						originalContent: "",
						encoding: "utf8",
						isDirty: true,
						isSaving: false,
						saveError: null,
						hasConflict: false,
						conflictContent: null,
						fromBase: false,
					});
			setBuffer(path, {
				...current,
				content,
				isDirty: content !== current.originalContent,
				saveError: null,
			});
		},
		discard,
		save,
		acceptConflict,
		forceSave,
		getEditorModel: (path) => editorRuntime.get(path)?.model ?? null,
		setEditorModel: (path, model) => {
			const current = editorRuntime.get(path) ?? { model: null, viewState: null };
			editorRuntime.set(path, { ...current, model });
		},
		getEditorViewState: (path) => editorRuntime.get(path)?.viewState ?? null,
		setEditorViewState: (path, viewState) => {
			const current = editorRuntime.get(path) ?? { model: null, viewState: null };
			editorRuntime.set(path, { ...current, viewState });
		},
		dispose: () => {
			cancelExpandAll();
			for (const runtime of editorRuntime.values()) {
				if (runtime.model && "dispose" in (runtime.model as object)) {
					(runtime.model as { dispose: () => void }).dispose();
				}
			}
			editorRuntime.clear();
		},
	};
}
