<script lang="ts">
	import { cn } from "$lib/utils";
	import { useTestResultsContext } from "./context";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
	const testResults = useTestResultsContext();

	function formatDuration(ms: number) {
		if (ms < 1000) {
			return `${ms}ms`;
		}
		return `${(ms / 1000).toFixed(2)}s`;
	}
</script>

{#if testResults.summary?.duration}
	<span class={cn("text-muted-foreground text-sm", className)} {...restProps}>
		{#if children}
			{@render children()}
		{:else}
			{formatDuration(testResults.summary.duration)}
		{/if}
	</span>
{/if}
