<script lang="ts">
	import { cn } from "$lib/utils";
	import PromptInputFile from "./PromptInputFile.svelte";
	import { usePromptInputContext } from "./context";

	type Props = {
		class?: string;
	};

	let { class: className, ...restProps }: Props = $props();
	const promptInput = usePromptInputContext();
</script>

{#if promptInput.files.length > 0}
	<div class={cn("flex w-full flex-wrap gap-1 border-b border-border px-3 pb-2 pt-3", className)} {...restProps}>
		{#each promptInput.files as file (file.id)}
			<PromptInputFile {file} />
		{/each}
	</div>
{/if}
