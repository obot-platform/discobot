<script lang="ts">
	import type { HTMLAttributes } from "svelte/elements";
	import { cn } from "$lib/utils";

	type Props = HTMLAttributes<HTMLElement> & {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<blockquote
	class={cn(
		"border-muted border-l-2 pl-3 text-muted-foreground text-sm italic",
		className,
	)}
	{...restProps}
>
	{@render children?.()}
</blockquote>
