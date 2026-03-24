<script lang="ts">
	import Loader2Icon from "@lucide/svelte/icons/loader-2";
	import type { PlanEntry } from "$lib/shell-types";

	type Props = {
		expanded: boolean;
		entries: PlanEntry[];
	};

	let { expanded, entries }: Props = $props();

	function completedCount() {
		return entries.filter((entry) => entry.status === "completed").length;
	}
</script>

{#if expanded && entries.length > 0}
	<div class="mb-2 rounded-lg border border-border bg-background shadow-sm">
		<div
			class="border-b border-border px-3 py-2 text-xs font-medium text-muted-foreground"
		>
			Todo ({completedCount()} completed)
		</div>
		<div class="max-h-48 overflow-auto p-1">
			{#each entries as entry, index (entry.content + index)}
				<div
					class={`flex items-center gap-2 rounded-md px-2 py-1.5 text-sm ${entry.status === "in_progress" ? "bg-blue-500/10" : "hover:bg-muted/50"}`}
				>
					{#if entry.status === "in_progress"}
						<Loader2Icon class="size-3 animate-spin text-blue-500" />
					{:else}
						<div
							class={`size-3 rounded-full border ${entry.status === "completed" ? "border-green-500 bg-green-500" : "border-muted-foreground/50"}`}
						></div>
					{/if}
					<div
						class={`flex-1 truncate ${entry.status === "completed" ? "text-muted-foreground line-through" : "text-foreground"}`}
					>
						{entry.content}
					</div>
				</div>
			{/each}
		</div>
	</div>
{/if}
