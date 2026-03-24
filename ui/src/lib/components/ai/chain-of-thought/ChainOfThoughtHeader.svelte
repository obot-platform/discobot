<script lang="ts">
	import BrainIcon from "@lucide/svelte/icons/brain";
	import ChevronDownIcon from "@lucide/svelte/icons/chevron-down";
	import {
		Collapsible,
		CollapsibleTrigger,
	} from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";
	import { useChainOfThoughtContext } from "./context";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
	const chainOfThought = useChainOfThoughtContext();
</script>

<Collapsible
	open={chainOfThought.isOpen}
	onOpenChange={chainOfThought.setIsOpen}
>
	<CollapsibleTrigger
		class={cn(
			"flex w-full items-center gap-2 text-muted-foreground text-sm transition-colors hover:text-foreground",
			className,
		)}
		{...restProps}
	>
		<BrainIcon class="size-4" />
		<span class="flex-1 text-left">
			{#if children}
				{@render children()}
			{:else}
				Chain of Thought
			{/if}
		</span>
		<ChevronDownIcon
			class={cn(
				"size-4 transition-transform",
				chainOfThought.isOpen ? "rotate-180" : "rotate-0",
			)}
		/>
	</CollapsibleTrigger>
</Collapsible>
