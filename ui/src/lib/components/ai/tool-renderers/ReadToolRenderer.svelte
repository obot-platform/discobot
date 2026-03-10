<script lang="ts">
	import EyeIcon from "@lucide/svelte/icons/eye";
	import FileTextIcon from "@lucide/svelte/icons/file-text";
	import { ToolInput, ToolOutput } from "$lib/components/ai/tool";
	import {
		type ReadToolOutput,
		validateReadInput,
		validateReadOutput,
	} from "$lib/components/ai/tool-schemas/read-schema";
	import { shortenPath } from "./utils";
	import type { ToolRendererComponentProps } from "./types";

	let { toolPart }: ToolRendererComponentProps = $props();

	const isStreaming = $derived.by(
		() =>
			toolPart.state === "input-streaming" || toolPart.state === "input-available",
	);

	const inputValidation = $derived.by(() => validateReadInput(toolPart.input));
	const validInput = $derived.by(() =>
		inputValidation.success ? inputValidation.data : undefined,
	);

	const outputValidation = $derived.by(() =>
		toolPart.output ? validateReadOutput(toolPart.output) : null,
	);
	const validOutput = $derived.by(() =>
		outputValidation?.success ? (outputValidation.data as ReadToolOutput) : undefined,
	);

	const content = $derived.by(() =>
		validOutput?.content || validOutput?.lines?.join("\n") || "",
	);

	const fileName = $derived.by(() => {
		if (!validInput?.file_path) {
			return undefined;
		}
		return validInput.file_path.split("/").pop() || validInput.file_path;
	});
</script>

{#if !toolPart.input || typeof toolPart.input !== "object"}
	<div class="p-4 text-muted-foreground text-sm">
		{isStreaming ? "Loading file details..." : "No input data"}
	</div>
{:else if !inputValidation.success || !validInput?.file_path}
	{#if isStreaming}
		<div class="p-4 text-muted-foreground text-sm">Loading file details...</div>
	{:else}
		<ToolInput input={toolPart.input} />
		<ToolOutput output={toolPart.output} errorText={toolPart.errorText} />
	{/if}
{:else}
	<div class="space-y-4 p-4">
		<div class="space-y-2">
			<div class="flex items-center gap-2">
				<FileTextIcon class="size-4 text-muted-foreground" />
				<h4 class="font-medium text-muted-foreground text-xs uppercase tracking-wide">Reading File</h4>
			</div>

			<div class="flex flex-wrap items-center gap-2 text-sm">
				<code class="rounded bg-muted px-2 py-1 font-mono text-foreground">{fileName}</code>
				{#if validInput.offset !== undefined}
					<span class="text-muted-foreground text-xs">offset: {validInput.offset}</span>
				{/if}
				{#if validInput.limit !== undefined}
					<span class="text-muted-foreground text-xs">limit: {validInput.limit}</span>
				{/if}
				{#if validInput.pages}
					<span class="text-muted-foreground text-xs">pages: {validInput.pages}</span>
				{/if}
			</div>

			<div class="font-mono text-muted-foreground text-xs">{shortenPath(validInput.file_path)}</div>
		</div>

		{#if content}
			<div class="space-y-2">
				<div class="flex items-center gap-2">
					<EyeIcon class="size-4 text-muted-foreground" />
					<h4 class="font-medium text-muted-foreground text-xs uppercase tracking-wide">Content</h4>
					<span class="text-muted-foreground text-xs">{content.split("\n").length} lines</span>
				</div>
				<div class="rounded-md bg-muted/50">
					<pre class="overflow-x-auto p-3 font-mono text-xs text-foreground"><code>{content}</code></pre>
				</div>
			</div>
		{/if}

		{#if toolPart.errorText}
			<div class="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-destructive text-sm">
				{toolPart.errorText}
			</div>
		{/if}
	</div>
{/if}
