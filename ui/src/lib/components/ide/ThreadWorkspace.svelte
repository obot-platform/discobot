<script lang="ts">
	import { onMount } from "svelte";
	import ConversationPane from "$lib/components/ide/ConversationPane.svelte";
	import DockPanel from "$lib/components/ide/DockPanel.svelte";
	import SessionToolbar from "$lib/components/ide/SessionToolbar.svelte";
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
	{#if (props.mode ?? "full") === "full"}
		<SessionToolbar
			sidebarOpen={props.sidebarOpen ?? false}
			onToggleSidebar={props.onToggleSidebar ?? noop}
		/>
	{/if}

	<div class="flex min-h-0 flex-1 overflow-hidden">
		<div
			class={showDock
				? "grid min-h-0 flex-1 xl:grid-cols-[1.1fr_0.9fr]"
				: "flex min-h-0 flex-1 flex-col overflow-hidden"}
		>
			<div class={showDock ? "min-h-0" : "contents"}>
				<ConversationPane contentTopPadding={5} />
			</div>
			{#if showDock}
				<div
					class="min-h-0 overflow-auto xl:rounded-tl-xl xl:border-t xl:border-l xl:border-border"
				>
					<DockPanel />
				</div>
			{/if}
		</div>
	</div>
</main>
