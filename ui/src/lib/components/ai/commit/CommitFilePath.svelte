<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<span class={cn("truncate font-mono text-xs", className)} {...restProps}>
	{@render children?.()}
</span>
