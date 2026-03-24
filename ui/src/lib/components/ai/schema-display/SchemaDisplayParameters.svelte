<script lang="ts">
	import ChevronRightIcon from "@lucide/svelte/icons/chevron-right";
	import { Badge } from "$lib/components/ui/badge";
	import {
		Collapsible,
		CollapsibleContent,
		CollapsibleTrigger,
	} from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";
	import { useSchemaDisplayContext } from "./context";
	import SchemaDisplayParameter from "./SchemaDisplayParameter.svelte";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
	const schemaDisplay = useSchemaDisplayContext();
	let open = $state(true);
</script>

<Collapsible bind:open class={cn(className)} {...restProps}>
	<CollapsibleTrigger
		class="group flex w-full items-center gap-2 px-4 py-3 text-left transition-colors hover:bg-muted/50"
	>
		<ChevronRightIcon
			class="size-4 shrink-0 text-muted-foreground transition-transform group-data-[state=open]:rotate-90"
		/>
		<span class="font-medium text-sm">Parameters</span>
		<Badge class="ml-auto text-xs" variant="secondary"
			>{schemaDisplay.parameters?.length ?? 0}</Badge
		>
	</CollapsibleTrigger>
	<CollapsibleContent>
		<div class="divide-y border-t">
			{#if children}
				{@render children()}
			{:else}
				{#each schemaDisplay.parameters ?? [] as parameter (parameter.name)}
					<SchemaDisplayParameter {...parameter} />
				{/each}
			{/if}
		</div>
	</CollapsibleContent>
</Collapsible>
