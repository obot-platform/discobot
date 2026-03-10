<script lang="ts">
	import { cn } from "$lib/utils";
	import { estimateCostUSD, useContextUsageContext } from "./context";
	import TokensWithCost from "./TokensWithCost.svelte";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
	const ctx = useContextUsageContext();

	const inputTokens = $derived.by(() => ctx.usage?.inputTokens ?? 0);
	const inputCostText = $derived.by(() =>
		new Intl.NumberFormat("en-US", {
			style: "currency",
			currency: "USD",
		}).format(estimateCostUSD(inputTokens)),
	);
</script>

{#if children}
	{@render children()}
{:else if inputTokens}
	<div class={cn("flex items-center justify-between text-xs", className)} {...restProps}>
		<span class="text-muted-foreground">Input</span>
		<TokensWithCost tokens={inputTokens} costText={inputCostText} />
	</div>
{/if}
