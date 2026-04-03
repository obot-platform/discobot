<script lang="ts">
	import ChevronDownIcon from "@lucide/svelte/icons/chevron-down";
	import ChevronRightIcon from "@lucide/svelte/icons/chevron-right";
	import EllipsisIcon from "@lucide/svelte/icons/ellipsis";
	import FolderIcon from "@lucide/svelte/icons/folder";
	import GitBranchIcon from "@lucide/svelte/icons/git-branch";
	import PackageIcon from "@lucide/svelte/icons/package";
	import PanelLeftIcon from "@lucide/svelte/icons/panel-left";
	import PlusIcon from "@lucide/svelte/icons/plus";
	import { Switch } from "$lib/components/ui/switch";
	import type { Workspace } from "$lib/api-types";
	import * as Collapsible from "$lib/components/ui/collapsible";
	import SessionStatus from "$lib/components/app/parts/SessionStatus.svelte";
	import {
		AlertDialog,
		AlertDialogAction,
		AlertDialogCancel,
		AlertDialogContent,
		AlertDialogDescription,
		AlertDialogFooter,
		AlertDialogHeader,
		AlertDialogTitle,
	} from "$lib/components/ui/alert-dialog";
	import { Button } from "$lib/components/ui/button";
	import * as Dialog from "$lib/components/ui/dialog";
	import {
		DropdownMenu,
		DropdownMenuContent,
		DropdownMenuItem,
		DropdownMenuTrigger,
	} from "$lib/components/ui/dropdown-menu";
	import { Input } from "$lib/components/ui/input";
	import { useAppContext } from "$lib/context/app-context.svelte";
	import { useSessionContext } from "$lib/context/session-context.svelte";

	type Props = {
		onThreadSelect?: () => void;
		onToggleSidebar?: () => void;
		mode?: "panel" | "dropdown" | "floating";
		collapsed?: boolean;
	};

	let {
		onThreadSelect,
		onToggleSidebar,
		mode = "panel",
		collapsed = false,
	}: Props = $props();

	const dropdownMode = $derived(mode === "dropdown");
	const floatingMode = $derived(mode === "floating");
	const floatingCollapsed = $derived(floatingMode && collapsed);

	const app = useAppContext();
	const sessions = app.sessions;
	const preferences = app.preferences;
	const session = useSessionContext();
	type SessionGroup = {
		key: string;
		workspaceId: string | null;
		label: string;
		sourceType: "local" | "git" | "managed";
		sessions: (typeof sessions.list)[number][];
	};

	let renameDialogOpen = $state(false);
	let renameSessionId = $state<string | null>(null);
	let renameDraft = $state("");
	let renamingSession = $state(false);
	let deleteDialogOpen = $state(false);
	let deleteSessionId = $state<string | null>(null);
	let deletingSession = $state(false);
	let renameWorkspaceDialogOpen = $state(false);
	let renameWorkspaceId = $state<string | null>(null);
	let renameWorkspaceDraft = $state("");
	let renamingWorkspace = $state(false);
	let deleteWorkspaceDialogOpen = $state(false);
	let deleteWorkspaceId = $state<string | null>(null);
	let deletingWorkspace = $state(false);
	let floatingOpen = $state(false);
	let shellRef = $state<HTMLElement | null>(null);
	const showSidebarBody = $derived(!floatingCollapsed || floatingOpen);

	function closeFloatingSidebar() {
		if (!floatingMode) {
			return;
		}
		floatingOpen = false;
	}

	function toggleFloatingSidebar() {
		if (!floatingCollapsed) {
			return;
		}
		floatingOpen = !floatingOpen;
	}

	$effect(() => {
		if (!floatingCollapsed) {
			floatingOpen = false;
		}
	});

	$effect(() => {
		if (
			!floatingCollapsed ||
			!floatingOpen ||
			typeof document === "undefined"
		) {
			return;
		}

		function handlePointerDown(event: PointerEvent) {
			const target = event.target;
			if (!(target instanceof Node)) {
				return;
			}
			if (shellRef?.contains(target)) {
				return;
			}
			floatingOpen = false;
		}

		document.addEventListener("pointerdown", handlePointerDown);
		return () => {
			document.removeEventListener("pointerdown", handlePointerDown);
		};
	});

	function sessionById(sessionId: string) {
		return sessions.list.find((s) => s.id === sessionId) ?? null;
	}

	function workspaceById(workspaceId: string | null) {
		if (!workspaceId) {
			return null;
		}
		return app.workspaces.get(workspaceId);
	}

	function handleSelectSession(sessionId: string) {
		sessions.select(sessionId);
		closeFloatingSidebar();
		onThreadSelect?.();
	}

	function handleSelectRecentThread(sessionId: string, threadId: string) {
		sessions.openThread(sessionId, threadId);
		closeFloatingSidebar();
		onThreadSelect?.();
	}

	function handleStartNewSession() {
		sessions.startNew();
		closeFloatingSidebar();
		onThreadSelect?.();
	}

	function hasRecentThreadSubtitle(
		threadObj: (typeof sessions.recentThreads)[number],
	) {
		return (threadObj.lastMessage ?? "").trim().length > 0;
	}

	function recentThreadStateLabel(
		threadObj: (typeof sessions.recentThreads)[number],
	) {
		if (threadObj.state === "interrupted") {
			return "Interrupted";
		}
		if (threadObj.state === "cancelled") {
			return "Cancelled";
		}
		return null;
	}

	function recentThreadStateClass(
		threadObj: (typeof sessions.recentThreads)[number],
	) {
		if (threadObj.state === "interrupted") {
			return "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300";
		}
		if (threadObj.state === "cancelled") {
			return "border-current/15 bg-current/10 text-current/75";
		}
		return "";
	}

	function openRenameDialog(sessionId: string) {
		const sessionItem = sessionById(sessionId);
		if (!sessionItem) {
			return;
		}
		renameSessionId = sessionId;
		renameDraft = sessionItem.name;
		renameDialogOpen = true;
	}

	function closeRenameDialog() {
		renameDialogOpen = false;
		renameSessionId = null;
		renameDraft = "";
		renamingSession = false;
	}

	async function handleRenameSession() {
		if (!renameSessionId || renamingSession) {
			return;
		}
		renamingSession = true;
		const renamed = await sessions.rename(renameSessionId, renameDraft);
		renamingSession = false;
		if (renamed) {
			closeRenameDialog();
		}
	}

	function handleRenameInputKeydown(event: KeyboardEvent) {
		if (event.key === "Enter") {
			event.preventDefault();
			void handleRenameSession();
		}
	}

	function openDeleteDialog(sessionId: string) {
		if (!sessionById(sessionId)) {
			return;
		}
		deleteSessionId = sessionId;
		deleteDialogOpen = true;
	}

	function closeDeleteDialog() {
		deleteDialogOpen = false;
		deleteSessionId = null;
		deletingSession = false;
	}

	async function handleDeleteSession() {
		if (!deleteSessionId || deletingSession) {
			return;
		}
		deletingSession = true;
		const deleted = await sessions.remove(deleteSessionId);
		deletingSession = false;
		if (deleted) {
			closeDeleteDialog();
		}
	}

	function deleteDialogSessionName() {
		if (!deleteSessionId) {
			return "this session";
		}
		return sessionById(deleteSessionId)?.name ?? "this session";
	}

	function isRecentThreadSelected(sessionId: string, threadId: string) {
		return (
			sessions.selectedId === sessionId &&
			(session.threads.selectedId ?? session.sessionId) === threadId
		);
	}

	function shortenHomePath(path: string) {
		return path.replace(/\\/g, "/").replace(/^\/home\/[^/]+/, "~");
	}

	function trimWorkspacePrefix(
		path: string,
		sourceType: Workspace["sourceType"],
	) {
		const normalizedPath = shortenHomePath(path).replace(/\/+$/, "");
		if (sourceType === "git") {
			return normalizedPath
				.replace(/^https?:\/\/github\.com\//i, "")
				.replace(/^ssh:\/\/git@github\.com\//i, "")
				.replace(/^git@github\.com:/i, "")
				.replace(/\.git$/i, "");
		}
		return normalizedPath;
	}

	function workspaceGroupLabel(workspace: Workspace | null) {
		if (!workspace || workspace.sourceType === "managed") {
			return workspace?.displayName?.trim() || "New Workspace";
		}

		return trimWorkspacePrefix(workspace.path, workspace.sourceType);
	}

	function openRenameWorkspaceDialog(workspaceId: string) {
		const workspace = workspaceById(workspaceId);
		if (!workspace) {
			return;
		}
		renameWorkspaceId = workspaceId;
		renameWorkspaceDraft = workspace.displayName ?? "";
		renameWorkspaceDialogOpen = true;
	}

	function closeRenameWorkspaceDialog() {
		renameWorkspaceDialogOpen = false;
		renameWorkspaceId = null;
		renameWorkspaceDraft = "";
		renamingWorkspace = false;
	}

	async function handleRenameWorkspace() {
		if (!renameWorkspaceId || renamingWorkspace) {
			return;
		}
		renamingWorkspace = true;
		await app.workspaces.update(renameWorkspaceId, {
			displayName: renameWorkspaceDraft.trim() || null,
		});
		renamingWorkspace = false;
		closeRenameWorkspaceDialog();
	}

	function openDeleteWorkspaceDialog(workspaceId: string) {
		if (!workspaceById(workspaceId)) {
			return;
		}
		deleteWorkspaceId = workspaceId;
		deleteWorkspaceDialogOpen = true;
	}

	function closeDeleteWorkspaceDialog() {
		deleteWorkspaceDialogOpen = false;
		deleteWorkspaceId = null;
		deletingWorkspace = false;
	}

	async function handleDeleteWorkspace() {
		if (!deleteWorkspaceId || deletingWorkspace) {
			return;
		}
		deletingWorkspace = true;
		await app.workspaces.remove(deleteWorkspaceId);
		deletingWorkspace = false;
		closeDeleteWorkspaceDialog();
	}

	function deleteDialogWorkspaceName() {
		const workspace = workspaceById(deleteWorkspaceId);
		if (!workspace) {
			return "this workspace";
		}
		return workspaceGroupLabel(workspace);
	}

	const workspaceSessionGroups = $derived.by(() => {
		const groups: SessionGroup[] = [];

		for (const sessionObj of sessions.list) {
			const workspace = sessionObj.workspaceId
				? app.workspaces.get(sessionObj.workspaceId)
				: null;
			const sourceType = workspace?.sourceType ?? "managed";
			const key =
				workspace?.id ?? `managed:${sessionObj.workspaceId ?? "none"}`;
			const existingGroup = groups.find((group) => group.key === key);

			if (existingGroup) {
				existingGroup.sessions.push(sessionObj);
				continue;
			}

			groups.push({
				key,
				workspaceId: workspace?.id ?? null,
				label: workspaceGroupLabel(workspace),
				sourceType,
				sessions: [sessionObj],
			});
		}

		return groups;
	});
</script>

{#snippet sessionItem(
	sessionObj: (typeof sessions.list)[number],
	isSelected: boolean,
)}
	<div class="group flex min-w-0 items-center gap-0.5">
		<button
			type="button"
			onclick={() => handleSelectSession(sessionObj.id)}
			class={`flex h-8 min-w-0 flex-1 items-center gap-2 rounded-md px-2 text-sm font-medium transition-colors ${isSelected ? "bg-sidebar-accent text-sidebar-accent-foreground shadow-inner" : "text-sidebar-foreground/80 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"}`}
		>
			<SessionStatus
				status={sessionObj.status}
				showLabel={false}
				class="shrink-0"
			/>
			<span class="truncate">{sessionObj.name || "New Session"}</span>
		</button>

		<DropdownMenu>
			<DropdownMenuTrigger>
				<Button
					variant="ghost"
					size="icon-xs"
					class={`h-8 w-7 rounded-md transition-colors ${isSelected ? "bg-sidebar-accent text-sidebar-accent-foreground shadow-inner hover:bg-sidebar-accent hover:text-sidebar-accent-foreground" : "text-sidebar-foreground/60 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"}`}
					aria-label={`Session actions for ${sessionObj.name || "New Session"}`}
					onclick={(event) => event.stopPropagation()}
				>
					<EllipsisIcon
						class="size-3.5 opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100"
					/>
				</Button>
			</DropdownMenuTrigger>
			<DropdownMenuContent align="end" class="w-32">
				<DropdownMenuItem onclick={() => openRenameDialog(sessionObj.id)}>
					Rename
				</DropdownMenuItem>
				<DropdownMenuItem
					variant="destructive"
					onclick={() => openDeleteDialog(sessionObj.id)}
				>
					Delete
				</DropdownMenuItem>
			</DropdownMenuContent>
		</DropdownMenu>
	</div>
{/snippet}

{#snippet recentThreadItem(
	threadObj: (typeof sessions.recentThreads)[number],
	isSelected: boolean,
)}
	<button
		type="button"
		onclick={() =>
			handleSelectRecentThread(threadObj.sessionId, threadObj.threadId)}
		class={`flex w-full min-w-0 items-start gap-2 overflow-hidden rounded-md px-2 py-1.5 text-left transition-colors ${hasRecentThreadSubtitle(threadObj) ? "min-h-10" : ""} ${isSelected ? "bg-sidebar-accent text-sidebar-accent-foreground shadow-inner" : "text-sidebar-foreground/80 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"}`}
	>
		<SessionStatus
			status={threadObj.sessionStatus}
			showLabel={false}
			class="mt-0.5 shrink-0"
		/>
		<span class="min-w-0 flex-1 overflow-hidden">
			<span class="flex min-w-0 items-center gap-2 overflow-hidden">
				<span class="block truncate text-sm font-medium"
					>{threadObj.threadName || "New Thread"}</span
				>
				{#if recentThreadStateLabel(threadObj)}
					<span
						class={`inline-flex shrink-0 items-center rounded-full border px-1.5 py-0.5 text-[10px] font-medium ${recentThreadStateClass(
							threadObj,
						)}`}
					>
						{recentThreadStateLabel(threadObj)}
					</span>
				{/if}
			</span>
			{#if hasRecentThreadSubtitle(threadObj)}
				<span class="block truncate text-xs text-current/60"
					>{threadObj.lastMessage ?? ""}</span
				>
			{/if}
		</span>
	</button>
{/snippet}

<aside
	bind:this={shellRef}
	class={`flex min-h-0 flex-col overflow-hidden text-sidebar-foreground ${dropdownMode ? "max-h-[min(70vh,32rem)] min-w-64 bg-sidebar" : floatingMode ? `${showSidebarBody ? "max-h-[calc(100vh-7rem)] w-[min(16rem,calc(100vw-1.5rem))] rounded-md border border-sidebar-border bg-sidebar shadow-sm" : "w-fit bg-transparent shadow-none border-transparent"} pointer-events-auto` : "h-full min-w-64 w-full rounded-md border border-sidebar-border bg-sidebar shadow-sm"}`}
>
	<div
		class={`flex h-10 items-center justify-between px-3 ${showSidebarBody ? "border-b border-sidebar-border" : ""}`}
	>
		<div class="flex min-w-0 items-center gap-1">
			{#if onToggleSidebar && !dropdownMode}
				<Button
					variant="ghost"
					size="icon-xs"
					onclick={onToggleSidebar}
					aria-label={floatingMode
						? "Expand sessions panel"
						: "Collapse sessions panel"}
					title={floatingMode
						? "Expand sessions panel"
						: "Collapse sessions panel"}
					class="text-sidebar-foreground/70 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
				>
					<PanelLeftIcon class="size-3.5" />
				</Button>
			{/if}
			{#if floatingCollapsed}
				<button
					type="button"
					onclick={toggleFloatingSidebar}
					aria-expanded={floatingOpen}
					class="inline-flex items-center gap-1 rounded-md py-0 pr-0.5 text-xs font-medium uppercase tracking-[0.16em] text-sidebar-foreground/70 transition-colors hover:text-sidebar-accent-foreground"
				>
					<span>Sessions</span>
					<ChevronDownIcon
						class={`size-3 shrink-0 transition-transform ${floatingOpen ? "rotate-180" : ""}`}
					/>
				</button>
			{:else}
				<p
					class="text-xs font-medium uppercase tracking-[0.16em] text-sidebar-foreground/70"
				>
					Sessions
				</p>
			{/if}
		</div>
		<div class="flex items-center gap-2">
			{#if !dropdownMode && !floatingCollapsed && sessions.list.length > 0}
				<label
					class="flex items-center gap-2 text-xs text-sidebar-foreground/70"
				>
					<span>workspace</span>
					<Switch
						checked={preferences.sidebarAllGroupedByWorkspace}
						class="data-[state=checked]:bg-muted data-[state=unchecked]:bg-muted/70 [&_[data-slot=switch-thumb]]:bg-foreground dark:[&_[data-slot=switch-thumb]]:bg-foreground focus-visible:ring-muted-foreground/20"
						onCheckedChange={(checked) =>
							preferences.setSidebarAllGroupedByWorkspace(checked === true)}
					/>
				</label>
			{/if}
			<Button
				variant="ghost"
				size="icon-xs"
				onclick={handleStartNewSession}
				aria-label="New session"
				title="New session"
				class="text-sidebar-foreground/70 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
			>
				<PlusIcon class="size-3.5" />
			</Button>
		</div>
	</div>

	{#if showSidebarBody}
		<div class="flex-1 overflow-y-auto p-2">
			<div class="space-y-0.5">
				{#if sessions.list.length === 0}
					<p class="px-2 text-xs text-sidebar-foreground/50">No sessions</p>
				{:else}
					{#if sessions.recentThreads.length > 0}
						<Collapsible.Root
							open={preferences.sidebarRecentOpen}
							onOpenChange={(v) => preferences.setSidebarRecentOpen(v)}
						>
							<Collapsible.Trigger
								class="flex w-full items-center gap-1 px-2 pb-1 pt-1 text-xs font-medium uppercase tracking-[0.16em] text-sidebar-foreground/70 transition-colors hover:text-sidebar-accent-foreground"
							>
								{#if preferences.sidebarRecentOpen}
									<ChevronDownIcon class="size-3 shrink-0" />
								{:else}
									<ChevronRightIcon class="size-3 shrink-0" />
								{/if}
								Recent
							</Collapsible.Trigger>
							<Collapsible.Content class="space-y-0.5">
								{#each sessions.recentThreads as threadObj (`${threadObj.sessionId}:${threadObj.threadId}`)}
									{@render recentThreadItem(
										threadObj,
										isRecentThreadSelected(
											threadObj.sessionId,
											threadObj.threadId,
										),
									)}
								{/each}
							</Collapsible.Content>
						</Collapsible.Root>
					{/if}

					{#if sessions.list.length > 0}
						<Collapsible.Root
							open={preferences.sidebarAllOpen}
							onOpenChange={(v) => preferences.setSidebarAllOpen(v)}
						>
							<Collapsible.Trigger
								class="flex w-full items-center gap-1 px-2 pb-1 pt-2 text-xs font-medium uppercase tracking-[0.16em] text-sidebar-foreground/70 transition-colors hover:text-sidebar-accent-foreground"
							>
								{#if preferences.sidebarAllOpen}
									<ChevronDownIcon class="size-3 shrink-0" />
								{:else}
									<ChevronRightIcon class="size-3 shrink-0" />
								{/if}
								All sessions
							</Collapsible.Trigger>
							<Collapsible.Content
								class={preferences.sidebarAllGroupedByWorkspace
									? "space-y-3"
									: "space-y-0.5"}
							>
								{#if preferences.sidebarAllGroupedByWorkspace}
									{#each workspaceSessionGroups as group (group.key)}
										<div class="space-y-1.5">
											<div
												class="group flex items-center gap-1.5 px-2 pt-1 text-xs font-medium uppercase tracking-[0.16em] text-sidebar-foreground/60"
											>
												{#if group.sourceType === "git"}
													<GitBranchIcon class="size-3 shrink-0" />
												{:else if group.sourceType === "local"}
													<FolderIcon class="size-3 shrink-0" />
												{:else}
													<PackageIcon class="size-3 shrink-0" />
												{/if}
												<span class="min-w-0 flex-1 truncate"
													>{group.label}</span
												>
												{#if group.workspaceId}
													<DropdownMenu>
														<DropdownMenuTrigger>
															<Button
																variant="ghost"
																size="icon-xs"
																class="h-6 w-6 rounded-md text-sidebar-foreground/60 transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
																aria-label={`Workspace actions for ${group.label}`}
																onclick={(event) => event.stopPropagation()}
															>
																<EllipsisIcon
																	class="size-3 opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100"
																/>
															</Button>
														</DropdownMenuTrigger>
														<DropdownMenuContent align="end" class="w-32">
															<DropdownMenuItem
																onclick={() =>
																	openRenameWorkspaceDialog(group.workspaceId!)}
															>
																Rename
															</DropdownMenuItem>
															<DropdownMenuItem
																variant="destructive"
																onclick={() =>
																	openDeleteWorkspaceDialog(group.workspaceId!)}
															>
																Delete
															</DropdownMenuItem>
														</DropdownMenuContent>
													</DropdownMenu>
												{/if}
											</div>
											<div class="space-y-0.5">
												{#each group.sessions as sessionObj (sessionObj.id)}
													{@render sessionItem(
														sessionObj,
														sessions.selectedId === sessionObj.id,
													)}
												{/each}
											</div>
										</div>
									{/each}
								{:else}
									{#each sessions.list as sessionObj (sessionObj.id)}
										{@render sessionItem(
											sessionObj,
											sessions.selectedId === sessionObj.id,
										)}
									{/each}
								{/if}
							</Collapsible.Content>
						</Collapsible.Root>
					{/if}
				{/if}
			</div>
		</div>
	{/if}

	<Dialog.Root bind:open={renameDialogOpen}>
		<Dialog.Content class="sm:max-w-md">
			<Dialog.Header>
				<Dialog.Title>Rename session</Dialog.Title>
				<Dialog.Description
					>Choose a new name for this session.</Dialog.Description
				>
			</Dialog.Header>
			<Input
				value={renameDraft}
				oninput={(event) => {
					renameDraft = (event.currentTarget as HTMLInputElement).value;
				}}
				onkeydown={handleRenameInputKeydown}
				maxlength={120}
				placeholder="Session name"
			/>
			<Dialog.Footer>
				<Button
					variant="ghost"
					size="sm"
					onclick={closeRenameDialog}
					disabled={renamingSession}
				>
					Cancel
				</Button>
				<Button
					variant="default"
					size="sm"
					onclick={() => {
						void handleRenameSession();
					}}
					disabled={renamingSession || renameDraft.trim().length === 0}
				>
					Save
				</Button>
			</Dialog.Footer>
		</Dialog.Content>
	</Dialog.Root>

	<Dialog.Root bind:open={renameWorkspaceDialogOpen}>
		<Dialog.Content class="sm:max-w-md">
			<Dialog.Header>
				<Dialog.Title>Rename workspace</Dialog.Title>
				<Dialog.Description
					>Choose a new name for this workspace.</Dialog.Description
				>
			</Dialog.Header>
			<Input
				value={renameWorkspaceDraft}
				oninput={(event) => {
					renameWorkspaceDraft = (event.currentTarget as HTMLInputElement)
						.value;
				}}
				maxlength={120}
				placeholder="Workspace name"
			/>
			<Dialog.Footer>
				<Button
					variant="ghost"
					size="sm"
					onclick={closeRenameWorkspaceDialog}
					disabled={renamingWorkspace}
				>
					Cancel
				</Button>
				<Button
					variant="default"
					size="sm"
					onclick={() => {
						void handleRenameWorkspace();
					}}
					disabled={renamingWorkspace}
				>
					Save
				</Button>
			</Dialog.Footer>
		</Dialog.Content>
	</Dialog.Root>

	<AlertDialog bind:open={deleteDialogOpen}>
		<AlertDialogContent>
			<AlertDialogHeader>
				<AlertDialogTitle>Delete session?</AlertDialogTitle>
				<AlertDialogDescription>
					Delete "{deleteDialogSessionName()}"? This action cannot be undone.
				</AlertDialogDescription>
			</AlertDialogHeader>
			<AlertDialogFooter>
				<AlertDialogCancel
					onclick={closeDeleteDialog}
					disabled={deletingSession}
				>
					Cancel
				</AlertDialogCancel>
				<AlertDialogAction
					onclick={() => {
						void handleDeleteSession();
					}}
					disabled={deletingSession}
				>
					Delete
				</AlertDialogAction>
			</AlertDialogFooter>
		</AlertDialogContent>
	</AlertDialog>

	<AlertDialog bind:open={deleteWorkspaceDialogOpen}>
		<AlertDialogContent>
			<AlertDialogHeader>
				<AlertDialogTitle>Delete workspace?</AlertDialogTitle>
				<AlertDialogDescription>
					Delete "{deleteDialogWorkspaceName()}"? This action cannot be undone.
				</AlertDialogDescription>
			</AlertDialogHeader>
			<AlertDialogFooter>
				<AlertDialogCancel
					onclick={closeDeleteWorkspaceDialog}
					disabled={deletingWorkspace}
				>
					Cancel
				</AlertDialogCancel>
				<AlertDialogAction
					onclick={() => {
						void handleDeleteWorkspace();
					}}
					disabled={deletingWorkspace}
				>
					Delete
				</AlertDialogAction>
			</AlertDialogFooter>
		</AlertDialogContent>
	</AlertDialog>
</aside>
