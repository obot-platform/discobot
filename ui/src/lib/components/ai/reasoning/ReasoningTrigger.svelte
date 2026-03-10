<script lang="ts">
	import BrainIcon from "@lucide/svelte/icons/brain";
	import ChevronDownIcon from "@lucide/svelte/icons/chevron-down";
	import { CollapsibleTrigger } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";
	import Shimmer from "../shimmer.svelte";
	import { useReasoningContext } from "./context";

	type Props = {
		getThinkingMessage?: (isStreaming: boolean, duration?: number) => string;
		class?: string;
		children?: () => any;
	};

	const defaultGetThinkingMessage = (
		isStreaming: boolean,
		duration?: number,
	): string => {
		if (isStreaming || duration === 0) {
			return "Thinking...";
		}
		if (duration === undefined) {
			return "Thought for a few seconds";
		}
		return `Thought for ${duration} seconds`;
	};

	let {
		getThinkingMessage = defaultGetThinkingMessage,
		class: className,
		children,
		...restProps
	}: Props = $props();

	const reasoning = useReasoningContext();
	const message = $derived.by(() =>
		getThinkingMessage(reasoning.isStreaming, reasoning.duration),
	);
</script>

<CollapsibleTrigger
	class={cn(
		"flex w-full items-center gap-2 text-muted-foreground text-sm transition-colors hover:text-foreground",
		className,
	)}
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else}
		<BrainIcon class="size-4" />
		{#if reasoning.isStreaming || reasoning.duration === 0}
			<Shimmer duration={1}>{message}</Shimmer>
		{:else}
			<p>{message}</p>
		{/if}
		<ChevronDownIcon
			class={cn(
				"size-4 transition-transform",
				reasoning.isOpen ? "rotate-180" : "rotate-0",
			)}
		/>
	{/if}
</CollapsibleTrigger>
