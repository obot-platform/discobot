<script lang="ts">
	import { cn } from "$lib/utils";
	import { useContextUsageContext } from "./context";
	import { Progress } from "$lib/components/ui/progress";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
	const ctx = useContextUsageContext();

	const usedPercent = $derived.by(() => (ctx.maxTokens === 0 ? 0 : ctx.usedTokens / ctx.maxTokens));
	const displayPct = $derived.by(
		() =>
			new Intl.NumberFormat("en-US", {
				style: "percent",
				maximumFractionDigits: 1,
			}).format(usedPercent),
	);
	const used = $derived.by(() => new Intl.NumberFormat("en-US", { notation: "compact" }).format(ctx.usedTokens));
	const total = $derived.by(() => new Intl.NumberFormat("en-US", { notation: "compact" }).format(ctx.maxTokens));
</script>

<div class={cn("w-full space-y-2 p-3", className)} {...restProps}>
	{#if children}
		{@render children()}
	{:else}
		<div class="flex items-center justify-between gap-3 text-xs">
			<p>{displayPct}</p>
			<p class="font-mono text-muted-foreground">{used} / {total}</p>
		</div>
		<div class="space-y-2">
			<Progress class="bg-muted" value={usedPercent * 100} />
		</div>
	{/if}
</div>
