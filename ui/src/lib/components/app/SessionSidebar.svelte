<script lang="ts">
	import ChevronDownIcon from "@lucide/svelte/icons/chevron-down";
	import ChevronRightIcon from "@lucide/svelte/icons/chevron-right";
	import EllipsisIcon from "@lucide/svelte/icons/ellipsis";
	import PanelLeftIcon from "@lucide/svelte/icons/panel-left";
	import PlusIcon from "@lucide/svelte/icons/plus";
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
	};

	let { onThreadSelect, onToggleSidebar }: Props = $props();

	const app = useAppContext();
	const sessions = app.sessions;
	const preferences = app.preferences;
	const session = useSessionContext();

	let renameDialogOpen = $state(false);
	let renameSessionId = $state<string | null>(null);
	let renameDraft = $state("");
	let renamingSession = $state(false);
	let deleteDialogOpen = $state(false);
	let deleteSessionId = $state<string | null>(null);
	let deletingSession = $state(false);

	function sessionById(sessionId: string) {
		return sessions.list.find((s) => s.id === sessionId) ?? null;
	}

	function handleSelectSession(sessionId: string) {
		sessions.select(sessionId);
		onThreadSelect?.();
	}

	function handleSelectRecentThread(sessionId: string, threadId: string) {
		sessions.openThread(sessionId, threadId);
		onThreadSelect?.();
	}

	function hasRecentThreadSubtitle(
		threadObj: (typeof sessions.recentThreads)[number],
	) {
		return (threadObj.lastMessage ?? "").trim().length > 0;
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
		class={`flex w-full items-start gap-2 rounded-md px-2 py-1.5 text-left transition-colors ${hasRecentThreadSubtitle(threadObj) ? "min-h-10" : ""} ${isSelected ? "bg-sidebar-accent text-sidebar-accent-foreground shadow-inner" : "text-sidebar-foreground/80 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"}`}
	>
		<SessionStatus
			status={threadObj.sessionStatus}
			showLabel={false}
			class="mt-0.5 shrink-0"
		/>
		<span class="min-w-0 flex-1">
			<span class="block truncate text-sm font-medium"
				>{threadObj.threadName || "New Thread"}</span
			>
			{#if hasRecentThreadSubtitle(threadObj)}
				<span class="block truncate text-xs text-current/60"
					>{threadObj.lastMessage ?? ""}</span
				>
			{/if}
		</span>
	</button>
{/snippet}

<aside
	class="flex h-full min-h-0 w-full flex-col overflow-hidden rounded-md border border-sidebar-border bg-sidebar text-sidebar-foreground shadow-sm"
>
	<div
		class="flex h-10 items-center justify-between border-b border-sidebar-border px-3"
	>
		<div class="flex min-w-0 items-center gap-1">
			{#if onToggleSidebar}
				<Button
					variant="ghost"
					size="icon-xs"
					onclick={onToggleSidebar}
					aria-label="Collapse sessions panel"
					title="Collapse sessions panel"
					class="text-sidebar-foreground/70 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
				>
					<PanelLeftIcon class="size-3.5" />
				</Button>
			{/if}
			<p
				class="text-xs font-medium uppercase tracking-[0.16em] text-sidebar-foreground/70"
			>
				Sessions
			</p>
		</div>
		<Button
			variant="ghost"
			size="icon-xs"
			onclick={() => sessions.startNew()}
			aria-label="New session"
			title="New session"
			class="text-sidebar-foreground/70 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
		>
			<PlusIcon class="size-3.5" />
		</Button>
	</div>

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
							{#each sessions.recentThreads as threadObj}
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
						<Collapsible.Content class="space-y-0.5">
							{#each sessions.list as sessionObj}
								{@render sessionItem(
									sessionObj,
									sessions.selectedId === sessionObj.id,
								)}
							{/each}
						</Collapsible.Content>
					</Collapsible.Root>
				{/if}
			{/if}
		</div>
	</div>

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
</aside>
