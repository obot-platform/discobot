<script lang="ts">
	import type { HTMLAttributes } from "svelte/elements";
	import { cn } from "$lib/utils";

	type Props = HTMLAttributes<HTMLSpanElement> & {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<span class={cn("group inline items-center gap-1", className)} {...restProps}>
	{@render children?.()}
</span>
