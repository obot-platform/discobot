<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = { tokens?: number; costText?: string; class?: string };
	let { tokens, costText, class: className }: Props = $props();

	const renderedTokens = $derived.by(() => {
		if (tokens === undefined) {
			return "—";
		}
		return new Intl.NumberFormat("en-US", { notation: "compact" }).format(
			tokens,
		);
	});
</script>

<span class={cn(className)}>
	{renderedTokens}
	{#if costText}
		<span class="ml-2 text-muted-foreground">• {costText}</span>
	{/if}
</span>
