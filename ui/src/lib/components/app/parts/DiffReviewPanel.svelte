<script lang="ts">
	import type { FileContents, FileDiffOptions } from "@pierre/diffs";
	import { FileDiff as PierreFileDiff } from "@pierre/diffs";
	import CheckIcon from "@lucide/svelte/icons/check";
	import ChevronDownIcon from "@lucide/svelte/icons/chevron-down";
	import ChevronRightIcon from "@lucide/svelte/icons/chevron-right";
	import RefreshCwIcon from "@lucide/svelte/icons/refresh-cw";

	import { writeStorage } from "$lib/app/app-helpers";
	import { api } from "$lib/api-client";
	import type {
		SessionDiffFileEntry,
		SessionDiffStats,
		SessionSingleFileDiffResponse,
	} from "$lib/api-types";
	import DockWindowChrome from "$lib/components/app/parts/DockWindowChrome.svelte";
	import { Badge } from "$lib/components/ui/badge";
	import { Button } from "$lib/components/ui/button";
	import { countDiffLinesFast, hashString, reconstructOriginalFromPatch } from "$lib/diff-utils";
	import type { ResolvedTheme } from "$lib/theme";
	import { cn } from "$lib/utils";
	import { onMount, untrack } from "svelte";

	const APPROVAL_STORAGE_KEY = "discobot.ui.diff-review.approved";
	const DIFF_STYLE_STORAGE_KEY = "discobot.ui.diff-review.style";
	const DIFF_WARNING_THRESHOLD = 10000;
	const DIFF_HARD_LIMIT = 20000;

	type DiffStyle = "split" | "unified";

	type Props = {
		dockMaximized: boolean;
		onClose: () => void;
		onOpenFile: (path: string) => Promise<void> | void;
		onRefresh: () => Promise<void> | void;
		onToggleDockMaximized: () => void;
		sessionId: string;
		diff: SessionDiffFileEntry[];
		fileContents: Record<string, string>;
		diffStats: SessionDiffStats;
		resolvedTheme: ResolvedTheme;
	};

	type SnapshotState = {
		originalContent: string;
		modifiedContent: string;
		source: "reverse-patch" | "base-read";
	};

	type ReadyDiffState = {
		status: "ready";
		response: SessionSingleFileDiffResponse;
		patchHash: string | null;
		lineCount: number;
		snapshotStatus: "idle" | "loading" | "ready" | "error";
		snapshot?: SnapshotState;
		snapshotError?: string;
	};

	type LoadedDiffState =
		| {
				status: "loading";
				snapshotStatus: "idle";
		  }
		| {
				status: "error";
				errorMessage: string;
				snapshotStatus: "idle";
		  }
		| ReadyDiffState;

	type DiffRendererParams = {
		diffStyle: DiffStyle;
		resolvedTheme: ResolvedTheme;
		oldFile: FileContents;
		newFile: FileContents;
	};

	const LANGUAGE_MAP: Record<string, string> = {
		js: "javascript",
		jsx: "javascript",
		ts: "typescript",
		tsx: "typescript",
		py: "python",
		rb: "ruby",
		go: "go",
		rs: "rust",
		java: "java",
		c: "c",
		cpp: "cpp",
		h: "c",
		hpp: "cpp",
		cs: "csharp",
		php: "php",
		swift: "swift",
		kt: "kotlin",
		html: "html",
		css: "css",
		scss: "scss",
		json: "json",
		xml: "xml",
		yaml: "yaml",
		yml: "yaml",
		md: "markdown",
		sql: "sql",
		sh: "bash",
		bash: "bash",
		zsh: "bash",
		dockerfile: "docker",
		makefile: "make",
		toml: "toml",
		graphql: "graphql",
		gql: "graphql",
		svelte: "svelte",
	};

	let {
		dockMaximized,
		onClose,
		onOpenFile,
		onRefresh,
		onToggleDockMaximized,
		sessionId,
		diff,
		fileContents,
		diffStats,
		resolvedTheme,
	}: Props = $props();

	let diffStates = $state<Record<string, LoadedDiffState>>({});
	let approvedBySession = $state<Record<string, Record<string, string>>>({});
	let storageLoaded = $state(false);
	let expandedPath = $state<string | null>(null);
	let forceLoadedPaths = $state<string[]>([]);
	let refreshing = $state(false);
	let loadGeneration = 0;
	let diffStyle = $state<DiffStyle>("unified");

	const sortedDiff = $derived.by(() => [...diff].toSorted((left, right) => left.path.localeCompare(right.path)));
	const sessionApprovals = $derived.by(() => approvedBySession[sessionId] ?? {});
	const approvedCount = $derived.by(() => sortedDiff.filter((file) => isApproved(file.path)).length);
	const allApproved = $derived.by(() => sortedDiff.length > 0 && sortedDiff.every((file) => isApproved(file.path)));
	const maximizeTitle = $derived.by(() => (dockMaximized ? "Restore split view" : "Maximize diff review panel"));
	const diffKey = $derived.by(() =>
		sortedDiff.map((file) => `${file.path}:${file.status}:${file.oldPath ?? ""}`).join("|")
	);

	onMount(() => {
		approvedBySession = readApprovalState();
		diffStyle = readDiffStyle();
		storageLoaded = true;
	});

	$effect(() => {
		if (!storageLoaded) {
			return;
		}
		writeStorage(APPROVAL_STORAGE_KEY, JSON.stringify(approvedBySession));
	});

	$effect(() => {
		if (!storageLoaded) {
			return;
		}
		writeStorage(DIFF_STYLE_STORAGE_KEY, diffStyle);
	});

	$effect(() => {
		const currentSessionId = sessionId;
		const currentDiffKey = diffKey;
		void currentDiffKey;

		if (!currentSessionId) {
			diffStates = {};
			expandedPath = null;
			forceLoadedPaths = [];
			return;
		}

		const generation = ++loadGeneration;
		const currentExpandedPath = untrack(() => expandedPath);
		const currentForceLoadedPaths = untrack(() => forceLoadedPaths);
		const nextStates = Object.fromEntries(
			sortedDiff.map((file) => [
				file.path,
				{ status: "loading", snapshotStatus: "idle" } satisfies LoadedDiffState,
			]),
		);
		if (sortedDiff.length === 0) {
			diffStates = {};
			expandedPath = null;
			forceLoadedPaths = [];
			return;
		}
		if (currentExpandedPath && !sortedDiff.some((file) => file.path === currentExpandedPath)) {
			expandedPath = null;
		}
		forceLoadedPaths = currentForceLoadedPaths.filter((path) => sortedDiff.some((file) => file.path === path));
		diffStates = nextStates;
		void loadDiffEntries(generation, currentSessionId, sortedDiff);
	});

	$effect(() => {
		if (!expandedPath) {
			return;
		}
		void ensureSnapshot(expandedPath);
	});

	function readApprovalState(): Record<string, Record<string, string>> {
		if (typeof window === "undefined") {
			return {};
		}
		const stored = window.localStorage.getItem(APPROVAL_STORAGE_KEY);
		if (!stored) {
			return {};
		}
		try {
			const parsed = JSON.parse(stored);
			return typeof parsed === "object" && parsed !== null ? parsed : {};
		} catch {
			return {};
		}
	}

	function readDiffStyle(): DiffStyle {
		if (typeof window === "undefined") {
			return "unified";
		}
		const stored = window.localStorage.getItem(DIFF_STYLE_STORAGE_KEY);
		return stored === "split" ? "split" : "unified";
	}

	function errorMessage(error: unknown): string {
		if (error instanceof Error && error.message.length > 0) {
			return error.message;
		}
		return "Unable to load diff.";
	}

	function getLanguageFromPath(path: string): string | undefined {
		const filename = path.split("/").at(-1)?.toLowerCase() ?? "";
		if (filename === "dockerfile") return "docker";
		if (filename === "makefile") return "make";
		const extension = path.split(".").at(-1)?.toLowerCase() ?? "";
		return LANGUAGE_MAP[extension];
	}

	function buildFileContents(path: string, content: string, cacheKey: string | null): FileContents {
		const language = getLanguageFromPath(path);
		return {
			name: path,
			contents: content,
			lang: language,
			cacheKey: cacheKey ?? `${path}:${content.length}`,
		};
	}

	function getRendererOptions(style: DiffStyle, theme: ResolvedTheme): FileDiffOptions<undefined> {
		return {
			diffStyle: style,
			theme: {
				light: "github-light",
				dark: "github-dark",
			},
			themeType: theme === "dark" ? "dark" : "light",
			disableFileHeader: true,
			hunkSeparators: "line-info",
			expandUnchanged: false,
			collapsedContextThreshold: 3,
			expansionLineCount: 20,
			lineDiffType: "word",
			overflow: "scroll",
		};
	}

	function renderPierreDiff(node: HTMLDivElement, params: DiffRendererParams) {
		const instance = new PierreFileDiff(getRendererOptions(params.diffStyle, params.resolvedTheme));

		function render(next: DiffRendererParams) {
			instance.setOptions(getRendererOptions(next.diffStyle, next.resolvedTheme));
			instance.render({
				oldFile: next.oldFile,
				newFile: next.newFile,
				containerWrapper: node,
				forceRender: true,
			});
		}

		render(params);

		return {
			update(next: DiffRendererParams) {
				render(next);
			},
			destroy() {
				instance.cleanUp();
				node.replaceChildren();
			},
		};
	}

	async function loadDiffEntries(
		generation: number,
		currentSessionId: string,
		entries: SessionDiffFileEntry[],
	) {
		const loadedEntries = await Promise.all(
			entries.map(async (entry) => {
				try {
					const response = (await api.getSessionDiff(currentSessionId, {
						path: entry.path,
					})) as SessionSingleFileDiffResponse;
					const patchHash = response.patch ? await hashString(response.patch) : null;
					return [
						entry.path,
						{
							status: "ready",
							response,
							patchHash,
							lineCount: response.patch ? countDiffLinesFast(response.patch) : 0,
							snapshotStatus: "idle",
						} satisfies LoadedDiffState,
					] as const;
				} catch (error) {
					return [
						entry.path,
						{
							status: "error",
							errorMessage: errorMessage(error),
							snapshotStatus: "idle",
						} satisfies LoadedDiffState,
					] as const;
				}
			}),
		);

		if (generation !== loadGeneration) {
			return;
		}

		diffStates = Object.fromEntries(loadedEntries);
	}

	async function ensureSnapshot(path: string) {
		const state = diffStates[path];
		if (!state || state.status !== "ready") {
			return;
		}
		if (state.snapshotStatus === "loading" || state.snapshotStatus === "ready") {
			return;
		}
		if (state.response.binary || !state.response.patch) {
			return;
		}

		diffStates = {
			...diffStates,
			[path]: {
				...state,
				snapshotStatus: "loading",
			},
		};

		try {
			let originalContent = "";
			let modifiedContent = "";
			let source: SnapshotState["source"] = "reverse-patch";

			if (state.response.status === "deleted") {
				originalContent = reconstructOriginalFromPatch("", state.response.patch);
				if (originalContent.length === 0 && state.response.deletions > 0) {
					const baseFile = await api.readSessionFile(sessionId, path, { fromBase: true });
					originalContent = baseFile.content;
					source = "base-read";
				}
			} else {
				modifiedContent =
					fileContents[path] ?? (await api.readSessionFile(sessionId, path)).content;
				originalContent =
					state.response.status === "added"
						? ""
						: reconstructOriginalFromPatch(modifiedContent, state.response.patch);
			}

			const nextState = diffStates[path];
			if (!nextState || nextState.status !== "ready") {
				return;
			}

			diffStates = {
				...diffStates,
				[path]: {
					...nextState,
					snapshotStatus: "ready",
					snapshot: {
						originalContent,
						modifiedContent,
						source,
					},
				},
			};
		} catch (error) {
			const nextState = diffStates[path];
			if (!nextState || nextState.status !== "ready") {
				return;
			}
			diffStates = {
				...diffStates,
				[path]: {
					...nextState,
					snapshotStatus: "error",
					snapshotError: errorMessage(error),
				},
			};
		}
	}

	function toggleExpanded(path: string) {
		expandedPath = expandedPath === path ? null : path;
	}

	function isApproved(path: string): boolean {
		const patchHash = diffStates[path]?.status === "ready" ? diffStates[path].patchHash : null;
		return Boolean(patchHash && sessionApprovals[path] === patchHash);
	}

	function toggleApproved(path: string) {
		const state = diffStates[path];
		if (!state || state.status !== "ready" || !state.patchHash) {
			return;
		}

		const nextSessionApprovals = { ...sessionApprovals };
		const wasApproved = nextSessionApprovals[path] === state.patchHash;
		if (wasApproved) {
			delete nextSessionApprovals[path];
		} else {
			nextSessionApprovals[path] = state.patchHash;
		}

		approvedBySession = {
			...approvedBySession,
			[sessionId]: nextSessionApprovals,
		};

		if (!wasApproved && expandedPath === path) {
			const currentIndex = sortedDiff.findIndex((file) => file.path === path);
			const nextUnapproved = sortedDiff
				.slice(currentIndex + 1)
				.find((file) => {
					const nextState = diffStates[file.path];
					const nextHash = nextState && nextState.status === "ready" ? nextState.patchHash : null;
					return !(nextHash && nextSessionApprovals[file.path] === nextHash);
				});
			expandedPath = nextUnapproved?.path ?? null;
		}
	}

	function markAllApproved() {
		const nextSessionApprovals: Record<string, string> = {};
		for (const file of sortedDiff) {
			const state = diffStates[file.path];
			if (state?.status === "ready" && state.patchHash) {
				nextSessionApprovals[file.path] = state.patchHash;
			}
		}
		approvedBySession = {
			...approvedBySession,
			[sessionId]: nextSessionApprovals,
		};
	}

	function loadLargeDiff(path: string) {
		if (!forceLoadedPaths.includes(path)) {
			forceLoadedPaths = [...forceLoadedPaths, path];
		}
	}

	function statusBadgeClass(status: SessionSingleFileDiffResponse["status"] | SessionDiffFileEntry["status"]) {
		switch (status) {
			case "added":
				return "text-green-500 border-green-500/40";
			case "modified":
				return "text-yellow-500 border-yellow-500/40";
			case "deleted":
				return "text-red-500 border-red-500/40";
			case "renamed":
				return "text-purple-500 border-purple-500/40";
			default:
				return "text-muted-foreground border-border";
		}
	}

	function statusLabel(status: SessionSingleFileDiffResponse["status"] | SessionDiffFileEntry["status"]) {
		switch (status) {
			case "added":
				return "Added";
			case "modified":
				return "Modified";
			case "deleted":
				return "Deleted";
			case "renamed":
				return "Renamed";
			default:
				return "Changed";
		}
	}

	function showLargeDiffFallback(path: string, state: LoadedDiffState): boolean {
		if (state.status !== "ready") {
			return false;
		}
		if (state.lineCount > DIFF_HARD_LIMIT) {
			return true;
		}
		return state.lineCount > DIFF_WARNING_THRESHOLD && !forceLoadedPaths.includes(path);
	}

	function getRendererParams(path: string, state: ReadyDiffState): DiffRendererParams | null {
		if (state.snapshotStatus !== "ready" || !state.snapshot) {
			return null;
		}
		const oldPath = state.response.oldPath ?? path;
		const oldFile = buildFileContents(oldPath, state.snapshot.originalContent, state.patchHash ? `${state.patchHash}:old` : null);
		const newFile = buildFileContents(path, state.snapshot.modifiedContent, state.patchHash ? `${state.patchHash}:new` : null);
		return {
			diffStyle,
			resolvedTheme,
			oldFile,
			newFile,
		};
	}

	async function refreshPanel() {
		refreshing = true;
		try {
			await onRefresh();
		} finally {
			refreshing = false;
		}
	}
</script>

<DockWindowChrome
	dockMaximized={dockMaximized}
	onClose={onClose}
	onToggleDockMaximized={onToggleDockMaximized}
	closeLabel="Close diff review panel"
	minimizeLabel="Minimize diff review panel"
	maximizeTitle={maximizeTitle}
	contentClass="flex min-h-0 flex-1 flex-col overflow-hidden p-3"
>
	{#snippet title()}
		<div class="flex min-w-0 items-center gap-2 text-xs">
			<p class="truncate text-sm font-medium">Diff review</p>
			<span class="truncate text-sidebar-foreground/70">
				{sortedDiff.length} changed {sortedDiff.length === 1 ? "file" : "files"}
			</span>
			{#if approvedCount > 0}
				<span class="truncate text-sidebar-foreground/70">{approvedCount} approved</span>
			{/if}
		</div>
	{/snippet}

	{#snippet actions()}
		<div class="flex items-center gap-2">
			<div class="inline-flex rounded-md border border-border bg-background p-0.5">
				<Button
					variant={diffStyle === "unified" ? "secondary" : "ghost"}
					size="sm"
					class="h-8 rounded-r-none px-3"
					onclick={() => {
						diffStyle = "unified";
					}}
				>
					Unified
				</Button>
				<Button
					variant={diffStyle === "split" ? "secondary" : "ghost"}
					size="sm"
					class="h-8 rounded-l-none border-l border-border px-3"
					onclick={() => {
						diffStyle = "split";
					}}
				>
					Split
				</Button>
			</div>
			<Button variant="outline" size="sm" onclick={() => void refreshPanel()} disabled={refreshing}>
				{#if refreshing}
					<RefreshCwIcon class="size-4 animate-spin" />
				{:else}
					<RefreshCwIcon class="size-4" />
				{/if}
				Refresh
			</Button>
		</div>
	{/snippet}

	{#if sortedDiff.length === 0}
		<div class="rounded-md border border-sidebar-border bg-sidebar p-4 text-sm text-sidebar-foreground/60">
			No changed files yet.
		</div>
	{:else}
		<div class="flex min-h-0 flex-1 flex-col overflow-hidden rounded-md border border-sidebar-border bg-sidebar">
			<div class="flex items-center justify-between gap-3 border-b border-sidebar-border px-3 py-2">
				<div>
					<p class="text-xs font-medium uppercase tracking-[0.16em] text-sidebar-foreground/60">
						{sortedDiff.length} changed {sortedDiff.length === 1 ? "file" : "files"}
					</p>
					{#if approvedCount > 0}
						<p class="mt-1 text-xs text-sidebar-foreground/60">{approvedCount} approved</p>
					{/if}
				</div>
				<div class="flex items-center gap-3">
					<div class="flex items-center gap-3 text-xs font-medium">
						<span class="text-green-500">+{diffStats.additions}</span>
						<span class="text-red-500">-{diffStats.deletions}</span>
					</div>
					<Button variant="ghost" size="sm" onclick={markAllApproved} disabled={allApproved}>
						<CheckIcon class="size-4" />
						Mark all approved
					</Button>
				</div>
			</div>

			<div class="min-h-0 flex-1 overflow-y-auto">
				<div class="divide-y divide-sidebar-border">
					{#each sortedDiff as file (file.path)}
						{@const state = diffStates[file.path]}
						{@const expanded = expandedPath === file.path}
						{@const approved = isApproved(file.path)}
						<section class={cn("flex flex-col", approved && "opacity-80")}>
							<button
								type="button"
								class="flex items-center justify-between gap-3 bg-sidebar/60 px-3 py-2 text-left transition hover:bg-sidebar-accent/70"
								onclick={() => toggleExpanded(file.path)}
							>
								<div class="flex min-w-0 items-center gap-2">
									{#if expanded}
										<ChevronDownIcon class="size-4 shrink-0" />
									{:else}
										<ChevronRightIcon class="size-4 shrink-0" />
									{/if}
									<Badge
										variant="outline"
										class={cn("inline-grid grid-cols-1 place-items-center", statusBadgeClass(state?.status === "ready" ? state.response.status : file.status))}
									>
										<span class="col-start-1 row-start-1">{statusLabel(state?.status === "ready" ? state.response.status : file.status)}</span>
										<span class="invisible col-start-1 row-start-1" aria-hidden="true">Modified</span>
									</Badge>
									<div class="min-w-0">
										<p class="truncate font-mono text-xs text-foreground">{file.path}</p>
										{#if file.oldPath && file.oldPath !== file.path}
											<p class="truncate text-[11px] text-muted-foreground">{file.oldPath} → {file.path}</p>
										{/if}
									</div>
									{#if approved}
										<span class="flex items-center gap-1 text-xs text-green-500">
											<CheckIcon class="size-3.5" />
											Approved
										</span>
									{/if}
								</div>
								<div class="flex shrink-0 items-center gap-2 text-xs font-medium">
									{#if state?.status === "loading"}
										<span class="text-muted-foreground">Loading…</span>
									{:else if state?.status === "ready"}
										{#if state.response.additions > 0}
											<span class="text-green-500">+{state.response.additions}</span>
										{/if}
										{#if state.response.deletions > 0}
											<span class="text-red-500">-{state.response.deletions}</span>
										{/if}
									{/if}
								</div>
							</button>

							{#if expanded}
								<div class="space-y-3 px-3 py-3">
									<div class="flex flex-wrap items-center justify-between gap-2">
										<div class="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
											{#if state?.status === "ready"}
												{#if state.response.binary}
													<span>Binary diff</span>
												{:else if state.lineCount > 0}
													<span>{state.lineCount.toLocaleString()} diff lines</span>
												{/if}
												{#if state.snapshotStatus === "ready" && state.snapshot?.source === "base-read"}
													<span>Deleted snapshot loaded from base</span>
												{:else if state.snapshotStatus === "ready" && state.snapshot?.source === "reverse-patch"}
													<span>Original snapshot reconstructed from patch</span>
												{:else if state.snapshotStatus === "error"}
													<span>{state.snapshotError}</span>
												{/if}
											{/if}
										</div>
										<div class="flex flex-wrap items-center gap-2">
											<Button variant={approved ? "secondary" : "outline"} size="sm" onclick={() => toggleApproved(file.path)}>
												<CheckIcon class="size-4" />
												{approved ? "Approved" : "Mark approved"}
											</Button>
											<Button variant="outline" size="sm" onclick={() => void onOpenFile(file.path)}>
												Open file
											</Button>
										</div>
									</div>

									{#if !state || state.status === "loading"}
										<div class="rounded-md border border-border bg-background px-3 py-4 text-sm text-muted-foreground">
											Loading diff…
										</div>
									{:else if state.status === "error"}
										<div class="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-4 text-sm text-destructive">
											{state.errorMessage}
										</div>
									{:else if state.response.binary}
										<div class="rounded-md border border-border bg-background px-3 py-4 text-sm text-muted-foreground">
											This is a binary file, so the text diff cannot be rendered here.
										</div>
									{:else if showLargeDiffFallback(file.path, state)}
										<div class="rounded-md border border-border bg-background px-3 py-4 text-sm text-muted-foreground">
											<p class="font-medium text-foreground">
												Large diff ({state.lineCount.toLocaleString()} lines)
											</p>
											<p class="mt-1">
												This file exceeds the inline rendering threshold.
											</p>
											<div class="mt-3 flex flex-wrap items-center gap-2">
												{#if state.lineCount <= DIFF_HARD_LIMIT}
													<Button variant="outline" size="sm" onclick={() => loadLargeDiff(file.path)}>
														Load anyway
													</Button>
												{/if}
												<Button variant="outline" size="sm" onclick={() => void onOpenFile(file.path)}>
													Open file
												</Button>
											</div>
										</div>
									{:else if state.snapshotStatus !== "ready" || !state.snapshot}
										<div class="rounded-md border border-border bg-background px-3 py-4 text-sm text-muted-foreground">
											Preparing interactive diff…
										</div>
									{:else}
										{@const rendererParams = getRendererParams(file.path, state)}
										{#if rendererParams}
											<div
												class="overflow-hidden rounded-md border border-border bg-background"
												use:renderPierreDiff={rendererParams}
											></div>
										{/if}
									{/if}
								</div>
							{/if}
						</section>
					{/each}
				</div>
			</div>
		</div>
	{/if}
</DockWindowChrome>
