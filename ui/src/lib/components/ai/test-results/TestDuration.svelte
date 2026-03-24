<script lang="ts">
	import { cn } from "$lib/utils";
	import { useTestContext } from "./context";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
	const test = useTestContext();
</script>

{#if test.duration !== undefined}
	<span
		class={cn("ml-auto text-muted-foreground text-xs", className)}
		{...restProps}
	>
		{#if children}
			{@render children()}
		{:else}
			{test.duration}ms
		{/if}
	</span>
{/if}
