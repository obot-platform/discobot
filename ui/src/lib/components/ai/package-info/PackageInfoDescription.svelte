<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<p class={cn("mt-2 text-muted-foreground text-sm", className)} {...restProps}>
	{@render children?.()}
</p>
