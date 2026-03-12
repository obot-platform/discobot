<script lang="ts">
	import XIcon from "@lucide/svelte/icons/x";
	import { Button } from "$lib/components/ui/button";
	import { InputGroupAddon } from "$lib/components/ui/input-group";
	import type { ComposerAttachment } from "./conversation-composer.types";

	type Props = {
		files: ComposerAttachment[];
		onRemove: (id: string) => void;
	};

	let { files, onRemove }: Props = $props();
</script>

{#if files.length > 0}
	<InputGroupAddon
		align="block-start"
		class="w-full flex-wrap gap-1 border-b border-border px-3 pb-2 pt-3"
	>
		{#each files as file (file.id)}
			<div
				class="inline-flex max-w-[220px] items-center gap-1 rounded-md border border-border bg-background px-2 py-1 text-xs"
			>
				<span class="truncate">{file.filename}</span>
				<Button
					variant="ghost"
					size="icon-xs"
					onclick={() => onRemove(file.id)}
					class="size-4"
					aria-label={`Remove ${file.filename}`}
				>
					<XIcon class="size-3" />
				</Button>
			</div>
		{/each}
	</InputGroupAddon>
{/if}
