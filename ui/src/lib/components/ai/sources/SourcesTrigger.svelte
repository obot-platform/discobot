<script lang="ts">
	import ChevronDownIcon from "@lucide/svelte/icons/chevron-down";
	import { CollapsibleTrigger } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";

	type Props = {
		count: number;
		class?: string;
		children?: () => any;
	};

	let { count, class: className, children, ...restProps }: Props = $props();
</script>

<CollapsibleTrigger class={cn("group flex items-center gap-2", className)} {...restProps}>
	{#if children}
		{@render children()}
	{:else}
		<p class="font-medium">Used {count} sources</p>
		<ChevronDownIcon class="h-4 w-4 transition-transform group-data-[state=open]:rotate-180" />
	{/if}
</CollapsibleTrigger>
