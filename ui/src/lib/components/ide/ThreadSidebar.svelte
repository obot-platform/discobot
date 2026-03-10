<script lang="ts">
	import EllipsisIcon from "@lucide/svelte/icons/ellipsis";
	import { tick } from "svelte";
	import { Button } from "$lib/components/ui/button";
	import {
		DropdownMenu,
		DropdownMenuContent,
		DropdownMenuItem,
		DropdownMenuTrigger,
	} from "$lib/components/ui/dropdown-menu";
	import { Input } from "$lib/components/ui/input";
	import { useSessionContext } from "$lib/context/session-context.svelte";

	type Props = {
		onThreadSelect?: () => void;
	};

	let { onThreadSelect }: Props = $props();

	const session = useSessionContext();
	let editingThreadId = $state<string | null>(null);
	let renameDraft = $state("");
	let renameInputRef = $state<HTMLInputElement | null>(null);

	async function startRename(threadId: string, currentName: string) {
		editingThreadId = threadId;
		renameDraft = currentName;
		await tick();
		renameInputRef?.focus();
		renameInputRef?.select();
	}

	function cancelRename() {
		editingThreadId = null;
		renameDraft = "";
	}

	function commitRename() {
		if (!editingThreadId) {
			return;
		}

		const thread = session.threads.list.find((item) => item.id === editingThreadId);
		if (!thread) {
			cancelRename();
			return;
		}

		const trimmedName = renameDraft.trim();
		if (trimmedName && trimmedName !== thread.name) {
			session.threads.rename(editingThreadId, trimmedName);
		}

		cancelRename();
	}

	function handleRenameKeydown(event: KeyboardEvent) {
		if (event.key === "Enter") {
			event.preventDefault();
			commitRename();
			return;
		}

		if (event.key === "Escape") {
			event.preventDefault();
			cancelRename();
		}
	}

	function handleDeleteThread(threadId: string) {
		if (editingThreadId === threadId) {
			cancelRename();
		}
		session.threads.remove(threadId);
	}
</script>

<aside class="h-full w-full border-r border-border bg-sidebar flex min-h-0 flex-col">
	<div class="flex h-10 items-center border-b border-border px-3">
		<p class="text-sm font-medium">Threads</p>
	</div>

	<div class="flex-1 overflow-y-auto p-2">
		<div class="space-y-1.5">
			{#if session.threads.list.length === 0}
				<p class="px-2 text-xs text-muted-foreground">No threads</p>
			{:else}
				{#each session.threads.list as thread}
					<div class="group flex items-center">
						{#if editingThreadId === thread.id}
							<Input
								bind:ref={renameInputRef}
								value={renameDraft}
								oninput={(event) => {
									renameDraft = (event.currentTarget as HTMLInputElement).value;
								}}
								onkeydown={handleRenameKeydown}
								onblur={commitRename}
								class="h-8 flex-1 rounded-r-none px-2 text-sm"
								maxlength={120}
							/>
						{:else}
							<Button
								variant={session.threads.selectedId === thread.id ? "secondary" : "ghost"}
								size="sm"
								onclick={() => {
									session.threads.select(thread.id);
									onThreadSelect?.();
								}}
								class={`h-8 flex-1 justify-start rounded-r-none px-2 text-left ${session.threads.selectedId === thread.id ? "text-foreground" : "text-muted-foreground group-hover:bg-accent group-hover:text-accent-foreground"}`}
							>
								<span class="truncate">{thread.name}</span>
							</Button>
						{/if}

						<DropdownMenu>
							<DropdownMenuTrigger class="tauri-no-drag">
								<Button
									variant={session.threads.selectedId === thread.id ? "secondary" : "ghost"}
									size="icon-xs"
									class={`h-8 w-6 rounded-l-none transition-colors ${session.threads.selectedId === thread.id ? "text-foreground" : "text-muted-foreground group-hover:bg-accent group-hover:text-accent-foreground"}`}
									aria-label={`Thread actions for ${thread.name}`}
								>
									<EllipsisIcon class="size-3.5 opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100" />
								</Button>
							</DropdownMenuTrigger>
							<DropdownMenuContent align="end" class="w-32">
								<DropdownMenuItem onclick={() => startRename(thread.id, thread.name)}>
									Rename
								</DropdownMenuItem>
								<DropdownMenuItem
									variant="destructive"
									onclick={() => handleDeleteThread(thread.id)}
								>
									Delete
								</DropdownMenuItem>
							</DropdownMenuContent>
						</DropdownMenu>
					</div>
				{/each}
			{/if}
		</div>
	</div>
</aside>
