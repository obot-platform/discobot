<script lang="ts">
	import { onMount } from "svelte";
	import ConversationPane from "$lib/components/app/ConversationPane.svelte";
	import DockPanel from "$lib/components/app/DockPanel.svelte";
	import ThreadWorkspaceHeader from "$lib/components/app/parts/ThreadWorkspaceHeader.svelte";
	import * as Resizable from "$lib/components/ui/resizable";
	import { useSessionContext } from "$lib/context/session-context.svelte";
	import { setThreadContext } from "$lib/context/thread-context.svelte";
	import { isChatView } from "$lib/session/view/create-session-view-state.svelte";

	type Props = {
		mainClass: string;
		showSidebarToggle?: boolean;
		reserveSidebarSpace?: boolean;
		onToggleSidebar?: () => void;
		mode?: "full" | "conversation-only";
	};

	const props: Props = $props();
	const noop = () => {};

	const session = useSessionContext();
	// threadId is stable at mount time because SessionWorkspace wraps us in {#key session.threads.selectedId}
	const thread = setThreadContext(
		session.threads.selectedId ?? session.sessionId,
	);

	onMount(() => {
		void thread.load();
		return () => {
			thread.dispose();
		};
	});

	const showDock = $derived(
		(props.mode ?? "full") === "full" && !isChatView(session.ui.activeView),
	);
	const dockMaximized = $derived(showDock && session.ui.dockMaximized);
</script>

<main class={props.mainClass}>
	{#if showDock && dockMaximized}
		<div class="min-h-0 flex-1 overflow-hidden">
			<DockPanel />
		</div>
	{:else if showDock}
		<Resizable.PaneGroup
			direction="horizontal"
			autoSaveId="discobot-ui-thread-layout"
			class="min-h-0 flex-1"
		>
			<Resizable.Pane defaultSize={35} minSize={25} class="min-h-0">
				<div class="flex min-h-0 h-full flex-col overflow-hidden">
					<ThreadWorkspaceHeader
						showSidebarToggle={props.showSidebarToggle ?? false}
						reserveSidebarSpace={props.reserveSidebarSpace ?? false}
						onToggleSidebar={props.onToggleSidebar ?? noop}
						title={session.threads.selected?.name ??
							(session.isPending ? "" : "No thread selected")}
						state={session.threads.selected?.state}
					/>
					<div class="min-h-0 flex-1 overflow-hidden">
						<ConversationPane />
					</div>
				</div>
			</Resizable.Pane>
			<Resizable.Handle class="bg-transparent" />
			<Resizable.Pane defaultSize={65} minSize={25} class="min-h-0">
				<div class="min-h-0 h-full overflow-auto">
					<DockPanel />
				</div>
			</Resizable.Pane>
		</Resizable.PaneGroup>
	{:else}
		<ThreadWorkspaceHeader
			showSidebarToggle={props.showSidebarToggle ?? false}
			reserveSidebarSpace={props.reserveSidebarSpace ?? false}
			onToggleSidebar={props.onToggleSidebar ?? noop}
			title={session.threads.selected?.name ??
				(session.isPending ? "" : "No thread selected")}
			state={session.threads.selected?.state}
		/>

		<div class="flex min-h-0 flex-1 flex-col overflow-hidden">
			<ConversationPane />
		</div>
	{/if}
</main>
