<script lang="ts">
	import PanelLeftIcon from "@lucide/svelte/icons/panel-left";
	import PlusIcon from "@lucide/svelte/icons/plus";
	import RefreshCwIcon from "@lucide/svelte/icons/refresh-cw";
	import SettingsIcon from "@lucide/svelte/icons/settings";
	import AppMacWindowSpacer from "$lib/components/app/AppMacWindowSpacer.svelte";
	import DiscobotBrand from "$lib/components/app/parts/DiscobotBrand.svelte";
	import DiscobotLogo from "$lib/components/app/parts/DiscobotLogo.svelte";
	import RightWindowControls from "$lib/components/app/parts/RightWindowControls.svelte";
	import SessionToolbarStack from "$lib/components/app/SessionToolbarStack.svelte";
	import SettingsDialog from "$lib/components/app/SettingsDialog.svelte";
	import { Button } from "$lib/components/ui/button";
	import { useAppContext } from "$lib/context/app-context.svelte";
	import { IsMobile } from "$lib/hooks/is-mobile.svelte.js";

	type Props = {
		showSessionToolbar?: boolean;
		onToggleSidebar?: () => void;
	};

	let { showSessionToolbar = true, onToggleSidebar }: Props = $props();

	const app = useAppContext();
	const environment = app.environment;
	const sessions = app.sessions;
	const ui = app.ui;
	const updates = app.updates;
	const preferences = app.preferences;
	const isMobile = new IsMobile(1024);

	function showWindowsLinuxControls(): boolean {
		return (
			!isMobile.current &&
			environment.supportsNativeWindowControls &&
			environment.windowControlsSide === "right"
		);
	}
</script>

<header
	class={`tauri-drag-region relative grid h-10 items-center bg-background ${isMobile.current ? "grid-cols-[auto_minmax(0,1fr)_auto]" : "grid-cols-[auto_minmax(0,1fr)_auto_auto]"}`}
	data-tauri-drag-region
>
	<div class="relative z-20 flex min-w-0 items-center gap-2 pl-4 pr-3">
		{#if isMobile.current}
			<DiscobotLogo size={24} />
			{#if onToggleSidebar}
				<Button
					variant="ghost"
					onclick={onToggleSidebar}
					aria-label="Expand sessions panel"
					title="Expand sessions panel"
					class="tauri-no-drag gap-1 px-1.5 text-xs font-medium uppercase tracking-[0.16em] text-muted-foreground"
				>
					<PanelLeftIcon class="size-3.5" />
					<span>Sessions</span>
				</Button>
			{/if}
		{:else}
			<AppMacWindowSpacer />
			<DiscobotBrand heightClass="h-6" />
		{/if}
	</div>

	<div class="relative z-20 min-w-0 px-2">
		{#if showSessionToolbar}
			<div class="tauri-no-drag min-w-0">
				<SessionToolbarStack />
			</div>
		{/if}
	</div>

	<div class="relative z-20 flex min-w-0 items-center justify-end gap-2">
		<button
			type="button"
			onclick={() => sessions.startNew()}
			aria-label="New session"
			title="New session"
			class="tauri-no-drag inline-flex shrink-0 items-center gap-1 rounded-md px-1 py-0.5 text-xs font-medium uppercase tracking-[0.16em] text-foreground/50 transition-colors hover:text-foreground/80"
		>
			<PlusIcon class="size-3 shrink-0" />
			<span>{isMobile.current ? "New" : "New Session"}</span>
		</button>

		<div class="flex min-w-0 items-center gap-1 pr-2">
			{#if preferences.showRefreshButton}
				<Button
					variant="ghost"
					size="icon-sm"
					onclick={() => location.reload()}
					aria-label="Refresh"
					title="Refresh"
					class="tauri-no-drag"
				>
					<RefreshCwIcon class="size-4" />
				</Button>
			{/if}
			<Button
				variant="ghost"
				size="icon-sm"
				onclick={() => ui.openSettings()}
				aria-label="Settings"
				title="Settings"
				class="tauri-no-drag relative"
			>
				<SettingsIcon class="size-4" />
				{#if updates.showBadge}
					<span class="absolute right-1 top-1 h-2 w-2 rounded-full bg-blue-500"
					></span>
				{/if}
			</Button>
		</div>
	</div>

	{#if !isMobile.current}
		<div
			class="tauri-no-drag relative z-20 flex h-full min-w-0 items-stretch justify-self-end pr-0"
		>
			{#if showWindowsLinuxControls()}
				<RightWindowControls />
			{/if}
		</div>
	{/if}

	<SettingsDialog />
</header>
