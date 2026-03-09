<script lang="ts">
	import { Separator } from "$lib/components/ui/separator";

	type Props = {
		allThreads: string[];
		currentThread: string;
		onSelectThread: (thread: string) => void;
		recentThreads: string[];
	};

	let { allThreads, currentThread, onSelectThread, recentThreads }: Props = $props();
</script>

<aside class="border-r border-border bg-sidebar/70 p-4">
	<div class="space-y-4">
		<div class="space-y-2">
			<p class="text-xs font-medium uppercase tracking-[0.22em] text-muted-foreground">
				Recent
			</p>
			{#each recentThreads as thread}
				<button
					type="button"
					onclick={() => onSelectThread(thread)}
					class={`block w-full rounded-xl border px-3 py-2 text-left text-sm ${currentThread === thread ? "border-primary bg-primary/10 text-foreground" : "border-border bg-card text-muted-foreground"}`}
				>
					{thread}
				</button>
			{/each}
		</div>

		<Separator />

		<div class="space-y-2">
			<p class="text-xs font-medium uppercase tracking-[0.22em] text-muted-foreground">
				All threads
			</p>
			{#each allThreads as thread}
				<button
					type="button"
					onclick={() => onSelectThread(thread)}
					class={`block w-full rounded-xl border px-3 py-2 text-left text-sm ${currentThread === thread ? "border-border bg-card text-foreground" : "border-border bg-card/70 text-muted-foreground"}`}
				>
					{thread}
				</button>
			{/each}
		</div>
	</div>
</aside>
