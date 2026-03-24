<script lang="ts">
	import { CardTitle } from "$lib/components/ui/card";
	import Shimmer from "$lib/components/ai/shimmer.svelte";
	import { usePlanContext } from "./context";

	type Props = {
		children?: () => any;
		text?: string;
	};

	let { children, text = "", ...restProps }: Props = $props();
	const plan = usePlanContext();
</script>

<CardTitle data-slot="plan-title" {...restProps}>
	{#if plan.isStreaming}
		<Shimmer {text}
			>{#if children}{@render children()}{/if}</Shimmer
		>
	{:else if children}
		{@render children()}
	{:else}
		{text}
	{/if}
</CardTitle>
