<script lang="ts">
	import ChevronDownIcon from "@lucide/svelte/icons/chevron-down";
	import ChevronRightIcon from "@lucide/svelte/icons/chevron-right";
	import EllipsisIcon from "@lucide/svelte/icons/ellipsis";
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

	type Props = {
		onThreadSelect?: () => void;
	};

	let { onThreadSelect }: Props = $props();

	const app = useAppContext();
	const sessions = app.sessions;
	const preferences = app.preferences;

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
</script>

{#snippet sessionItem(sessionObj: (typeof sessions.list)[number], isSelected: boolean)}
	<div class="group flex min-w-0 items-center">
		<button
			type="button"
			onclick={() => handleSelectSession(sessionObj.id)}
			class={`flex min-w-0 flex-1 items-center gap-2 rounded-l-md rounded-r-none px-2 h-8 text-sm font-medium transition-colors ${isSelected ? "bg-secondary text-foreground" : "text-muted-foreground hover:bg-accent hover:text-accent-foreground"}`}
		>
			<SessionStatus status={sessionObj.status} showLabel={false} class="shrink-0" />
			<span class="truncate">{sessionObj.name || "New Session"}</span>
		</button>

		<DropdownMenu>
			<DropdownMenuTrigger>
				<Button
					variant={isSelected ? "secondary" : "ghost"}
					size="icon-xs"
					class={`h-8 w-6 rounded-l-none transition-colors ${isSelected ? "text-foreground" : "text-muted-foreground group-hover:bg-accent group-hover:text-accent-foreground"}`}
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
				<DropdownMenuItem variant="destructive" onclick={() => openDeleteDialog(sessionObj.id)}>
					Delete
				</DropdownMenuItem>
			</DropdownMenuContent>
		</DropdownMenu>
	</div>
{/snippet}

<aside class="h-full w-full border-r border-border bg-sidebar flex min-h-0 flex-col">
	<div class="flex h-10 items-center justify-between border-b border-border px-3">
		<p class="text-sm font-medium">Sessions</p>
		<Button
			variant="ghost"
			size="icon-xs"
			onclick={() => sessions.startNew()}
			aria-label="New session"
			title="New session"
		>
			<PlusIcon class="size-3.5" />
		</Button>
	</div>

	<div class="flex-1 overflow-y-auto p-2">
		<div class="space-y-0.5">
			{#if sessions.list.length === 0}
				<p class="px-2 text-xs text-muted-foreground">No sessions</p>
			{:else}
				{#if sessions.recent.length > 0}
					<Collapsible.Root
						open={preferences.sidebarRecentOpen}
						onOpenChange={(v) => preferences.setSidebarRecentOpen(v)}
					>
						<Collapsible.Trigger class="flex w-full items-center gap-1 px-2 pb-1 pt-1 text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground hover:text-foreground transition-colors">
							{#if preferences.sidebarRecentOpen}
								<ChevronDownIcon class="size-3 shrink-0" />
							{:else}
								<ChevronRightIcon class="size-3 shrink-0" />
							{/if}
							Recent
						</Collapsible.Trigger>
						<Collapsible.Content class="space-y-0.5">
							{#each sessions.recent as sessionObj}
								{@render sessionItem(sessionObj, sessions.selectedId === sessionObj.id)}
							{/each}
						</Collapsible.Content>
					</Collapsible.Root>
				{/if}

				{#if sessions.list.length > 0}
					<Collapsible.Root
						open={preferences.sidebarAllOpen}
						onOpenChange={(v) => preferences.setSidebarAllOpen(v)}
					>
						<Collapsible.Trigger class="flex w-full items-center gap-1 px-2 pb-1 pt-2 text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground hover:text-foreground transition-colors">
							{#if preferences.sidebarAllOpen}
								<ChevronDownIcon class="size-3 shrink-0" />
							{:else}
								<ChevronRightIcon class="size-3 shrink-0" />
							{/if}
							All sessions
						</Collapsible.Trigger>
						<Collapsible.Content class="space-y-0.5">
							{#each sessions.list as sessionObj}
								{@render sessionItem(sessionObj, sessions.selectedId === sessionObj.id)}
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
				<Dialog.Description>Choose a new name for this session.</Dialog.Description>
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
				<Button variant="ghost" size="sm" onclick={closeRenameDialog} disabled={renamingSession}>
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
				<AlertDialogCancel onclick={closeDeleteDialog} disabled={deletingSession}>
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
