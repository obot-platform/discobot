<script lang="ts">
	import { onDestroy, onMount } from "svelte";
	import type { PaneAPI } from "paneforge";
	import AppHeader from "$lib/components/app/AppHeader.svelte";
	import StartupTasksBanner from "$lib/components/app/parts/StartupTasksBanner.svelte";
	import ThreadWorkspace from "$lib/components/app/ThreadWorkspace.svelte";
	import SessionSidebar from "$lib/components/app/SessionSidebar.svelte";
	import * as Resizable from "$lib/components/ui/resizable";
	import * as Sheet from "$lib/components/ui/sheet";
	import { setSessionContext } from "$lib/context/session-context.svelte";
	import { useAppContext } from "$lib/context/app-context.svelte";
	import { IsMobile } from "$lib/hooks/is-mobile.svelte.js";

	const app = useAppContext();
	const isMobile = new IsMobile(1024);
	const SIDEBAR_LAYOUT_STORAGE_KEY = "paneforge:discobot-ui-sidebar-layout";
	let desktopSidebarPane = $state<PaneAPI | null>(null);
	let desktopSidebarInitialized = $state(false);

	const session = setSessionContext();
	const sessionView = session.ui;

	onMount(() => {
		void session.load();
	});

	onDestroy(() => {
		if (app.sessions.sessionContexts.get(session.sessionId) !== session) {
			return;
		}
		session.dispose();
		app.sessions.sessionContexts.delete(session.sessionId);
	});

	function sidebarOpen() {
		return isMobile.current ? sessionView.mobileSidebarOpen : sessionView.desktopSidebarOpen;
	}

	function toggleSidebar() {
		if (isMobile.current) {
			sessionView.mobileSidebarOpen = !sessionView.mobileSidebarOpen;
			return;
		}

		if (!desktopSidebarPane) {
			sessionView.desktopSidebarOpen = !sessionView.desktopSidebarOpen;
			return;
		}

		if (desktopSidebarPane.isCollapsed()) {
			desktopSidebarPane.expand();
			sessionView.desktopSidebarOpen = true;
			return;
		}

		desktopSidebarPane.collapse();
		sessionView.desktopSidebarOpen = false;
	}

	function handleSessionSelect() {
		if (isMobile.current) {
			sessionView.mobileSidebarOpen = false;
		}
	}

	function hasSavedSidebarLayout() {
		return typeof window !== "undefined" && window.localStorage.getItem(SIDEBAR_LAYOUT_STORAGE_KEY);
	}

	$effect(() => {
		if (isMobile.current || !desktopSidebarPane || desktopSidebarInitialized) {
			return;
		}

		desktopSidebarInitialized = true;

		if (hasSavedSidebarLayout()) {
			sessionView.desktopSidebarOpen = !desktopSidebarPane.isCollapsed();
			return;
		}

		// Default to expanded — sessions list is primary navigation
		desktopSidebarPane.expand();
		sessionView.desktopSidebarOpen = true;
	});
</script>

<div class="h-screen flex flex-col bg-background text-foreground">
	<AppHeader showSessionToolbar={!session.isPending} />
	<StartupTasksBanner startup={app.startup} />

	<div class="flex min-h-0 flex-1 overflow-hidden">
		{#if isMobile.current}
			<Sheet.Root bind:open={sessionView.mobileSidebarOpen}>
				<Sheet.Content side="left" class="w-64 max-w-none bg-background p-3 [&>button]:hidden">
					<SessionSidebar onThreadSelect={handleSessionSelect} onToggleSidebar={toggleSidebar} />
				</Sheet.Content>
			</Sheet.Root>

			{#key session.threads.selectedId ?? session.sessionId}
				<ThreadWorkspace
					mainClass="flex min-h-0 flex-1 flex-col overflow-hidden"
					sidebarOpen={sidebarOpen()}
					onToggleSidebar={toggleSidebar}
					mode={session.isPending ? "conversation-only" : undefined}
				/>
			{/key}
		{:else}
			<Resizable.PaneGroup
				direction="horizontal"
				autoSaveId="discobot-ui-sidebar-layout"
				class="min-h-0 flex-1"
			>
				<Resizable.Pane
					bind:this={desktopSidebarPane}
					defaultSize={16}
					minSize={10}
					maxSize={35}
					collapsible
					collapsedSize={0}
					onCollapse={() => {
						sessionView.desktopSidebarOpen = false;
					}}
					onExpand={() => {
						sessionView.desktopSidebarOpen = true;
					}}
				>
					<div class="box-border h-full min-h-0 py-3 pl-3 pr-2">
						<SessionSidebar onToggleSidebar={toggleSidebar} />
					</div>
				</Resizable.Pane>
				<Resizable.Handle class="bg-transparent after:w-3" />
				<Resizable.Pane minSize={45} class="min-h-0">
					{#key session.threads.selectedId ?? session.sessionId}
						<ThreadWorkspace
							mainClass="flex h-full min-h-0 flex-col overflow-hidden"
							sidebarOpen={sidebarOpen()}
							onToggleSidebar={toggleSidebar}
							mode={session.isPending ? "conversation-only" : undefined}
						/>
					{/key}
				</Resizable.Pane>
			</Resizable.PaneGroup>
		{/if}
	</div>
</div>
