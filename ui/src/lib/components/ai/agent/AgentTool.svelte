<script lang="ts">
	import type { ComponentProps } from "svelte";
	import { CodeBlock } from "$lib/components/ai/code-block";
	import {
		AccordionContent,
		AccordionItem,
		AccordionTrigger,
	} from "$lib/components/ui/accordion";
	import { cn } from "$lib/utils";

	type AgentToolDefinition = {
		description?: string;
		jsonSchema?: unknown;
		inputSchema?: unknown;
	};

	type Props = ComponentProps<typeof AccordionItem> & {
		tool: AgentToolDefinition;
		class?: string;
	};

	let { tool, value, class: className, ...restProps }: Props = $props();

	const schema = $derived.by(() => tool.jsonSchema ?? tool.inputSchema);
</script>

<AccordionItem
	class={cn("border-b last:border-b-0", className)}
	{value}
	{...restProps}
>
	<AccordionTrigger class="px-3 py-2 text-sm hover:no-underline">
		{tool.description ?? "No description"}
	</AccordionTrigger>
	<AccordionContent class="px-3 pb-3">
		<div class="rounded-md bg-muted/50">
			<CodeBlock code={JSON.stringify(schema, null, 2)} language="json" />
		</div>
	</AccordionContent>
</AccordionItem>
