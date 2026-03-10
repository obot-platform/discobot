<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
</script>

<p class={cn("font-medium text-red-700 text-sm dark:text-red-400", className)} {...restProps}>
	{@render children?.()}
</p>
