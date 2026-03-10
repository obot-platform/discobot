<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		position?: "top" | "right" | "bottom" | "left";
		children?: () => any;
	};

	let {
		class: className,
		position = "bottom",
		children,
		...restProps
	}: Props = $props();
</script>

<div
	class={cn(
		"flex items-center gap-1 rounded-sm border bg-background p-1.5",
		className,
	)}
	data-position={position}
	{...restProps}
>
	{@render children?.()}
</div>
