<script lang="ts">
	import GlobeIcon from "@lucide/svelte/icons/globe";
	import { MessageResponse } from "$lib/components/ai/message";
	import {
		ToolContent,
		ToolHeaderControls,
		ToolHeaderStatus,
	} from "$lib/components/ai/tool";
	import {
		type WebFetchToolOutput,
		validateWebFetchInput,
		validateWebFetchOutput,
	} from "$lib/components/ai/tool-schemas/webfetch-schema";
	import { CollapsibleTrigger } from "$lib/components/ui/collapsible";
	import type { ToolRendererComponentProps } from "./types";
	import { renderToolValue } from "./utils";

	let { toolPart, isRaw, onToggleRaw }: ToolRendererComponentProps = $props();

	const isStreaming = $derived.by(
		() =>
			toolPart.state === "input-streaming" ||
			toolPart.state === "input-available",
	);
	const inputValidation = $derived.by(() =>
		validateWebFetchInput(toolPart.input),
	);
	const validInput = $derived.by(() =>
		inputValidation.success ? inputValidation.data : undefined,
	);
	const outputValidation = $derived.by(() =>
		toolPart.output ? validateWebFetchOutput(toolPart.output) : null,
	);
	const validOutput = $derived.by(() =>
		outputValidation?.success
			? (outputValidation.data as WebFetchToolOutput)
			: undefined,
	);
	const fetchError = $derived.by(
		() => toolPart.errorText || validOutput?.error,
	);
	const rawOutputText = $derived.by(() => renderToolValue(toolPart.output));
</script>

<div class="flex items-center justify-between gap-4 px-4 pt-4">
	<CollapsibleTrigger class="flex min-w-0 flex-1 items-center gap-2 text-left">
		<GlobeIcon class="size-4 shrink-0 text-muted-foreground" />
		<span class="truncate font-medium text-sm">
			{validInput?.url || (isStreaming ? "Loading web fetch..." : "Web fetch")}
		</span>
		<ToolHeaderStatus state={toolPart.state} />
	</CollapsibleTrigger>
	<ToolHeaderControls {isRaw} {onToggleRaw} />
</div>

<ToolContent>
	{#if !toolPart.input || typeof toolPart.input !== "object"}
		<div class="p-4 pt-3 text-muted-foreground text-sm">
			{isStreaming ? "Loading web fetch..." : "Fetch details are unavailable."}
		</div>
	{:else if !inputValidation.success || !validInput?.url}
		<div class="space-y-3 p-4 pt-3">
			<p class="text-muted-foreground text-sm">
				{isStreaming
					? "Loading web fetch..."
					: "Could not parse fetch details."}
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
				<p class="break-all font-mono">{validInput.url}</p>
				{#if validInput.prompt}
					<p class="mt-2 text-muted-foreground">{validInput.prompt}</p>
				{/if}
			</div>

			{#if validOutput?.content}
				<div class="rounded-md border bg-muted/20 p-3 text-sm">
					<MessageResponse text={validOutput.content} />
				</div>
			{:else if outputValidation?.success && !fetchError}
				<div
					class="rounded-md border border-dashed px-3 py-2 text-muted-foreground text-sm"
				>
					Fetch completed without content.
				</div>
			{/if}

			{#if fetchError}
				<div
					class="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-destructive text-sm"
				>
					{fetchError}
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
