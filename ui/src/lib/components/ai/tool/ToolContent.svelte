<script lang="ts">
	import { CollapsibleContent } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";
	import { useToolContext } from "./context";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
	const tool = useToolContext();
</script>

<CollapsibleContent
	class={cn(
		"data-[state=closed]:fade-out-0 data-[state=closed]:slide-out-to-top-2 data-[state=open]:slide-in-from-top-2 text-popover-foreground outline-none data-[state=closed]:animate-out data-[state=open]:animate-in",
		className,
	)}
	{...restProps}
>
	{#if tool.isOpen}
		{@render children?.()}
	{/if}
</CollapsibleContent>
