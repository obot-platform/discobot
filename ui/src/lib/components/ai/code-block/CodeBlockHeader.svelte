<script lang="ts">
	import type { HTMLAttributes } from "svelte/elements";
	import { cn } from "$lib/utils";

	type Props = HTMLAttributes<HTMLDivElement> & {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<div
	class={cn(
		"flex items-center justify-between bg-muted/80 px-3 py-2 text-muted-foreground text-xs",
		className,
	)}
	{...restProps}
>
	{@render children?.()}
</div>
