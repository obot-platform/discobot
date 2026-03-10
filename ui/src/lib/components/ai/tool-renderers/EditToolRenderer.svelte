<script lang="ts">
	import FilePenIcon from "@lucide/svelte/icons/file-pen";
	import { ToolInput, ToolOutput } from "$lib/components/ai/tool";
	import {
		type EditToolOutput,
		validateEditInput,
		validateEditOutput,
	} from "$lib/components/ai/tool-schemas/edit-schema";
	import { shortenPath } from "./utils";
	import type { ToolRendererComponentProps } from "./types";

	let { toolPart }: ToolRendererComponentProps = $props();

	const isStreaming = $derived.by(
		() =>
			toolPart.state === "input-streaming" || toolPart.state === "input-available",
	);
	const inputValidation = $derived.by(() => validateEditInput(toolPart.input));
	const validInput = $derived.by(() =>
		inputValidation.success ? inputValidation.data : undefined,
	);
	const outputValidation = $derived.by(() =>
		toolPart.output ? validateEditOutput(toolPart.output) : null,
	);
	const validOutput = $derived.by(() =>
		outputValidation?.success ? (outputValidation.data as EditToolOutput) : undefined,
	);
</script>

{#if !toolPart.input || typeof toolPart.input !== "object"}
	<div class="p-4 text-muted-foreground text-sm">
		{isStreaming ? "Loading edit details..." : "No input data"}
	</div>
{:else if !inputValidation.success || !validInput?.file_path}
	{#if isStreaming}
		<div class="p-4 text-muted-foreground text-sm">Loading edit details...</div>
	{:else}
		<ToolInput input={toolPart.input} />
		<ToolOutput output={toolPart.output} errorText={toolPart.errorText} />
	{/if}
{:else}
	<div class="space-y-4 p-4">
		<div class="flex items-center gap-2">
			<FilePenIcon class="size-4 text-muted-foreground" />
			<h4 class="font-medium text-muted-foreground text-xs uppercase tracking-wide">Edit file</h4>
		</div>

		<div class="rounded-md border bg-muted/30 p-3 text-xs">
			<p class="font-mono text-muted-foreground">{shortenPath(validInput.file_path)}</p>
			<div class="mt-2 space-y-1">
				<p>replace_all: {validInput.replace_all ? "true" : "false"}</p>
				<p>old length: {validInput.old_string?.length ?? 0}</p>
				<p>new length: {validInput.new_string?.length ?? 0}</p>
			</div>
		</div>

		{#if validOutput?.replacements !== undefined}
			<p class="text-muted-foreground text-xs">{validOutput.replacements} replacements applied</p>
		{/if}

		<ToolOutput output={toolPart.output} errorText={toolPart.errorText} />
	</div>
{/if}
