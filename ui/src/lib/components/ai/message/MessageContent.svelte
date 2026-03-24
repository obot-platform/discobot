<script lang="ts">
	import { cn } from "$lib/utils";
	import { onMount } from "svelte";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<div
	class={cn(
		"flex min-w-0 flex-col gap-2 overflow-hidden text-sm",
		"[&>[data-ai-tool]+[data-ai-tool]]:-mt-8 [&>[data-ai-reasoning]+[data-ai-reasoning]]:-mt-8",
		"group-[.is-user]:ml-auto group-[.is-user]:w-fit group-[.is-user]:max-w-full group-[.is-user]:rounded-lg group-[.is-user]:bg-secondary group-[.is-user]:px-4 group-[.is-user]:py-3 group-[.is-user]:text-foreground",
		"group-[.is-assistant]:w-full group-[.is-assistant]:text-foreground",
		className,
	)}
	{...restProps}
>
	{@render children?.()}
</div>
