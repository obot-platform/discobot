<script lang="ts">
	import CircleCheckIcon from "@lucide/svelte/icons/circle-check";
	import CircleIcon from "@lucide/svelte/icons/circle";
	import ChevronDownIcon from "@lucide/svelte/icons/chevron-down";
	import EllipsisIcon from "@lucide/svelte/icons/ellipsis";
	import Loader2Icon from "@lucide/svelte/icons/loader-2";
	import MoonIcon from "@lucide/svelte/icons/moon";
	import SettingsIcon from "@lucide/svelte/icons/settings";
	import SunIcon from "@lucide/svelte/icons/sun";
	import DiscobotBrand from "$lib/components/ide/DiscobotBrand.svelte";
	import SettingsDialog from "$lib/components/ide/SettingsDialog.svelte";
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
		DropdownMenuLabel,
		DropdownMenuSeparator,
		DropdownMenuTrigger,
	} from "$lib/components/ui/dropdown-menu";
	import { Input } from "$lib/components/ui/input";
	import { useAppContext } from "$lib/context/app-context.svelte";
	import { useSessionContext } from "$lib/context/session-context.svelte";
	import type { SessionRuntimeStatus } from "$lib/shell-types";

	const app = useAppContext();
	const session = useSessionContext();
	let renameDialogOpen = $state(false);
	let renameSessionId = $state<string | null>(null);
	let renameDraft = $state("");
	let renamingSession = $state(false);
	let deleteDialogOpen = $state(false);
	let deleteSessionId = $state<string | null>(null);
	let deletingSession = $state(false);

	function normalizedStatus(status: SessionRuntimeStatus): string {
		return status.toLowerCase();
	}

	function statusLabel(status: SessionRuntimeStatus): string {
		return status
			.replace(/_/g, " ")
			.replace(/\b\w/g, (char) => char.toUpperCase());
	}

	function statusTone(status: SessionRuntimeStatus): string {
		switch (normalizedStatus(status)) {
			case "error":
				return "text-destructive";
			case "ready":
				return "text-green-500";
			case "running":
				return "text-blue-500";
			case "initializing":
			case "reinitializing":
			case "cloning":
			case "pulling_image":
			case "creating_sandbox":
				return "text-yellow-500";
			case "removing":
				return "text-orange-500";
			default:
				return "text-muted-foreground";
		}
	}

	function isSpinningStatus(status: SessionRuntimeStatus): boolean {
		switch (normalizedStatus(status)) {
			case "running":
			case "initializing":
			case "reinitializing":
			case "cloning":
			case "pulling_image":
			case "creating_sandbox":
			case "removing":
				return true;
			default:
				return false;
		}
	}

	function nonRecentSessions() {
		return app.sessions.filter((sessionItem) => !sessionItem.isRecent);
	}

	function sessionById(sessionId: string) {
		return app.sessions.find((sessionItem) => sessionItem.id === sessionId) ?? null;
	}

	function handleSelectSession(sessionId: string) {
		app.selectSession(sessionId);
	}

	function handleStartNewSession() {
		app.startNewSession();
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
		const renamed = await app.renameSession(renameSessionId, renameDraft);
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
		const deleted = await app.deleteSession(deleteSessionId);
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

	function showMacSpacer(): boolean {
		return app.isTauri && app.windowControlsSide === "left";
	}

	function showWindowsLinuxControls(): boolean {
		return app.isTauri && app.windowControlsSide === "right";
	}
</script>

<header class="relative z-[60] flex h-12 items-center justify-between border-b border-border bg-background" data-tauri-drag-region>
	<div class="absolute inset-0 pointer-events-auto" data-tauri-drag-region></div>

	<div class="relative flex min-w-0 items-center gap-2 px-3">
		{#if showMacSpacer()}
			<div class="w-14 shrink-0"></div>
		{/if}

		<DiscobotBrand textSizeClass="text-sm" />

		<DropdownMenu>
			<DropdownMenuTrigger class="tauri-no-drag">
				<Button variant="ghost" size="sm" class="h-8 gap-1.5">
					<span class="max-w-[14rem] truncate">
						{session.current?.name ?? app.selectedSession?.name ?? "No session"}
					</span>
					<ChevronDownIcon class="size-3.5 opacity-70" />
				</Button>
			</DropdownMenuTrigger>
			<DropdownMenuContent align="start" class="w-80">
				<DropdownMenuItem onclick={handleStartNewSession}>New session</DropdownMenuItem>
				<DropdownMenuSeparator />
				<DropdownMenuLabel class="text-xs uppercase tracking-[0.16em] text-muted-foreground">
					Recent sessions
				</DropdownMenuLabel>
				{#if app.recentSessions.length === 0}
					<DropdownMenuItem disabled class="text-muted-foreground">
						No recent sessions
					</DropdownMenuItem>
				{:else}
					{#each app.recentSessions as sessionItem}
						<DropdownMenu>
							<DropdownMenuItem
								onclick={() => handleSelectSession(sessionItem.id)}
								class={`group h-8 justify-between gap-3 ${app.selectedSessionId === sessionItem.id ? "bg-accent" : ""}`}
							>
								<span class="truncate">{sessionItem.name}</span>
								<span class="relative inline-flex size-4 items-center justify-center">
									<span
										class={`inline-flex items-center transition-opacity duration-150 group-hover:opacity-0 group-focus-within:opacity-0 ${statusTone(sessionItem.status)}`}
										title={statusLabel(sessionItem.status)}
										aria-label={statusLabel(sessionItem.status)}
									>
										{#if isSpinningStatus(sessionItem.status)}
											<Loader2Icon class="size-3.5 animate-spin" />
										{:else if normalizedStatus(sessionItem.status) === "ready"}
											<CircleCheckIcon class="size-3.5" />
										{:else}
											<CircleIcon class="size-2.5 fill-current" />
										{/if}
									</span>
									<DropdownMenuTrigger class="tauri-no-drag absolute inset-0">
										<Button
											variant="ghost"
											size="icon-xs"
											class="size-4 p-0 opacity-0 transition-opacity duration-150 group-hover:opacity-100 group-focus-within:opacity-100"
											aria-label={`Session actions for ${sessionItem.name}`}
											onclick={(event) => {
												event.stopPropagation();
											}}
										>
											<EllipsisIcon class="size-3.5" />
										</Button>
									</DropdownMenuTrigger>
								</span>
							</DropdownMenuItem>
							<DropdownMenuContent align="end" class="w-36">
								<DropdownMenuItem onclick={() => openRenameDialog(sessionItem.id)}>
									Rename
								</DropdownMenuItem>
								<DropdownMenuItem variant="destructive" onclick={() => openDeleteDialog(sessionItem.id)}>
									Delete
								</DropdownMenuItem>
							</DropdownMenuContent>
						</DropdownMenu>
					{/each}
				{/if}
				<DropdownMenuSeparator />
				<DropdownMenuLabel class="text-xs uppercase tracking-[0.16em] text-muted-foreground">
					All sessions
				</DropdownMenuLabel>
				{#each nonRecentSessions() as sessionItem}
					<DropdownMenu>
						<DropdownMenuItem
							onclick={() => handleSelectSession(sessionItem.id)}
							class={`group h-8 justify-between gap-3 ${app.selectedSessionId === sessionItem.id ? "bg-accent" : ""}`}
						>
							<span class="truncate">{sessionItem.name}</span>
							<span class="relative inline-flex size-4 items-center justify-center">
								<span
									class={`inline-flex items-center transition-opacity duration-150 group-hover:opacity-0 group-focus-within:opacity-0 ${statusTone(sessionItem.status)}`}
									title={statusLabel(sessionItem.status)}
									aria-label={statusLabel(sessionItem.status)}
								>
									{#if isSpinningStatus(sessionItem.status)}
										<Loader2Icon class="size-3.5 animate-spin" />
									{:else if normalizedStatus(sessionItem.status) === "ready"}
										<CircleCheckIcon class="size-3.5" />
									{:else}
										<CircleIcon class="size-2.5 fill-current" />
									{/if}
								</span>
								<DropdownMenuTrigger class="tauri-no-drag absolute inset-0">
									<Button
										variant="ghost"
										size="icon-xs"
										class="size-4 p-0 opacity-0 transition-opacity duration-150 group-hover:opacity-100 group-focus-within:opacity-100"
										aria-label={`Session actions for ${sessionItem.name}`}
										onclick={(event) => {
											event.stopPropagation();
										}}
									>
										<EllipsisIcon class="size-3.5" />
									</Button>
								</DropdownMenuTrigger>
							</span>
						</DropdownMenuItem>
						<DropdownMenuContent align="end" class="w-36">
							<DropdownMenuItem onclick={() => openRenameDialog(sessionItem.id)}>
								Rename
							</DropdownMenuItem>
							<DropdownMenuItem variant="destructive" onclick={() => openDeleteDialog(sessionItem.id)}>
								Delete
							</DropdownMenuItem>
						</DropdownMenuContent>
					</DropdownMenu>
				{/each}
			</DropdownMenuContent>
		</DropdownMenu>

		<div class="tauri-no-drag flex flex-wrap items-center gap-1">
			{#each app.workflowActions as action, index (action + index)}
				<Button
					variant="ghost"
					size="xs"
					class="h-7 px-2 text-xs"
					disabled={!app.selectedSessionId}
				>
					{action}
				</Button>
			{/each}
		</div>
	</div>

	<div class="relative flex items-center gap-1 pr-2">
		<Button
			variant="ghost"
			size="icon-sm"
			onclick={app.toggleTheme}
			aria-label={
				app.resolvedTheme === "dark" ? "Switch to light theme" : "Switch to dark theme"
			}
			title={
				app.resolvedTheme === "dark" ? "Switch to light theme" : "Switch to dark theme"
			}
		>
			{#if app.resolvedTheme === "dark"}
				<SunIcon class="size-4" />
			{:else}
				<MoonIcon class="size-4" />
			{/if}
		</Button>
		<Button
			variant="ghost"
			size="icon-sm"
			onclick={app.openSettingsDialog}
			aria-label="Settings"
			title="Settings"
			class="relative"
		>
			<SettingsIcon class="size-4" />
			{#if app.showUpdateBadge}
				<span class="absolute right-1 top-1 h-2 w-2 rounded-full bg-blue-500"></span>
			{/if}
		</Button>

		{#if showWindowsLinuxControls()}
			<div class="tauri-no-drag flex h-full items-stretch -mr-2">
				<button
					type="button"
					class="tauri-no-drag flex h-full w-[46px] items-center justify-center bg-transparent text-foreground transition-colors duration-150 hover:bg-foreground/10"
					aria-label="Minimize"
				>
					<svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
						<path d="M0 5H10" stroke="currentColor" stroke-width="1" />
					</svg>
				</button>
				<button
					type="button"
					class="tauri-no-drag flex h-full w-[46px] items-center justify-center bg-transparent text-foreground transition-colors duration-150 hover:bg-foreground/10"
					aria-label="Maximize"
				>
					<svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
						<rect x="0.5" y="0.5" width="9" height="9" stroke="currentColor" fill="none" />
					</svg>
				</button>
				<button
					type="button"
					class="tauri-no-drag flex h-full w-[46px] items-center justify-center bg-transparent text-foreground transition-colors duration-150 hover:bg-[#e81123] hover:text-white"
					aria-label="Close"
				>
					<svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
						<path d="M0 0L10 10M10 0L0 10" stroke="currentColor" stroke-width="1" />
					</svg>
				</button>
			</div>
		{/if}
	</div>

	<SettingsDialog />

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
</header>
