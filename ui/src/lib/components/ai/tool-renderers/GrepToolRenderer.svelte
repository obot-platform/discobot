<script lang="ts">
	import SearchIcon from "@lucide/svelte/icons/search";
	import { ToolInput, ToolOutput } from "$lib/components/ai/tool";
	import {
		type GrepToolOutput,
		validateGrepInput,
		validateGrepOutput,
	} from "$lib/components/ai/tool-schemas/grep-schema";
	import { shortenPath } from "./utils";
	import type { ToolRendererComponentProps } from "./types";

	let { toolPart }: ToolRendererComponentProps = $props();

	const isStreaming = $derived.by(
		() =>
			toolPart.state === "input-streaming" || toolPart.state === "input-available",
	);
	const inputValidation = $derived.by(() => validateGrepInput(toolPart.input));
	const validInput = $derived.by(() =>
		inputValidation.success ? inputValidation.data : undefined,
	);
	const outputValidation = $derived.by(() =>
		toolPart.output ? validateGrepOutput(toolPart.output) : null,
	);
	const validOutput = $derived.by(() =>
		outputValidation?.success ? (outputValidation.data as GrepToolOutput) : undefined,
	);
</script>

{#if !toolPart.input || typeof toolPart.input !== "object"}
	<div class="p-4 text-muted-foreground text-sm">{isStreaming ? "Loading search..." : "No input data"}</div>
{:else if !inputValidation.success || !validInput?.pattern}
	{#if isStreaming}
		<div class="p-4 text-muted-foreground text-sm">Loading search...</div>
	{:else}
		<ToolInput input={toolPart.input} />
		<ToolOutput output={toolPart.output} errorText={toolPart.errorText} />
	{/if}
{:else}
	<div class="space-y-4 p-4">
		<div class="flex items-center gap-2">
			<SearchIcon class="size-4 text-muted-foreground" />
			<h4 class="font-medium text-muted-foreground text-xs uppercase tracking-wide">Grep</h4>
		</div>

		<div class="rounded-md border bg-muted/30 p-3 text-xs">
			<p><span class="text-muted-foreground">pattern:</span> <code>{validInput.pattern}</code></p>
			{#if validInput.path}
				<p class="mt-1"><span class="text-muted-foreground">path:</span> <code>{shortenPath(validInput.path)}</code></p>
			{/if}
			{#if validInput.glob}
				<p class="mt-1"><span class="text-muted-foreground">glob:</span> <code>{validInput.glob}</code></p>
			{/if}
		</div>

		{#if validOutput?.matches?.length}
			<div class="rounded-md border bg-muted/20 p-2">
				{#each validOutput.matches as match}
					<div class="border-b px-2 py-1 text-xs last:border-b-0">
						<div class="font-mono text-muted-foreground">{shortenPath(match.file)}:{match.line}</div>
						<div class="font-mono text-foreground">{match.content}</div>
					</div>
				{/each}
			</div>
		{/if}

		<ToolOutput output={toolPart.output} errorText={toolPart.errorText} />
	</div>
{/if}
