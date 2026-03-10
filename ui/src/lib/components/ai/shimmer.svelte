<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = {
		children?: () => any;
		as?: keyof HTMLElementTagNameMap;
		class?: string;
		duration?: number;
		spread?: number;
		text?: string;
	};

	let {
		children,
		as = "p",
		class: className,
		duration = 2,
		spread = 2,
		text = "",
	}: Props = $props();

	const renderedText = $derived(text || "");
	const dynamicSpread = $derived((renderedText.length || 0) * spread);
</script>

<svelte:element
	this={as}
	class={cn(
		"discobot-ai-shimmer relative inline-block bg-[length:250%_100%,auto] bg-clip-text text-transparent",
		className,
	)}
	style={`--spread:${dynamicSpread}px;--duration:${duration}s;background-image:linear-gradient(90deg,#0000 calc(50% - var(--spread)),var(--color-background),#0000 calc(50% + var(--spread))),linear-gradient(var(--color-muted-foreground),var(--color-muted-foreground));`}
>
	{#if children}
		{@render children()}
	{:else}
		{renderedText}
	{/if}
</svelte:element>

<style>
	.discobot-ai-shimmer {
		background-repeat: no-repeat, padding-box;
		animation: discobot-ai-shimmer-slide var(--duration) linear infinite;
	}

	@keyframes discobot-ai-shimmer-slide {
		0% {
			background-position: 100% center;
		}
		100% {
			background-position: 0% center;
		}
	}
</style>
