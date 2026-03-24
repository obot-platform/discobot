<script lang="ts">
	import CheckCircle2Icon from "@lucide/svelte/icons/check-circle-2";
	import CircleIcon from "@lucide/svelte/icons/circle";
	import XCircleIcon from "@lucide/svelte/icons/x-circle";
	import { Badge } from "$lib/components/ui/badge";
	import { cn } from "$lib/utils";
	import { useTestResultsContext } from "./context";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
	const testResults = useTestResultsContext();
</script>

{#if testResults.summary}
	<div class={cn("flex items-center gap-3", className)} {...restProps}>
		{#if children}
			{@render children()}
		{:else}
			<Badge
				class="gap-1 bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400"
				variant="secondary"
			>
				<CheckCircle2Icon class="size-3" />
				{testResults.summary.passed} passed
			</Badge>
			{#if testResults.summary.failed > 0}
				<Badge
					class="gap-1 bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400"
					variant="secondary"
				>
					<XCircleIcon class="size-3" />
					{testResults.summary.failed} failed
				</Badge>
			{/if}
			{#if testResults.summary.skipped > 0}
				<Badge
					class="gap-1 bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400"
					variant="secondary"
				>
					<CircleIcon class="size-3" />
					{testResults.summary.skipped} skipped
				</Badge>
			{/if}
		{/if}
	</div>
{/if}
