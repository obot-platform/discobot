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
		"flex w-full items-center border-border border-t border-b",
		className,
	)}
	{...restProps}
>
	{@render children?.()}
</div>
