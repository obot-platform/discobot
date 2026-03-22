<script lang="ts">
	import ChevronDownIcon from "@lucide/svelte/icons/chevron-down";
	import CodeIcon from "@lucide/svelte/icons/code";
	import { CollapsibleTrigger } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";

	type Props = {
		isRaw?: boolean;
		onToggleRaw?: () => void;
		canCollapse?: boolean;
		class?: string;
	};

	let {
		isRaw,
		onToggleRaw,
		canCollapse = true,
		class: className,
		...restProps
	}: Props = $props();
</script>

<div class={cn("flex items-center gap-2", className)} {...restProps}>
	{#if onToggleRaw}
		<button
			type="button"
			class="inline-flex size-7 items-center justify-center rounded-md pointer-events-none opacity-0 transition-opacity hover:bg-accent hover:text-accent-foreground group-data-[state=open]/tool:pointer-events-auto group-data-[state=open]/tool:opacity-100"
			onpointerdown={(event) => {
				event.stopPropagation();
			}}
			onclick={(event) => {
				event.stopPropagation();
				onToggleRaw?.();
			}}
			title={isRaw ? "Show optimized view" : "Show raw view"}
		>
			<CodeIcon class="size-4" />
		</button>
	{/if}
	{#if canCollapse}
		<CollapsibleTrigger class="inline-flex size-7 items-center justify-center rounded-md opacity-0 transition-opacity hover:bg-accent hover:text-accent-foreground group-hover/tool:opacity-100 group-data-[state=open]/tool:opacity-100 focus-visible:opacity-100">
			<ChevronDownIcon class="size-4 text-muted-foreground transition-transform group-data-[state=open]/tool:rotate-180" />
		</CollapsibleTrigger>
	{/if}
</div>
