<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
</script>

<pre
	class={cn(
		"mx-4 mb-4 overflow-auto rounded-md bg-muted p-4 font-mono text-sm",
		className,
	)}
	{...restProps}>
	{@render children?.()}
</pre>
