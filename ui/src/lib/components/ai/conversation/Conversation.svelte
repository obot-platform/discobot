<script lang="ts">
	import { cn } from "$lib/utils";
	import { setConversationContext } from "./context";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
	let viewport = $state<HTMLElement | null>(null);

	const conversation = $state({
		isAtBottom: true,
		scrollToBottom: () => {
			viewport?.scrollTo({ top: viewport.scrollHeight, behavior: "smooth" });
		},
	});

	$effect(() => {
		const element = viewport;
		if (!element) {
			conversation.isAtBottom = true;
			return;
		}

		const updateAtBottom = () => {
			const distanceToBottom =
				element.scrollHeight - element.scrollTop - element.clientHeight;
			conversation.isAtBottom = distanceToBottom < 8;
		};

		updateAtBottom();
		element.addEventListener("scroll", updateAtBottom);

		return () => {
			element.removeEventListener("scroll", updateAtBottom);
		};
	});

	setConversationContext(conversation);
</script>

<div
	bind:this={viewport}
	class={cn("relative flex-1 overflow-y-auto", className)}
	role="log"
	{...restProps}
>
	{@render children?.()}
</div>
