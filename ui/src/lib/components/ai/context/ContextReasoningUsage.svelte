<script lang="ts">
	import { cn } from "$lib/utils";
	import { estimateCostUSD, useContextUsageContext } from "./context";
	import TokensWithCost from "./TokensWithCost.svelte";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
	const ctx = useContextUsageContext();

	const reasoningTokens = $derived.by(() => ctx.usage?.reasoningTokens ?? 0);
	const reasoningCostText = $derived.by(() =>
		new Intl.NumberFormat("en-US", {
			style: "currency",
			currency: "USD",
		}).format(estimateCostUSD(reasoningTokens)),
	);
</script>

{#if children}
	{@render children()}
{:else if reasoningTokens}
	<div class={cn("flex items-center justify-between text-xs", className)} {...restProps}>
		<span class="text-muted-foreground">Reasoning</span>
		<TokensWithCost tokens={reasoningTokens} costText={reasoningCostText} />
	</div>
{/if}
