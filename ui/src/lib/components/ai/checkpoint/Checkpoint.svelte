<script lang="ts">
	import { Separator } from "$lib/components/ui/separator";
	import type { HTMLAttributes } from "svelte/elements";
	import { cn, type WithElementRef } from "$lib/utils";

	let {
		ref = $bindable(null),
		class: className,
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLDivElement>> = $props();
</script>

<div
	bind:this={ref}
	class={cn(
		"flex items-center gap-0.5 overflow-hidden text-muted-foreground",
		className,
	)}
	{...restProps}
>
	{@render children?.()}
	<Separator />
</div>
