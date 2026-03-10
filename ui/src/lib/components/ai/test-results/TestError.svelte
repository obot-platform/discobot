<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
</script>

<div class={cn("mt-2 rounded-md bg-red-50 p-3 dark:bg-red-900/20", className)} {...restProps}>
	{@render children?.()}
</div>
