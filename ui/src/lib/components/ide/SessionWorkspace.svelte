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

	const app = useAppContext();
	const ui = app.ui;
	const isMobile = new IsMobile(1024);
	const THREADS_LAYOUT_STORAGE_KEY = "paneforge:discobot-ui-threads-layout";
	let desktopThreadsPane = $state<PaneAPI | null>(null);
	let desktopThreadsInitialized = $state(false);

	const session = setSessionContext();
	const sessionView = session.ui;

	function threadsOpen() {
		return isMobile.current ? sessionView.mobileThreadsOpen : sessionView.desktopThreadsOpen;
	}

	function toggleThreads() {
		if (isMobile.current) {
			sessionView.mobileThreadsOpen = !sessionView.mobileThreadsOpen;
			return;
		}

		if (!desktopThreadsPane) {
			sessionView.desktopThreadsOpen = !sessionView.desktopThreadsOpen;
			return;
		}

		if (desktopThreadsPane.isCollapsed()) {
			desktopThreadsPane.expand();
			sessionView.desktopThreadsOpen = true;
			return;
		}

		desktopThreadsPane.collapse();
		sessionView.desktopThreadsOpen = false;
	}

	function handleThreadSelect() {
		if (isMobile.current) {
			sessionView.mobileThreadsOpen = false;
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
			sessionView.desktopThreadsOpen = !desktopThreadsPane.isCollapsed();
			return;
		}

		desktopThreadsPane.collapse();
		sessionView.desktopThreadsOpen = false;
	});
</script>

<div class="h-screen flex flex-col bg-background text-foreground">
	<AppHeader />

	<div class="flex min-h-0 flex-1 overflow-hidden">
		{#if isMobile.current}
			{#if !session.isPending}
				<Sheet.Root bind:open={sessionView.mobileThreadsOpen}>
					<Sheet.Content side="left" class="w-64 max-w-none p-0 [&>button]:hidden">
						<ThreadSidebar onThreadSelect={handleThreadSelect} />
					</Sheet.Content>
				</Sheet.Root>
			{/if}

			{#key session.threads.selectedId ?? session.sessionId}
				<ThreadWorkspace
					mainClass="flex min-h-0 flex-1 flex-col overflow-hidden"
					threadsOpen={session.isPending ? false : threadsOpen()}
					onToggleThreads={session.isPending ? undefined : toggleThreads}
					mode={session.isPending ? "conversation-only" : undefined}
				/>
			{/key}
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
						sessionView.desktopThreadsOpen = false;
					}}
					onExpand={() => {
						sessionView.desktopThreadsOpen = true;
					}}
				>
					{#if !session.isPending}
						<ThreadSidebar />
					{/if}
				</Resizable.Pane>
				<Resizable.Handle />
				<Resizable.Pane minSize={45} class="min-h-0">
					{#key session.threads.selectedId ?? session.sessionId}
						<ThreadWorkspace
							mainClass="flex h-full min-h-0 flex-col overflow-hidden"
							threadsOpen={session.isPending ? false : threadsOpen()}
							onToggleThreads={session.isPending ? undefined : toggleThreads}
							mode={session.isPending ? "conversation-only" : undefined}
						/>
					{/key}
				</Resizable.Pane>
			</Resizable.PaneGroup>
		{/if}
	</div>
</div>
