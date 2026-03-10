<script lang="ts">
	import ChevronRightIcon from "@lucide/svelte/icons/chevron-right";
	import { Badge } from "$lib/components/ui/badge";
	import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";
	import type { SchemaProperty } from "./context";
	import SchemaDisplayProperty from "./SchemaDisplayProperty.svelte";

	type Props = SchemaProperty & {
		depth?: number;
		class?: string;
	};

	let {
		name,
		type,
		required,
		description,
		properties,
		items,
		depth = 0,
		class: className,
		...restProps
	}: Props = $props();

	const hasChildren = $derived.by(() => !!(properties?.length || items));
	const paddingLeft = $derived.by(() => 40 + depth * 16);
	let open = $state(false);
	$effect(() => {
		open = depth < 2;
	});
</script>

{#if hasChildren}
	<Collapsible bind:open>
		<CollapsibleTrigger
			class={cn(
				"group flex w-full items-center gap-2 py-3 text-left transition-colors hover:bg-muted/50",
				className,
			)}
			style={`padding-left:${paddingLeft}px`}
		>
			<ChevronRightIcon class="size-4 shrink-0 text-muted-foreground transition-transform group-data-[state=open]:rotate-90" />
			<span class="font-mono text-sm">{name}</span>
			<Badge class="text-xs" variant="outline">{type}</Badge>
			{#if required}
				<Badge class="bg-red-100 text-red-700 text-xs dark:bg-red-900/30 dark:text-red-400" variant="secondary">
					required
				</Badge>
			{/if}
		</CollapsibleTrigger>
		{#if description}
			<p class="pb-2 text-muted-foreground text-sm" style={`padding-left:${paddingLeft + 24}px`}>
				{description}
			</p>
		{/if}
		<CollapsibleContent>
			<div class="divide-y border-t">
				{#each properties ?? [] as property (property.name)}
					<SchemaDisplayProperty {...property} depth={depth + 1} />
				{/each}
				{#if items}
					<SchemaDisplayProperty {...items} depth={depth + 1} name={`${name}[]`} />
				{/if}
			</div>
		</CollapsibleContent>
	</Collapsible>
{:else}
	<div class={cn("py-3 pr-4", className)} style={`padding-left:${paddingLeft}px`} {...restProps}>
		<div class="flex items-center gap-2">
			<span class="size-4"></span>
			<span class="font-mono text-sm">{name}</span>
			<Badge class="text-xs" variant="outline">{type}</Badge>
			{#if required}
				<Badge class="bg-red-100 text-red-700 text-xs dark:bg-red-900/30 dark:text-red-400" variant="secondary">
					required
				</Badge>
			{/if}
		</div>
		{#if description}
			<p class="mt-1 pl-6 text-muted-foreground text-sm">{description}</p>
		{/if}
	</div>
{/if}
