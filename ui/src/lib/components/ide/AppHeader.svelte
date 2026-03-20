<script lang="ts">
	import MoonIcon from "@lucide/svelte/icons/moon";
	import SettingsIcon from "@lucide/svelte/icons/settings";
	import SunIcon from "@lucide/svelte/icons/sun";
	import DiscobotBrand from "$lib/components/ide/DiscobotBrand.svelte";
	import SettingsDialog from "$lib/components/ide/SettingsDialog.svelte";
	import { Button } from "$lib/components/ui/button";
	import { useAppContext } from "$lib/context/app-context.svelte";

	const app = useAppContext();
	const environment = app.environment;
	const preferences = app.preferences;
	const sessions = app.sessions;
	const ui = app.ui;
	const updates = app.updates;

	function showMacSpacer(): boolean {
		return environment.isTauri && environment.windowControlsSide === "left";
	}

	function showWindowsLinuxControls(): boolean {
		return environment.isTauri && environment.windowControlsSide === "right";
	}
</script>

<header class="relative z-[60] flex h-12 items-center justify-between border-b border-border bg-background" data-tauri-drag-region>
	<div class="absolute inset-0 pointer-events-auto" data-tauri-drag-region></div>

	<div class="relative flex min-w-0 items-center gap-2 px-3">
		{#if showMacSpacer()}
			<div class="w-14 shrink-0"></div>
		{/if}

		<DiscobotBrand textSizeClass="text-sm" />

		<div class="tauri-no-drag flex flex-wrap items-center gap-1">
			{#each environment.workflowActions as action, index (action + index)}
				<Button
					variant="ghost"
					size="xs"
					class="h-7 px-2 text-xs"
					disabled={!sessions.selectedId}
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
			onclick={preferences.toggleTheme}
			aria-label={
				preferences.resolvedTheme === "dark" ? "Switch to light theme" : "Switch to dark theme"
			}
			title={
				preferences.resolvedTheme === "dark" ? "Switch to light theme" : "Switch to dark theme"
			}
		>
			{#if preferences.resolvedTheme === "dark"}
				<SunIcon class="size-4" />
			{:else}
				<MoonIcon class="size-4" />
			{/if}
		</Button>
		<Button
			variant="ghost"
			size="icon-sm"
			onclick={() => ui.openSettings()}
			aria-label="Settings"
			title="Settings"
			class="relative"
		>
			<SettingsIcon class="size-4" />
			{#if updates.showBadge}
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
