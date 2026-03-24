<script lang="ts">
	import { Badge } from "$lib/components/ui/badge";
	import { cn } from "$lib/utils";
	import type { SchemaParameter } from "./context";

	type Props = SchemaParameter & { class?: string };
	let {
		name,
		type,
		required,
		description,
		location,
		class: className,
		...restProps
	}: Props = $props();
</script>

<div class={cn("px-4 py-3 pl-10", className)} {...restProps}>
	<div class="flex items-center gap-2">
		<span class="font-mono text-sm">{name}</span>
		<Badge class="text-xs" variant="outline">{type}</Badge>
		{#if location}
			<Badge class="text-xs" variant="secondary">{location}</Badge>
		{/if}
		{#if required}
			<Badge
				class="bg-red-100 text-red-700 text-xs dark:bg-red-900/30 dark:text-red-400"
				variant="secondary"
			>
				required
			</Badge>
		{/if}
	</div>
	{#if description}
		<p class="mt-1 text-muted-foreground text-sm">{description}</p>
	{/if}
</div>
