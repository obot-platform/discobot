<script lang="ts">
	import CheckCircleIcon from "@lucide/svelte/icons/check-circle";
	import { Button } from "$lib/components/ui/button";
	import { useThreadContext } from "$lib/context/thread-context.svelte";

	type Props = {
		expanded?: boolean;
	};

	const thread = useThreadContext();

	let { expanded = $bindable(false) }: Props = $props();

	function queueEntries() {
		return thread.planEntries;
	}

	function queueCompletedCount() {
		return queueEntries().filter((entry) => entry.status === "completed").length;
	}

	function queueTotalCount() {
		return queueEntries().length;
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
		<span class="text-xs font-medium">{queueCompletedCount()}/{queueTotalCount()}</span>
	</Button>
{/if}
