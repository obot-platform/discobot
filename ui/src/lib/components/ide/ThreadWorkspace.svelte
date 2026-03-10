<script lang="ts">
	import ConversationPane from "$lib/components/ide/ConversationPane.svelte";
	import DockPanel from "$lib/components/ide/DockPanel.svelte";
	import SessionToolbar from "$lib/components/ide/SessionToolbar.svelte";
	import { setThreadContext } from "$lib/context/thread-context.svelte";
	import type { ThreadRuntime } from "$lib/session/runtime/session-runtime.types";

	type Props = {
		threadRuntime: ThreadRuntime;
		mainClass: string;
		threadsOpen?: boolean;
		onToggleThreads?: () => void;
		mode?: "full" | "conversation-only";
	};

	const props: Props = $props();
	const noop = () => {};

	const thread = setThreadContext(() => props.threadRuntime);
</script>

<main class={props.mainClass}>
	{#if (props.mode ?? "full") === "full"}
		<SessionToolbar
			threadsOpen={props.threadsOpen ?? false}
			onToggleThreads={props.onToggleThreads ?? noop}
		/>
	{/if}

	<div class="flex min-h-0 flex-1 overflow-hidden">
		{#if (props.mode ?? "full") === "conversation-only" || thread.ui.centerPanel === "chat"}
			<div class="flex min-h-0 flex-1 flex-col overflow-hidden">
				<ConversationPane />
			</div>
		{:else}
			<div class="grid min-h-0 flex-1 xl:grid-cols-[1.1fr_0.9fr]">
				<div class="min-h-0">
					<ConversationPane />
				</div>
				<div class="min-h-0 overflow-auto xl:rounded-tl-xl xl:border-t xl:border-l xl:border-border">
					<DockPanel />
				</div>
			</div>
		{/if}
	</div>
</main>
