<script lang="ts">
	import ChevronsUpDownIcon from "@lucide/svelte/icons/chevrons-up-down";
	import { CollapsibleTrigger } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
	};

	let { class: className, ...restProps }: Props = $props();
</script>

<CollapsibleTrigger
	class={cn(
		"inline-flex size-8 items-center justify-center rounded-md transition-colors hover:bg-accent hover:text-accent-foreground",
		className,
	)}
	data-slot="plan-trigger"
	{...restProps}
>
	<ChevronsUpDownIcon class="size-4" />
	<span class="sr-only">Toggle plan</span>
</CollapsibleTrigger>
