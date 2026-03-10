<script lang="ts">
	import { cn } from "$lib/utils";
	import { useTestResultsContext } from "./context";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
	const testResults = useTestResultsContext();

	const passedPercent = $derived.by(() => {
		const summary = testResults.summary;
		if (!summary || summary.total === 0) {
			return 0;
		}
		return (summary.passed / summary.total) * 100;
	});

	const failedPercent = $derived.by(() => {
		const summary = testResults.summary;
		if (!summary || summary.total === 0) {
			return 0;
		}
		return (summary.failed / summary.total) * 100;
	});
</script>

{#if testResults.summary}
	<div class={cn("space-y-2", className)} {...restProps}>
		{#if children}
			{@render children()}
		{:else}
			<div class="flex h-2 overflow-hidden rounded-full bg-muted">
				<div class="bg-green-500 transition-all" style={`width:${passedPercent}%`}></div>
				<div class="bg-red-500 transition-all" style={`width:${failedPercent}%`}></div>
			</div>
			<div class="flex justify-between text-muted-foreground text-xs">
				<span>{testResults.summary.passed}/{testResults.summary.total} tests passed</span>
				<span>{passedPercent.toFixed(0)}%</span>
			</div>
		{/if}
	</div>
{/if}
