<script lang="ts">
	import XIcon from "@lucide/svelte/icons/x";
	import { Button } from "$lib/components/ui/button";
	import { cn } from "$lib/utils";
	import type { PromptInputFile } from "./context";
	import { usePromptInputContext } from "./context";

	type Props = {
		file: PromptInputFile;
		class?: string;
	};

	let { file, class: className, ...restProps }: Props = $props();
	const promptInput = usePromptInputContext();
</script>

<div
	class={cn(
		"inline-flex max-w-[220px] items-center gap-1 rounded-md border border-border bg-background px-2 py-1 text-xs",
		className,
	)}
	{...restProps}
>
	<span class="truncate">{file.filename ?? "Attachment"}</span>
	<Button
		variant="ghost"
		size="icon-xs"
		class="size-4"
		onclick={() => promptInput.removeFile(file.id)}
		aria-label={`Remove ${file.filename ?? "attachment"}`}
	>
		<XIcon class="size-3" />
	</Button>
</div>
