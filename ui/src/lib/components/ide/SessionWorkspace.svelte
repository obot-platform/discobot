<script lang="ts">
	import type { PaneAPI } from "paneforge";
	import AppHeader from "$lib/components/ide/AppHeader.svelte";
	import ThreadWorkspace from "$lib/components/ide/ThreadWorkspace.svelte";
	import ThreadSidebar from "$lib/components/ide/ThreadSidebar.svelte";
	import * as Resizable from "$lib/components/ui/resizable";
	import * as Sheet from "$lib/components/ui/sheet";
	import { setSessionContext } from "$lib/context/session-context.svelte";
	import { useAppContext } from "$lib/context/app-context.svelte";
	import { IsMobile } from "$lib/hooks/is-mobile.svelte.js";
	import { envSets, sessionDataById } from "$lib/mock-shell-data";

	const app = useAppContext();
	const isMobile = new IsMobile(1024);
	const THREADS_LAYOUT_STORAGE_KEY = "paneforge:discobot-ui-threads-layout";
	let desktopThreadsOpen = $state(false);
	let mobileThreadsOpen = $state(false);
	let desktopThreadsPane = $state<PaneAPI | null>(null);
	let desktopThreadsInitialized = $state(false);

	const runtimeBundle = setSessionContext({
		sessionDataById,
		envSets,
		selectedSessionId: app.selectedSessionId ?? undefined,
	});

	function threadsOpen() {
		return isMobile.current ? mobileThreadsOpen : desktopThreadsOpen;
	}

	function toggleThreads() {
		if (isMobile.current) {
			mobileThreadsOpen = !mobileThreadsOpen;
			return;
		}

		if (!desktopThreadsPane) {
			desktopThreadsOpen = !desktopThreadsOpen;
			return;
		}

		if (desktopThreadsPane.isCollapsed()) {
			desktopThreadsPane.expand();
			desktopThreadsOpen = true;
			return;
		}

		desktopThreadsPane.collapse();
		desktopThreadsOpen = false;
	}

	function handleThreadSelect() {
		if (isMobile.current) {
			mobileThreadsOpen = false;
		}
	}

	function hasSavedThreadsLayout() {
		return typeof window !== "undefined" && window.localStorage.getItem(THREADS_LAYOUT_STORAGE_KEY);
	}

	$effect(() => {
		if (isMobile.current || !desktopThreadsPane || desktopThreadsInitialized) {
			return;
		}

		desktopThreadsInitialized = true;

		if (hasSavedThreadsLayout()) {
			desktopThreadsOpen = !desktopThreadsPane.isCollapsed();
			return;
		}

		desktopThreadsPane.collapse();
		desktopThreadsOpen = false;
	});
</script>

<div class="h-screen flex flex-col bg-background text-foreground">
	<AppHeader />

	{#if !app.selectedSessionId}
		<ThreadWorkspace
			threadRuntime={runtimeBundle.thread}
			mainClass="flex min-h-0 flex-1 flex-col overflow-hidden"
			mode="conversation-only"
		/>
	{:else}
		<div class="flex min-h-0 flex-1 overflow-hidden">
			{#if isMobile.current}
				<Sheet.Root bind:open={mobileThreadsOpen}>
					<Sheet.Content side="left" class="w-64 max-w-none p-0 [&>button]:hidden">
						<ThreadSidebar onThreadSelect={handleThreadSelect} />
					</Sheet.Content>
				</Sheet.Root>

				<ThreadWorkspace
					threadRuntime={runtimeBundle.thread}
					mainClass="flex min-h-0 flex-1 flex-col overflow-hidden"
					threadsOpen={threadsOpen()}
					onToggleThreads={toggleThreads}
				/>
			{:else}
				<Resizable.PaneGroup
					direction="horizontal"
					autoSaveId="discobot-ui-threads-layout"
					class="min-h-0 flex-1"
				>
					<Resizable.Pane
						bind:this={desktopThreadsPane}
						defaultSize={16}
						minSize={10}
						maxSize={35}
						collapsible
						collapsedSize={0}
						onCollapse={() => {
							desktopThreadsOpen = false;
						}}
						onExpand={() => {
							desktopThreadsOpen = true;
						}}
					>
						<ThreadSidebar />
					</Resizable.Pane>
					<Resizable.Handle />
					<Resizable.Pane minSize={45} class="min-h-0">
						<ThreadWorkspace
							threadRuntime={runtimeBundle.thread}
							mainClass="flex h-full min-h-0 flex-col overflow-hidden"
							threadsOpen={threadsOpen()}
							onToggleThreads={toggleThreads}
						/>
					</Resizable.Pane>
				</Resizable.PaneGroup>
			{/if}
		</div>
	{/if}
</div>
