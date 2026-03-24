<script lang="ts">
	import { cn } from "$lib/utils";
	import { estimateCostUSD, useContextUsageContext } from "./context";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
	const ctx = useContextUsageContext();

	const totalCost = $derived.by(() =>
		new Intl.NumberFormat("en-US", {
			style: "currency",
			currency: "USD",
		}).format(
			estimateCostUSD(
				(ctx.usage?.inputTokens ?? 0) + (ctx.usage?.outputTokens ?? 0),
			),
		),
	);
</script>

<div
	class={cn(
		"flex w-full items-center justify-between gap-3 bg-secondary p-3 text-xs",
		className,
	)}
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else}
		<span class="text-muted-foreground">Total cost</span>
		<span>{totalCost}</span>
	{/if}
</div>
