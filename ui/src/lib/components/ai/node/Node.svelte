<script lang="ts">
	import { Card } from "$lib/components/ui/card";
	import { cn } from "$lib/utils";

	type Props = {
		handles: {
			target: boolean;
			source: boolean;
		};
		class?: string;
		children?: () => any;
	};

	let { handles, class: className, children, ...restProps }: Props = $props();
</script>

<Card
	class={cn(
		"node-container relative size-full h-auto w-sm gap-0 rounded-md p-0",
		className,
	)}
	{...restProps}
>
	{#if handles.target}
		<div class="absolute top-1/2 -left-2 size-3 -translate-y-1/2 rounded-full border bg-background"></div>
	{/if}
	{#if handles.source}
		<div class="absolute top-1/2 -right-2 size-3 -translate-y-1/2 rounded-full border bg-background"></div>
	{/if}
	{@render children?.()}
</Card>
