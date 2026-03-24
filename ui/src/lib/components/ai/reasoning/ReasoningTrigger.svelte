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
		!reasoning.isStreaming && reasoning.previewText
			? reasoning.previewText
			: getThinkingMessage(reasoning.isStreaming, reasoning.duration),
	);
</script>

<div class={cn("flex items-center justify-between gap-4 px-4 pt-4", className)}>
	{#if reasoning.isStreaming}
		<div
			class="flex min-w-0 flex-1 items-center gap-2 text-left text-muted-foreground"
		>
			<BrainIcon class="size-4 shrink-0 text-muted-foreground" />
			<span class="truncate font-medium text-sm">
				{#if children}
					{@render children()}
				{:else}
					<Shimmer duration={1}>{message}</Shimmer>
				{/if}
			</span>
		</div>
		<div class="size-7 shrink-0" aria-hidden="true"></div>
	{:else}
		<CollapsibleTrigger
			class="flex min-w-0 flex-1 items-center gap-2 text-left text-muted-foreground"
			{...restProps}
		>
			<BrainIcon class="size-4 shrink-0 text-muted-foreground" />
			<span class="truncate font-medium text-sm">
				{#if children}
					{@render children()}
				{:else}
					{message}
				{/if}
			</span>
		</CollapsibleTrigger>
		<CollapsibleTrigger
			class="inline-flex size-7 items-center justify-center rounded-md opacity-0 transition-opacity hover:bg-accent hover:text-accent-foreground group-hover/reasoning:opacity-100 group-data-[state=open]/reasoning:opacity-100 focus-visible:opacity-100"
		>
			<ChevronDownIcon
				class={cn(
					"size-4 text-muted-foreground transition-transform",
					reasoning.isOpen ? "rotate-180" : "rotate-0",
				)}
			/>
		</CollapsibleTrigger>
	{/if}
</div>
