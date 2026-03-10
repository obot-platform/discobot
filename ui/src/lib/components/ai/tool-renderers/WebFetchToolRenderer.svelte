<script lang="ts">
	import GlobeIcon from "@lucide/svelte/icons/globe";
	import { ToolInput, ToolOutput } from "$lib/components/ai/tool";
	import {
		type WebFetchToolOutput,
		validateWebFetchInput,
		validateWebFetchOutput,
	} from "$lib/components/ai/tool-schemas/webfetch-schema";
	import type { ToolRendererComponentProps } from "./types";

	let { toolPart }: ToolRendererComponentProps = $props();

	const isStreaming = $derived.by(
		() =>
			toolPart.state === "input-streaming" || toolPart.state === "input-available",
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
</script>

{#if !toolPart.input || typeof toolPart.input !== "object"}
	<div class="p-4 text-muted-foreground text-sm">{isStreaming ? "Loading web fetch..." : "No input data"}</div>
{:else if !inputValidation.success || !validInput?.url}
	{#if isStreaming}
		<div class="p-4 text-muted-foreground text-sm">Loading web fetch...</div>
	{:else}
		<ToolInput input={toolPart.input} />
		<ToolOutput output={toolPart.output} errorText={toolPart.errorText} />
	{/if}
{:else}
	<div class="space-y-4 p-4">
		<div class="flex items-center gap-2">
			<GlobeIcon class="size-4 text-muted-foreground" />
			<h4 class="font-medium text-muted-foreground text-xs uppercase tracking-wide">Web fetch</h4>
		</div>

		<div class="rounded-md border bg-muted/30 p-3 text-xs">
			<p class="font-mono">{validInput.url}</p>
			{#if validInput.prompt}
				<p class="mt-2 text-muted-foreground">{validInput.prompt}</p>
			{/if}
		</div>

		{#if validOutput?.content}
			<div class="rounded-md border bg-muted/20 p-3">
				<pre class="max-h-56 overflow-auto whitespace-pre-wrap text-xs"><code>{validOutput.content}</code></pre>
			</div>
		{/if}

		<ToolOutput output={toolPart.output} errorText={toolPart.errorText} />
	</div>
{/if}
