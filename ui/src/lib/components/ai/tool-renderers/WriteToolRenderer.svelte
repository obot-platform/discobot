<script lang="ts">
	import FilePenLineIcon from "@lucide/svelte/icons/file-pen-line";
	import {
		ToolContent,
		ToolHeaderControls,
		ToolHeaderStatus,
	} from "$lib/components/ai/tool";
	import {
		type WriteToolOutput,
		validateWriteInput,
		validateWriteOutput,
	} from "$lib/components/ai/tool-schemas/write-schema";
	import { CollapsibleTrigger } from "$lib/components/ui/collapsible";
	import type { ToolRendererComponentProps } from "./types";
	import {
		countLines,
		getToolInputString,
		renderToolValue,
		shortenPath,
	} from "./utils";

	let { toolPart, isRaw, onToggleRaw }: ToolRendererComponentProps = $props();

	const isStreaming = $derived.by(
		() =>
			toolPart.state === "input-streaming" ||
			toolPart.state === "input-available",
	);
	const headerFilePath = $derived.by(() =>
		getToolInputString(toolPart.input, "file_path"),
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
	const previewContent = $derived.by(
		() => validInput?.content?.slice(0, 400) || "",
	);
	const writeError = $derived.by(
		() => toolPart.errorText || validOutput?.error,
	);
	const rawOutputText = $derived.by(() => renderToolValue(toolPart.output));
</script>

<div class="flex items-center justify-between gap-4 px-4 pt-4">
	<CollapsibleTrigger
		class="flex min-w-0 flex-1 items-center gap-2 text-left text-muted-foreground"
	>
		<FilePenLineIcon class="size-4 shrink-0 text-muted-foreground" />
		<span class="truncate font-medium text-sm">
			{headerFilePath
				? shortenPath(headerFilePath)
				: isStreaming
					? "Loading write details..."
					: "Write file"}
		</span>
		<ToolHeaderStatus state={toolPart.state} />
	</CollapsibleTrigger>
	<ToolHeaderControls {isRaw} {onToggleRaw} />
</div>

<ToolContent>
	{#if !toolPart.input || typeof toolPart.input !== "object"}
		<div class="p-4 pt-3 text-muted-foreground text-sm">
			{isStreaming
				? "Loading write details..."
				: "Write details are unavailable."}
		</div>
	{:else if !inputValidation.success || !validInput?.file_path}
		<div class="space-y-3 p-4 pt-3">
			<p class="text-muted-foreground text-sm">
				{isStreaming
					? "Loading write details..."
					: "Could not parse write details."}
			</p>
			{#if rawOutputText}
				<div class="rounded-md border border-dashed bg-muted/20 p-3">
					<pre
						class="overflow-x-auto whitespace-pre-wrap break-words font-mono text-xs"><code
							>{rawOutputText}</code
						></pre>
				</div>
			{/if}
		</div>
	{:else}
		<div class="space-y-4 p-4 pt-3">
			<div class="space-y-3 rounded-md border bg-muted/20 p-3">
				<p class="font-mono text-xs text-muted-foreground">
					{shortenPath(validInput.file_path)}
				</p>
				<div class="flex flex-wrap gap-2 text-xs">
					<span class="rounded-full bg-muted px-2 py-0.5 text-muted-foreground">
						{validInput.content?.length ?? 0} chars
					</span>
					<span class="rounded-full bg-muted px-2 py-0.5 text-muted-foreground">
						{countLines(validInput.content ?? "")} lines
					</span>
				</div>
				{#if previewContent}
					<div class="rounded-md border bg-background/80">
						<pre
							class="max-h-56 overflow-auto whitespace-pre-wrap break-words p-3 font-mono text-xs"><code
								>{previewContent}</code
							></pre>
					</div>
				{/if}
			</div>

			<div class="flex flex-wrap items-center gap-3 text-xs">
				{#if validOutput?.success !== undefined}
					<span
						class={validOutput.success ? "text-green-700" : "text-yellow-700"}
					>
						{validOutput.success
							? "Write completed"
							: "Write returned warnings"}
					</span>
				{/if}
				{#if validOutput?.bytes_written !== undefined}
					<span class="text-muted-foreground"
						>{validOutput.bytes_written} bytes written</span
					>
				{/if}
			</div>

			{#if writeError}
				<div
					class="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-destructive text-sm"
				>
					{writeError}
				</div>
			{/if}

			{#if outputValidation && !outputValidation.success && rawOutputText}
				<div class="rounded-md border border-dashed bg-muted/20 p-3">
					<h5
						class="mb-2 font-medium text-muted-foreground text-xs uppercase tracking-wide"
					>
						Unparsed output
					</h5>
					<pre
						class="overflow-x-auto whitespace-pre-wrap break-words font-mono text-xs"><code
							>{rawOutputText}</code
						></pre>
				</div>
			{/if}
		</div>
	{/if}
</ToolContent>
