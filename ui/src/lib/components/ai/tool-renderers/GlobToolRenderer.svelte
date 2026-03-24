<script lang="ts">
	import FolderSearchIcon from "@lucide/svelte/icons/folder-search";
	import {
		ToolContent,
		ToolHeaderControls,
		ToolHeaderStatus,
	} from "$lib/components/ai/tool";
	import {
		type GlobToolOutput,
		validateGlobInput,
		validateGlobOutput,
	} from "$lib/components/ai/tool-schemas/glob-schema";
	import { CollapsibleTrigger } from "$lib/components/ui/collapsible";
	import type { ToolRendererComponentProps } from "./types";
	import { renderToolValue, shortenPath } from "./utils";

	let { toolPart, isRaw, onToggleRaw }: ToolRendererComponentProps = $props();

	const isStreaming = $derived.by(
		() =>
			toolPart.state === "input-streaming" ||
			toolPart.state === "input-available",
	);
	const inputValidation = $derived.by(() => validateGlobInput(toolPart.input));
	const validInput = $derived.by(() =>
		inputValidation.success ? inputValidation.data : undefined,
	);
	const outputValidation = $derived.by(() =>
		toolPart.output ? validateGlobOutput(toolPart.output) : null,
	);
	const validOutput = $derived.by(() =>
		outputValidation?.success
			? (outputValidation.data as GlobToolOutput)
			: undefined,
	);
	const rawOutputText = $derived.by(() => renderToolValue(toolPart.output));
</script>

<div class="flex items-center justify-between gap-4 px-4 pt-4">
	<CollapsibleTrigger
		class="flex min-w-0 flex-1 items-center gap-2 text-left text-muted-foreground"
	>
		<FolderSearchIcon class="size-4 shrink-0 text-muted-foreground" />
		<span class="truncate font-medium text-sm">
			{validInput?.pattern || (isStreaming ? "Loading file search..." : "Glob")}
		</span>
		<ToolHeaderStatus state={toolPart.state} />
	</CollapsibleTrigger>
	<ToolHeaderControls {isRaw} {onToggleRaw} />
</div>

<ToolContent>
	{#if !toolPart.input || typeof toolPart.input !== "object"}
		<div class="p-4 pt-3 text-muted-foreground text-sm">
			{isStreaming
				? "Loading file search..."
				: "File search details are unavailable."}
		</div>
	{:else if !inputValidation.success || !validInput?.pattern}
		<div class="space-y-3 p-4 pt-3">
			<p class="text-muted-foreground text-sm">
				{isStreaming
					? "Loading file search..."
					: "Could not parse file search details."}
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
			<div class="rounded-md border bg-muted/30 p-3 text-xs">
				<p>
					<span class="text-muted-foreground">pattern:</span>
					<code>{validInput.pattern}</code>
				</p>
				{#if validInput.path}
					<p class="mt-1">
						<span class="text-muted-foreground">path:</span>
						<code>{shortenPath(validInput.path)}</code>
					</p>
				{/if}
			</div>

			{#if validOutput?.files?.length}
				<div class="space-y-2">
					<p class="text-muted-foreground text-xs">
						{validOutput.files.length} files matched
					</p>
					<div class="rounded-md border bg-muted/20 p-2">
						{#each validOutput.files as file}
							<div class="border-b px-2 py-1 font-mono text-xs last:border-b-0">
								{shortenPath(file)}
							</div>
						{/each}
					</div>
				</div>
			{:else if validOutput?.content}
				<div class="rounded-md border bg-muted/20 p-3">
					<pre
						class="overflow-x-auto whitespace-pre-wrap break-words font-mono text-xs"><code
							>{validOutput.content}</code
						></pre>
				</div>
			{:else if outputValidation?.success}
				<div
					class="rounded-md border border-dashed px-3 py-2 text-muted-foreground text-sm"
				>
					No files matched.
				</div>
			{/if}

			{#if toolPart.errorText}
				<div
					class="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-destructive text-sm"
				>
					{toolPart.errorText}
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
