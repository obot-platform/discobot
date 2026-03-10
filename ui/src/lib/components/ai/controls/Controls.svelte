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
		"flex items-center gap-px overflow-hidden rounded-md border bg-card p-1",
		"[&_button]:rounded-md [&_button]:border-none [&_button]:bg-transparent [&_button]:hover:bg-secondary",
		className,
	)}
	{...restProps}
>
	{@render children?.()}
</div>
