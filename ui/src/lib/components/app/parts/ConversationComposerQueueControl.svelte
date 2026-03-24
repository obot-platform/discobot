<script lang="ts">
	import CheckCircleIcon from "@lucide/svelte/icons/check-circle";
	import { Button } from "$lib/components/ui/button";
	import type { PlanEntry } from "$lib/shell-types";

	type Props = {
		expanded?: boolean;
		entries: PlanEntry[];
	};

	let { expanded = $bindable(false), entries }: Props = $props();

	function queueCompletedCount() {
		return entries.filter((entry) => entry.status === "completed").length;
	}

	function queueTotalCount() {
		return entries.length;
	}
</script>

{#if queueTotalCount() > 0}
	<Button
		variant="ghost"
		size="xs"
		class="h-8 gap-1.5 px-2"
		onclick={() => {
			expanded = !expanded;
		}}
	>
		<CheckCircleIcon class="size-3.5" />
		<span class="text-xs font-medium"
			>{queueCompletedCount()}/{queueTotalCount()}</span
		>
	</Button>
{/if}
