<script lang="ts">
	import { CardDescription } from "$lib/components/ui/card";
	import Shimmer from "$lib/components/ai/shimmer.svelte";
	import { cn } from "$lib/utils";
	import { usePlanContext } from "./context";

	type Props = {
		class?: string;
		children?: () => any;
		text?: string;
	};

	let { class: className, children, text = "", ...restProps }: Props = $props();
	const plan = usePlanContext();
</script>

<CardDescription class={cn("text-balance", className)} data-slot="plan-description" {...restProps}>
	{#if plan.isStreaming}
		<Shimmer text={text}>{#if children}{@render children()}{/if}</Shimmer>
	{:else if children}
		{@render children()}
	{:else}
		{text}
	{/if}
</CardDescription>
