<script lang="ts">
	import { cn } from "$lib/utils";
	import { setTestResultsContext, type TestResultsSummary } from "./context";

	type Props = {
		summary?: TestResultsSummary;
		class?: string;
		children?: () => any;
	};

	let { summary, class: className, children, ...restProps }: Props = $props();
	const testResults = $state({
		summary: undefined as TestResultsSummary | undefined,
	});
	$effect(() => {
		testResults.summary = summary;
	});
	setTestResultsContext(testResults);
</script>

<div class={cn("rounded-lg border bg-background", className)} {...restProps}>
	{@render children?.()}
</div>
