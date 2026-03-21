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
		sidebarOpen?: boolean;
		onToggleSidebar?: () => void;
		mode?: "full" | "conversation-only";
	};

	const props: Props = $props();
	const noop = () => {};

	const session = useSessionContext();
	// threadId is stable at mount time because SessionWorkspace wraps us in {#key session.threads.selectedId}
	const thread = setThreadContext(session.threads.selectedId ?? session.sessionId);

	onMount(() => {
		void thread.load();
		return () => {
			thread.dispose();
		};
	});

	const showDock = $derived(
		(props.mode ?? "full") === "full" && !isChatView(session.ui.activeView),
	);
</script>

<main class={props.mainClass}>
	{#if showDock}
		<Resizable.PaneGroup
			direction="horizontal"
			autoSaveId="discobot-ui-thread-layout"
			class="min-h-0 flex-1"
		>
			<Resizable.Pane defaultSize={55} minSize={35} class="min-h-0">
				<div class="flex min-h-0 h-full flex-col overflow-hidden">
					<ThreadWorkspaceHeader
						sidebarOpen={props.sidebarOpen ?? false}
						onToggleSidebar={props.onToggleSidebar ?? noop}
						title={session.threads.selected?.name ?? (session.isPending ? "" : "No thread selected")}
					/>
					<div class="min-h-0 flex-1 overflow-hidden">
						<ConversationPane contentTopPadding={5} />
					</div>
				</div>
			</Resizable.Pane>
			<Resizable.Handle />
			<Resizable.Pane defaultSize={45} minSize={25} class="min-h-0">
				<div class="min-h-0 h-full overflow-auto xl:rounded-tl-xl xl:border-t xl:border-l xl:border-border">
					<DockPanel />
				</div>
			</Resizable.Pane>
		</Resizable.PaneGroup>
	{:else}
		<ThreadWorkspaceHeader
			sidebarOpen={props.sidebarOpen ?? false}
			onToggleSidebar={props.onToggleSidebar ?? noop}
			title={session.threads.selected?.name ?? (session.isPending ? "" : "No thread selected")}
		/>

		<div class="flex min-h-0 flex-1 flex-col overflow-hidden">
			<ConversationPane contentTopPadding={5} />
		</div>
	{/if}
</main>
