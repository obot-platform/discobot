<script lang="ts">
	import { CollapsibleContent } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";

	type Props = {
		maxHeight?: number;
		class?: string;
		children?: () => any;
	};

	let {
		maxHeight = 400,
		class: className,
		children,
		...restProps
	}: Props = $props();
</script>

<CollapsibleContent
	class={cn(
		"overflow-auto border-t bg-muted/30",
		"data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:animate-out data-[state=open]:animate-in",
		className,
	)}
	style={`max-height: ${maxHeight}px;`}
	{...restProps}
>
	{@render children?.()}
</CollapsibleContent>
