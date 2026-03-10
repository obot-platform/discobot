<script lang="ts">
	import { Collapsible, CollapsibleContent } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";
	import { useChainOfThoughtContext } from "./context";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
	const chainOfThought = useChainOfThoughtContext();
</script>

<Collapsible open={chainOfThought.isOpen}>
	<CollapsibleContent
		class={cn(
			"mt-2 space-y-3",
			"data-[state=closed]:fade-out-0 data-[state=closed]:slide-out-to-top-2 data-[state=open]:slide-in-from-top-2 text-popover-foreground outline-none data-[state=closed]:animate-out data-[state=open]:animate-in",
			className,
		)}
		{...restProps}
	>
		{@render children?.()}
	</CollapsibleContent>
</Collapsible>
