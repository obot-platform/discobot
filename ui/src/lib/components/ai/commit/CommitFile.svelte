<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<div
	class={cn(
		"flex items-center justify-between gap-2 rounded px-2 py-1 text-sm hover:bg-muted/50",
		className,
	)}
	{...restProps}
>
	{@render children?.()}
</div>
