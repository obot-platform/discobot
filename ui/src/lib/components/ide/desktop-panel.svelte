<script lang="ts">
	import { Button } from "$lib/components/ui/button";

	type Props = {
		onClose: () => void;
		suggestedPrompts: string[];
	};

	let { onClose, suggestedPrompts }: Props = $props();
</script>

<div class="space-y-4">
	<div class="flex items-center justify-between gap-3">
		<div>
			<p class="text-sm font-medium">Desktop sidecar</p>
			<p class="mt-1 text-xs text-muted-foreground">
				Desktop content stays visible beside the conversation.
			</p>
		</div>
		<Button variant="outline" size="sm" onclick={onClose}>Close panel</Button>
	</div>
	<div class="rounded-2xl border border-dashed border-border bg-background/70 p-4">
		<div class="space-y-3 text-sm text-muted-foreground">
			<div class="rounded-xl border border-border bg-card/70 px-3 py-2">Header + service group</div>
			<div class="rounded-xl border border-border bg-card/70 px-3 py-2">Chat left pane remains active</div>
			<div class="rounded-xl border border-border bg-card/70 px-3 py-2">Desktop preview right pane</div>
		</div>
	</div>
	<div class="space-y-2">
		<p class="text-xs font-medium uppercase tracking-[0.2em] text-muted-foreground">
			Suggested prompts
		</p>
		{#each suggestedPrompts as prompt}
			<div class="rounded-xl border border-border bg-background/70 px-3 py-3 text-sm text-muted-foreground">
				{prompt}
			</div>
		{/each}
	</div>
</div>
