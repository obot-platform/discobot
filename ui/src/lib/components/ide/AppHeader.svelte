<script lang="ts">
	import CircleCheckIcon from "@lucide/svelte/icons/circle-check";
	import CircleIcon from "@lucide/svelte/icons/circle";
	import ChevronDownIcon from "@lucide/svelte/icons/chevron-down";
	import Loader2Icon from "@lucide/svelte/icons/loader-2";
	import MoonIcon from "@lucide/svelte/icons/moon";
	import SettingsIcon from "@lucide/svelte/icons/settings";
	import SunIcon from "@lucide/svelte/icons/sun";
	import DiscobotBrand from "$lib/components/ide/DiscobotBrand.svelte";
	import SettingsDialog from "$lib/components/ide/SettingsDialog.svelte";
	import { Button } from "$lib/components/ui/button";
	import {
		DropdownMenu,
		DropdownMenuContent,
		DropdownMenuItem,
		DropdownMenuLabel,
		DropdownMenuSeparator,
		DropdownMenuTrigger,
	} from "$lib/components/ui/dropdown-menu";
	import { useAppContext } from "$lib/context/app-context.svelte";
	import { useSessionContext } from "$lib/context/session-context.svelte";
	import type { SessionRuntimeStatus } from "$lib/shell-types";

	const app = useAppContext();
	const session = useSessionContext();

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

	function handleSelectSession(sessionId: string) {
		app.selectSession(sessionId);
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
				<DropdownMenuItem onclick={app.startNewSession}>New session</DropdownMenuItem>
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
						<DropdownMenuItem
							onclick={() => handleSelectSession(sessionItem.id)}
							class={`justify-between gap-3 ${app.selectedSessionId === sessionItem.id ? "bg-accent" : ""}`}
						>
							<span class="truncate">{sessionItem.name}</span>
							<span
								class={`inline-flex items-center ${statusTone(sessionItem.status)}`}
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
						</DropdownMenuItem>
					{/each}
				{/if}
				<DropdownMenuSeparator />
				<DropdownMenuLabel class="text-xs uppercase tracking-[0.16em] text-muted-foreground">
					All sessions
				</DropdownMenuLabel>
				{#each nonRecentSessions() as sessionItem}
					<DropdownMenuItem
						onclick={() => handleSelectSession(sessionItem.id)}
						class={`justify-between gap-3 ${app.selectedSessionId === sessionItem.id ? "bg-accent" : ""}`}
					>
						<span class="truncate">{sessionItem.name}</span>
						<span
							class={`inline-flex items-center ${statusTone(sessionItem.status)}`}
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
					</DropdownMenuItem>
				{/each}
			</DropdownMenuContent>
		</DropdownMenu>
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
</header>
