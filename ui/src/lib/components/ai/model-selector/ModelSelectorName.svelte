<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<span class={cn("flex-1 truncate text-left", className)} {...restProps}>
	{@render children?.()}
</span>
