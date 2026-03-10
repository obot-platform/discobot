<script lang="ts">
	import { cn } from "$lib/utils";
	import { useTerminalContext } from "./context";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
	const terminal = useTerminalContext();
	let containerRef = $state<HTMLDivElement | null>(null);

	$effect(() => {
		if (terminal.autoScroll && containerRef) {
			containerRef.scrollTop = containerRef.scrollHeight;
		}
	});
</script>

<div
	bind:this={containerRef}
	class={cn(
		"max-h-96 overflow-auto p-4 font-mono text-sm leading-relaxed",
		className,
	)}
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else}
		<pre class="whitespace-pre-wrap break-words">{terminal.output}{#if terminal.isStreaming}<span class="ml-0.5 inline-block h-4 w-2 animate-pulse bg-zinc-100"></span>{/if}</pre>
	{/if}
</div>
