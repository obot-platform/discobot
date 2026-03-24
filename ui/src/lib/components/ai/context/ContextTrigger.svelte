<script lang="ts">
	import { HoverCardTrigger } from "$lib/components/ui/hover-card";
	import { Button } from "$lib/components/ui/button";
	import { useContextUsageContext } from "./context";

	type Props = { children?: () => any };
	let { children, ...restProps }: Props = $props();
	const ctx = useContextUsageContext();

	const usedPercent = $derived.by(() =>
		ctx.maxTokens === 0 ? 0 : ctx.usedTokens / ctx.maxTokens,
	);
	const renderedPercent = $derived.by(() =>
		new Intl.NumberFormat("en-US", {
			style: "percent",
			maximumFractionDigits: 1,
		}).format(usedPercent),
	);
	const circumference = 2 * Math.PI * 10;
	const dashOffset = $derived.by(() => circumference * (1 - usedPercent));
</script>

<HoverCardTrigger>
	{#if children}
		{@render children()}
	{:else}
		<Button type="button" variant="ghost" {...restProps}>
			<span class="font-medium text-muted-foreground">{renderedPercent}</span>
			<svg
				aria-label="Model context usage"
				height="20"
				role="img"
				style="color: currentcolor"
				viewBox="0 0 24 24"
				width="20"
			>
				<circle
					cx="12"
					cy="12"
					fill="none"
					opacity="0.25"
					r="10"
					stroke="currentColor"
					stroke-width="2"
				/>
				<circle
					cx="12"
					cy="12"
					fill="none"
					opacity="0.7"
					r="10"
					stroke="currentColor"
					stroke-dasharray={`${circumference} ${circumference}`}
					stroke-dashoffset={dashOffset}
					stroke-linecap="round"
					stroke-width="2"
					style="transform-origin: center; transform: rotate(-90deg);"
				/>
			</svg>
		</Button>
	{/if}
</HoverCardTrigger>
