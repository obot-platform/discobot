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
		"flex items-center justify-between border-zinc-800 border-b px-4 py-2",
		className,
	)}
	{...restProps}
>
	{@render children?.()}
</div>
