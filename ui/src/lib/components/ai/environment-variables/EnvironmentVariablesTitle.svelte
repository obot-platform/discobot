<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
</script>

<h3 class={cn("font-medium text-sm", className)} {...restProps}>
	{#if children}
		{@render children()}
	{:else}
		Environment Variables
	{/if}
</h3>
