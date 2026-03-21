<script lang="ts">
	import MoonIcon from "@lucide/svelte/icons/moon";
	import SettingsIcon from "@lucide/svelte/icons/settings";
	import SunIcon from "@lucide/svelte/icons/sun";
	import DiscobotBrand from "$lib/components/app/parts/DiscobotBrand.svelte";
	import LeftWindowControls from "$lib/components/app/parts/LeftWindowControls.svelte";
	import RightWindowControls from "$lib/components/app/parts/RightWindowControls.svelte";
	import SessionToolbar from "$lib/components/app/SessionToolbar.svelte";
	import SettingsDialog from "$lib/components/app/SettingsDialog.svelte";
	import { Button } from "$lib/components/ui/button";
	import { useAppContext } from "$lib/context/app-context.svelte";

	type Props = {
		showSessionToolbar?: boolean;
	};

	let { showSessionToolbar = true }: Props = $props();

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

<header
	class="relative z-[60] grid h-12 grid-cols-[auto_minmax(0,1fr)_auto] items-center border-b border-border bg-background"
	data-tauri-drag-region
>
	<div class="absolute inset-0 pointer-events-auto" data-tauri-drag-region></div>

	<div class="relative z-20 flex min-w-0 items-center gap-2 px-3">
		{#if showMacSpacer()}
			<LeftWindowControls />
		{/if}

		<DiscobotBrand textSizeClass="text-sm" />

		<div class="tauri-no-drag flex min-w-0 flex-wrap items-center gap-1">
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

	{#if showSessionToolbar}
		<div class="relative z-20 min-w-0 px-2">
			<div class="tauri-no-drag min-w-0">
				<SessionToolbar />
			</div>
		</div>
	{/if}

	<div class="relative z-20 flex min-w-0 items-center justify-self-end gap-1 pr-2">
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
