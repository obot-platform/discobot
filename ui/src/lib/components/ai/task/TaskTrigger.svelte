<script lang="ts">
	import ChevronDownIcon from "@lucide/svelte/icons/chevron-down";
	import SearchIcon from "@lucide/svelte/icons/search";
	import { CollapsibleTrigger } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";

	type Props = {
		title: string;
		class?: string;
		children?: () => any;
	};

	let { title, class: className, children, ...restProps }: Props = $props();
</script>

<CollapsibleTrigger class={cn("group", className)} {...restProps}>
	{#if children}
		{@render children()}
	{:else}
		<div
			class="flex w-full cursor-pointer items-center gap-2 text-muted-foreground text-sm transition-colors hover:text-foreground"
		>
			<SearchIcon class="size-4" />
			<p class="text-sm">{title}</p>
			<ChevronDownIcon
				class="size-4 transition-transform group-data-[state=open]:rotate-180"
			/>
		</div>
	{/if}
</CollapsibleTrigger>
