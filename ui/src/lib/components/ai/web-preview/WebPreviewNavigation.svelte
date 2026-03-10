<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<div class={cn("flex items-center gap-1 border-b p-2", className)} {...restProps}>
	{@render children?.()}
</div>
