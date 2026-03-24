<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
</script>

<pre
	class={cn(
		"mt-2 overflow-auto font-mono text-red-600 text-xs dark:text-red-400",
		className,
	)}
	{...restProps}>
	{@render children?.()}
</pre>
