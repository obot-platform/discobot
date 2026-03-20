<script lang="ts">
	import type { MessageRole } from "$lib/components/ai/types";
	import { cn } from "$lib/utils";

	type Props = {
		from: MessageRole;
		class?: string;
		children?: () => any;
		"data-conversation-message-id"?: string;
	};

	let { from, class: className, children, ...restProps }: Props = $props();
</script>

<div
	class={cn(
		"group flex w-full flex-col gap-2",
		from === "user"
			? "is-user ml-auto max-w-[95%] justify-end"
			: "is-assistant",
		className,
	)}
	{...restProps}
>
	{@render children?.()}
</div>
