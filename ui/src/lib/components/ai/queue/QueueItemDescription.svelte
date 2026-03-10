<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = {
		completed?: boolean;
		class?: string;
		children?: () => any;
	};

	let {
		completed = false,
		class: className,
		children,
		...restProps
	}: Props = $props();
</script>

<div
	class={cn(
		"ml-6 text-xs",
		completed
			? "text-muted-foreground/40 line-through"
			: "text-muted-foreground",
		className,
	)}
	{...restProps}
>
	{@render children?.()}
</div>
