<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = {
		input: unknown;
		class?: string;
	};

	let { input, class: className, ...restProps }: Props = $props();
	const rendered = $derived.by(() => JSON.stringify(input, null, 2) ?? "");
</script>

<div class={cn("space-y-2 overflow-hidden p-4", className)} {...restProps}>
	<h4 class="font-medium text-muted-foreground text-xs uppercase tracking-wide">Parameters</h4>
	<div class="rounded-md bg-muted/50">
		<pre class="overflow-x-auto p-3 font-mono text-xs text-foreground"><code>{rendered}</code></pre>
	</div>
</div>
