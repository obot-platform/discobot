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

<span
	class={cn(
		"line-clamp-1 grow break-words",
		completed
			? "text-muted-foreground/50 line-through"
			: "text-muted-foreground",
		className,
	)}
	{...restProps}
>
	{@render children?.()}
</span>
