<script lang="ts">
	import GlobeIcon from "@lucide/svelte/icons/globe";
	import { ToolInput, ToolOutput } from "$lib/components/ai/tool";
	import {
		type WebSearchToolOutput,
		validateWebSearchInput,
		validateWebSearchOutput,
	} from "$lib/components/ai/tool-schemas/websearch-schema";
	import type { ToolRendererComponentProps } from "./types";

	let { toolPart }: ToolRendererComponentProps = $props();

	const isStreaming = $derived.by(
		() =>
			toolPart.state === "input-streaming" || toolPart.state === "input-available",
	);
	const inputValidation = $derived.by(() =>
		validateWebSearchInput(toolPart.input),
	);
	const validInput = $derived.by(() =>
		inputValidation.success ? inputValidation.data : undefined,
	);
	const outputValidation = $derived.by(() =>
		toolPart.output ? validateWebSearchOutput(toolPart.output) : null,
	);
	const validOutput = $derived.by(() =>
		outputValidation?.success
			? (outputValidation.data as WebSearchToolOutput)
			: undefined,
	);
</script>

{#if !toolPart.input || typeof toolPart.input !== "object"}
	<div class="p-4 text-muted-foreground text-sm">{isStreaming ? "Loading web search..." : "No input data"}</div>
{:else if !inputValidation.success || !validInput?.query}
	{#if isStreaming}
		<div class="p-4 text-muted-foreground text-sm">Loading web search...</div>
	{:else}
		<ToolInput input={toolPart.input} />
		<ToolOutput output={toolPart.output} errorText={toolPart.errorText} />
	{/if}
{:else}
	<div class="space-y-4 p-4">
		<div class="flex items-center gap-2">
			<GlobeIcon class="size-4 text-muted-foreground" />
			<h4 class="font-medium text-muted-foreground text-xs uppercase tracking-wide">Web search</h4>
		</div>

		<div class="rounded-md border bg-muted/30 p-3 text-xs">
			<p><span class="text-muted-foreground">query:</span> {validInput.query}</p>
			{#if validInput.allowed_domains?.length}
				<p class="mt-1"><span class="text-muted-foreground">allowed:</span> {validInput.allowed_domains.join(", ")}</p>
			{/if}
			{#if validInput.blocked_domains?.length}
				<p class="mt-1"><span class="text-muted-foreground">blocked:</span> {validInput.blocked_domains.join(", ")}</p>
			{/if}
		</div>

		{#if validOutput?.results?.length}
			<div class="rounded-md border bg-muted/20 p-2">
				{#each validOutput.results as result}
					<a
						href={result.url}
						target="_blank"
						rel="noreferrer"
						class="block border-b px-2 py-2 last:border-b-0 hover:bg-muted/40"
					>
						<div class="font-medium text-sm">{result.title}</div>
						<div class="font-mono text-muted-foreground text-xs">{result.url}</div>
						{#if result.snippet}
							<div class="mt-1 text-muted-foreground text-xs">{result.snippet}</div>
						{/if}
					</a>
				{/each}
			</div>
		{/if}

		<ToolOutput output={toolPart.output} errorText={toolPart.errorText} />
	</div>
{/if}
