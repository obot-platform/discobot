<script lang="ts">
	import { cn } from "$lib/utils";
	import { estimateCostUSD, useContextUsageContext } from "./context";
	import TokensWithCost from "./TokensWithCost.svelte";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
	const ctx = useContextUsageContext();

	const outputTokens = $derived.by(() => ctx.usage?.outputTokens ?? 0);
	const outputCostText = $derived.by(() =>
		new Intl.NumberFormat("en-US", {
			style: "currency",
			currency: "USD",
		}).format(estimateCostUSD(outputTokens)),
	);
</script>

{#if children}
	{@render children()}
{:else if outputTokens}
	<div class={cn("flex items-center justify-between text-xs", className)} {...restProps}>
		<span class="text-muted-foreground">Output</span>
		<TokensWithCost tokens={outputTokens} costText={outputCostText} />
	</div>
{/if}
