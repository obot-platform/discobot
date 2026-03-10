<script lang="ts">
	import type { HTMLAttributes } from "svelte/elements";
	import { cn } from "$lib/utils";

	type Props = HTMLAttributes<HTMLSpanElement> & {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<span
	class={cn("line-clamp-1 flex items-center gap-2", className)}
	data-slot="select-value"
	{...restProps}
>
	{@render children?.()}
</span>
