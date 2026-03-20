<script lang="ts">
	import MoonIcon from "@lucide/svelte/icons/moon";
	import SettingsIcon from "@lucide/svelte/icons/settings";
	import SunIcon from "@lucide/svelte/icons/sun";
	import DiscobotBrand from "$lib/components/ide/DiscobotBrand.svelte";
	import LeftWindowControls from "$lib/components/ide/LeftWindowControls.svelte";
	import RightWindowControls from "$lib/components/ide/RightWindowControls.svelte";
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
			<LeftWindowControls />
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
			<RightWindowControls />
		{/if}
	</div>

	<SettingsDialog />
</header>
