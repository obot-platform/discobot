<script lang="ts">
	import Trash2Icon from "@lucide/svelte/icons/trash-2";

	import { Button } from "$lib/components/ui/button";
	import type { QueuedPrompt } from "$lib/api-types";

	type Props = {
		entries: QueuedPrompt[];
		onDelete: (queueId: string) => void | Promise<void>;
	};

	let { entries, onDelete }: Props = $props();

	function getPromptText(entry: QueuedPrompt): string {
		const parts = entry.message.parts ?? [];
		const text: string[] = [];
		for (const part of parts) {
			if (part.type === "text") {
				const trimmed = part.text.trim();
				if (trimmed.length > 0) {
					text.push(trimmed);
				}
			}
		}
		return text.join(" ") || "Queued prompt";
	}

	function getAttachmentCount(entry: QueuedPrompt): number {
		const parts = entry.message.parts ?? [];
		return parts.filter((part) => part.type === "file").length;
	}
</script>

{#if entries.length > 0}
	<div class="mb-2 rounded-lg border border-border bg-background shadow-sm">
		<div
			class="border-b border-border px-3 py-2 text-xs font-medium text-muted-foreground"
		>
			Queued prompts ({entries.length})
		</div>
		<div class="flex flex-col gap-1 p-1">
			{#each entries as entry (entry.id)}
				<div
					class="flex items-start gap-2 rounded-md px-2 py-2 hover:bg-muted/50"
				>
					<div class="min-w-0 flex-1">
						<div class="truncate text-sm text-foreground">
							{getPromptText(entry)}
						</div>
						<div
							class="mt-1 flex flex-wrap gap-x-3 text-xs text-muted-foreground"
						>
							{#if getAttachmentCount(entry) > 0}
								<span
									>{getAttachmentCount(entry)} attachment{getAttachmentCount(
										entry,
									) === 1
										? ""
										: "s"}</span
								>
							{/if}
							{#if entry.model}
								<span>{entry.model}</span>
							{/if}
							{#if entry.mode}
								<span>{entry.mode}</span>
							{/if}
						</div>
					</div>
					<Button
						variant="ghost"
						size="icon-sm"
						class="shrink-0"
						title="Delete queued prompt"
						onclick={() => {
							void onDelete(entry.id);
						}}
					>
						<Trash2Icon class="size-3.5 text-destructive" />
					</Button>
				</div>
			{/each}
		</div>
	</div>
{/if}
