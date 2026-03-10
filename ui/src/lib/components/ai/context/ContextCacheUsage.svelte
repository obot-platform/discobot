<script lang="ts">
	import { cn } from "$lib/utils";
	import { estimateCostUSD, useContextUsageContext } from "./context";
	import TokensWithCost from "./TokensWithCost.svelte";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
	const ctx = useContextUsageContext();

	const cacheTokens = $derived.by(() => ctx.usage?.cachedInputTokens ?? 0);
	const cacheCostText = $derived.by(() =>
		new Intl.NumberFormat("en-US", {
			style: "currency",
			currency: "USD",
		}).format(estimateCostUSD(cacheTokens)),
	);
</script>

{#if children}
	{@render children()}
{:else if cacheTokens}
	<div class={cn("flex items-center justify-between text-xs", className)} {...restProps}>
		<span class="text-muted-foreground">Cache</span>
		<TokensWithCost tokens={cacheTokens} costText={cacheCostText} />
	</div>
{/if}
