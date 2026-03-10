<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<div
	class={cn("flex shrink-0 items-center gap-1 font-mono text-xs", className)}
	{...restProps}
>
	{@render children?.()}
</div>
