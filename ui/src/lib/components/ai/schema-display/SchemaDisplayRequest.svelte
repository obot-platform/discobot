<script lang="ts">
	import ChevronRightIcon from "@lucide/svelte/icons/chevron-right";
	import {
		Collapsible,
		CollapsibleContent,
		CollapsibleTrigger,
	} from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";
	import { useSchemaDisplayContext } from "./context";
	import SchemaDisplayProperty from "./SchemaDisplayProperty.svelte";

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
		<span class="font-medium text-sm">Request Body</span>
	</CollapsibleTrigger>
	<CollapsibleContent>
		<div class="border-t">
			{#if children}
				{@render children()}
			{:else}
				{#each schemaDisplay.requestBody ?? [] as property (property.name)}
					<SchemaDisplayProperty {...property} depth={0} />
				{/each}
			{/if}
		</div>
	</CollapsibleContent>
</Collapsible>
