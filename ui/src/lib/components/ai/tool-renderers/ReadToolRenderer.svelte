<script lang="ts">
	import EyeIcon from "@lucide/svelte/icons/eye";
	import FileTextIcon from "@lucide/svelte/icons/file-text";
	import {
		ToolContent,
		ToolHeaderControls,
		ToolHeaderStatus,
	} from "$lib/components/ai/tool";
	import {
		type ReadToolOutput,
		validateReadInput,
		validateReadOutput,
	} from "$lib/components/ai/tool-schemas/read-schema";
	import { CollapsibleTrigger } from "$lib/components/ui/collapsible";
	import type { ToolRendererComponentProps } from "./types";
	import {
		countLines,
		parseNumberedToolOutput,
		renderToolValue,
		shortenPath,
	} from "./utils";

	let { toolPart, isRaw, onToggleRaw }: ToolRendererComponentProps = $props();

	const isStreaming = $derived.by(
		() =>
			toolPart.state === "input-streaming" ||
			toolPart.state === "input-available",
	);
	const inputValidation = $derived.by(() => validateReadInput(toolPart.input));
	const validInput = $derived.by(() =>
		inputValidation.success ? inputValidation.data : undefined,
	);
	const outputValidation = $derived.by(() =>
		toolPart.output ? validateReadOutput(toolPart.output) : null,
	);
	const validOutput = $derived.by(() =>
		outputValidation?.success
			? (outputValidation.data as ReadToolOutput)
			: undefined,
	);
	const content = $derived.by(
		() => validOutput?.content || validOutput?.lines?.join("\n") || "",
	);
	const parsedContent = $derived.by(() => parseNumberedToolOutput(content));
	const hasParsedContentLines = $derived.by(() => parsedContent.lines.length > 0);
	const displayedLineCount = $derived.by(() =>
		hasParsedContentLines ? parsedContent.lines.length : countLines(content),
	);
	const fileName = $derived.by(() => {
		if (!validInput?.file_path) {
			return undefined;
		}
		return validInput.file_path.split("/").pop() || validInput.file_path;
	});
	const readError = $derived.by(() => toolPart.errorText || validOutput?.error);
	const rawOutputText = $derived.by(() => renderToolValue(toolPart.output));
</script>

<div class="flex items-center justify-between gap-4 px-4 pt-4">
	<CollapsibleTrigger class="flex min-w-0 flex-1 items-center gap-2 text-left">
		<FileTextIcon class="size-4 shrink-0 text-muted-foreground" />
		<span class="truncate font-medium text-sm">
			{fileName || (isStreaming ? "Loading file details..." : "Reading file")}
		</span>
		<ToolHeaderStatus state={toolPart.state} />
	</CollapsibleTrigger>
	<ToolHeaderControls {isRaw} {onToggleRaw} />
</div>

<ToolContent>
	{#if !toolPart.input || typeof toolPart.input !== "object"}
		<div class="p-4 pt-3 text-muted-foreground text-sm">
			{isStreaming
				? "Loading file details..."
				: "File details are unavailable."}
		</div>
	{:else if !inputValidation.success || !validInput?.file_path}
		<div class="space-y-3 p-4 pt-3">
			<p class="text-muted-foreground text-sm">
				{isStreaming
					? "Loading file details..."
					: "Could not parse file details."}
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
			<div class="space-y-2">
				<div class="flex flex-wrap items-center gap-2 text-sm">
					<code class="rounded bg-muted px-2 py-1 font-mono text-foreground"
						>{fileName}</code
					>
					{#if validInput.offset !== undefined}
						<span class="text-muted-foreground text-xs"
							>offset: {validInput.offset}</span
						>
					{/if}
					{#if validInput.limit !== undefined}
						<span class="text-muted-foreground text-xs"
							>limit: {validInput.limit}</span
						>
					{/if}
					{#if validInput.pages}
						<span class="text-muted-foreground text-xs"
							>pages: {validInput.pages}</span
						>
					{/if}
				</div>
				<div class="font-mono text-muted-foreground text-xs">
					{shortenPath(validInput.file_path)}
				</div>
			</div>

			{#if content}
				<div class="space-y-2">
					<div class="flex items-center gap-2">
						<EyeIcon class="size-4 text-muted-foreground" />
						<h4
							class="font-medium text-muted-foreground text-xs uppercase tracking-wide"
						>
							Content
						</h4>
						<span class="text-muted-foreground text-xs"
							>{displayedLineCount} lines</span
						>
						{#if parsedContent.isTruncated}
							<span class="rounded-full bg-muted px-2 py-0.5 text-muted-foreground text-xs">
								Truncated
							</span>
						{/if}
					</div>
					<div class="rounded-md border bg-muted/30">
						{#if parsedContent.isTruncated}
							<div class="border-b px-3 py-2 text-muted-foreground text-xs">
								Output truncated{#if parsedContent.truncationFilePath}
									— full output written to {shortenPath(parsedContent.truncationFilePath)}
								{/if}
							</div>
						{/if}
						{#if hasParsedContentLines}
							<div
								class="overflow-x-auto p-3 font-mono text-xs text-foreground"
							>
								<div class="grid min-w-max grid-cols-[auto_1fr] gap-x-3">
									{#each parsedContent.lines as line}
										<div
											class="select-none text-muted-foreground/60 text-right"
										>
											{line.lineNumber}
										</div>
										<div class="whitespace-pre-wrap break-words">
											{line.text || " "}
										</div>
									{/each}
								</div>
							</div>
						{:else}
							<pre
								class="overflow-x-auto p-3 font-mono text-xs text-foreground"><code
									>{content}</code
								></pre>
						{/if}
					</div>
				</div>
			{:else if outputValidation?.success && !readError}
				<div
					class="rounded-md border border-dashed px-3 py-2 text-muted-foreground text-sm"
				>
					Read completed without file content.
				</div>
			{/if}

			{#if readError}
				<div
					class="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-destructive text-sm"
				>
					{readError}
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
