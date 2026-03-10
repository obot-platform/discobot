<script lang="ts">
	import FilePenLineIcon from "@lucide/svelte/icons/file-pen-line";
	import { ToolInput, ToolOutput } from "$lib/components/ai/tool";
	import {
		type WriteToolOutput,
		validateWriteInput,
		validateWriteOutput,
	} from "$lib/components/ai/tool-schemas/write-schema";
	import { shortenPath } from "./utils";
	import type { ToolRendererComponentProps } from "./types";

	let { toolPart }: ToolRendererComponentProps = $props();

	const isStreaming = $derived.by(
		() =>
			toolPart.state === "input-streaming" || toolPart.state === "input-available",
	);
	const inputValidation = $derived.by(() => validateWriteInput(toolPart.input));
	const validInput = $derived.by(() =>
		inputValidation.success ? inputValidation.data : undefined,
	);
	const outputValidation = $derived.by(() =>
		toolPart.output ? validateWriteOutput(toolPart.output) : null,
	);
	const validOutput = $derived.by(() =>
		outputValidation?.success
			? (outputValidation.data as WriteToolOutput)
			: undefined,
	);

	const previewContent = $derived.by(() => validInput?.content?.slice(0, 300));
</script>

{#if !toolPart.input || typeof toolPart.input !== "object"}
	<div class="p-4 text-muted-foreground text-sm">
		{isStreaming ? "Loading write details..." : "No input data"}
	</div>
{:else if !inputValidation.success || !validInput?.file_path}
	{#if isStreaming}
		<div class="p-4 text-muted-foreground text-sm">Loading write details...</div>
	{:else}
		<ToolInput input={toolPart.input} />
		<ToolOutput output={toolPart.output} errorText={toolPart.errorText} />
	{/if}
{:else}
	<div class="space-y-4 p-4">
		<div class="flex items-center gap-2">
			<FilePenLineIcon class="size-4 text-muted-foreground" />
			<h4 class="font-medium text-muted-foreground text-xs uppercase tracking-wide">Write file</h4>
		</div>

		<div class="rounded-md border bg-muted/30 p-3">
			<p class="font-mono text-xs text-muted-foreground">{shortenPath(validInput.file_path)}</p>
			{#if previewContent}
				<pre class="mt-2 max-h-40 overflow-auto whitespace-pre-wrap font-mono text-xs"><code>{previewContent}</code></pre>
			{/if}
		</div>

		<div class="flex items-center gap-3 text-xs">
			{#if validOutput?.success !== undefined}
				<span class={validOutput.success ? "text-green-700" : "text-yellow-700"}>
					{validOutput.success ? "Success" : "Completed with warnings"}
				</span>
			{/if}
			{#if validOutput?.bytes_written !== undefined}
				<span class="text-muted-foreground">{validOutput.bytes_written} bytes written</span>
			{/if}
		</div>

		<ToolOutput output={toolPart.output} errorText={toolPart.errorText} />
	</div>
{/if}
