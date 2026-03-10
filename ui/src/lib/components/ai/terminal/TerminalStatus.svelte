<script lang="ts">
	import Shimmer from "$lib/components/ai/shimmer.svelte";
	import { cn } from "$lib/utils";
	import { useTerminalContext } from "./context";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();

	const terminal = useTerminalContext();
</script>

{#if terminal.isStreaming}
	<div
		class={cn("flex items-center gap-2 text-xs text-zinc-400", className)}
		{...restProps}
	>
		{#if children}
			{@render children()}
		{:else}
			<Shimmer class="w-16">Loading...</Shimmer>
		{/if}
	</div>
{/if}
