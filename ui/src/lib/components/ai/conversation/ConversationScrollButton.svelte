<script lang="ts">
	import ArrowDownIcon from "@lucide/svelte/icons/arrow-down";
	import { Button } from "$lib/components/ui/button";
	import { cn } from "$lib/utils";
	import { useConversationContext } from "./context";

	type Props = {
		class?: string;
	};

	let { class: className, ...restProps }: Props = $props();
	const conversation = useConversationContext();
</script>

{#if !conversation.isAtBottom}
	<Button
		class={cn(
			"absolute bottom-4 left-[50%] translate-x-[-50%] rounded-full dark:bg-background dark:hover:bg-muted",
			className,
		)}
		onclick={conversation.scrollToBottom}
		size="icon"
		type="button"
		variant="outline"
		{...restProps}
	>
		<ArrowDownIcon class="size-4" />
	</Button>
{/if}
