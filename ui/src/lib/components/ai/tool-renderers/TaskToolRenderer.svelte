<script lang="ts">
	import BotIcon from "@lucide/svelte/icons/bot";
	import FileOutputIcon from "@lucide/svelte/icons/file-output";
	import PlayIcon from "@lucide/svelte/icons/play";
	import { ToolInput, ToolOutput } from "$lib/components/ai/tool";
	import {
		type TaskToolOutput,
		validateTaskInput,
		validateTaskOutput,
	} from "$lib/components/ai/tool-schemas/task-schema";
	import { shortenPath } from "./utils";
	import type { ToolRendererComponentProps } from "./types";

	let { toolPart }: ToolRendererComponentProps = $props();

	const isStreaming = $derived.by(
		() =>
			toolPart.state === "input-streaming" || toolPart.state === "input-available",
	);
	const inputValidation = $derived.by(() => validateTaskInput(toolPart.input));
	const validInput = $derived.by(() =>
		inputValidation.success ? inputValidation.data : undefined,
	);
	const outputValidation = $derived.by(() =>
		toolPart.output ? validateTaskOutput(toolPart.output) : null,
	);
	const validOutput = $derived.by(() =>
		outputValidation?.success ? (outputValidation.data as TaskToolOutput) : undefined,
	);
</script>

{#if !toolPart.input || typeof toolPart.input !== "object"}
	<div class="p-4 text-muted-foreground text-sm">{isStreaming ? "Loading task details..." : "No input data"}</div>
{:else if !inputValidation.success}
	{#if isStreaming}
		<div class="p-4 text-muted-foreground text-sm">Loading task details...</div>
	{:else}
		<ToolInput input={toolPart.input} />
		<ToolOutput output={toolPart.output} errorText={toolPart.errorText} />
	{/if}
{:else}
	<div class="space-y-4 p-4">
		<div class="space-y-2">
			<div class="flex items-center gap-2">
				<BotIcon class="size-4 text-muted-foreground" />
				<h4 class="font-medium text-muted-foreground text-xs uppercase tracking-wide">Sub-agent</h4>
			</div>

			<div class="flex flex-wrap items-center gap-2">
				<code class="rounded bg-muted px-2 py-1 font-mono text-foreground text-sm">{validInput?.subagent_type}</code>
				{#if validInput?.run_in_background}
					<span class="rounded-full bg-muted px-2 py-0.5 text-muted-foreground text-xs">Background</span>
				{/if}
				{#if validInput?.model}
					<span class="rounded-full bg-muted px-2 py-0.5 text-muted-foreground text-xs">{validInput.model}</span>
				{/if}
				{#if validInput?.max_turns}
					<span class="text-muted-foreground text-xs">max {validInput.max_turns} turns</span>
				{/if}
			</div>

			<p class="text-foreground text-sm">{validInput?.description}</p>
		</div>

		<div class="space-y-2">
			<div class="flex items-center gap-2">
				<PlayIcon class="size-4 text-muted-foreground" />
				<h4 class="font-medium text-muted-foreground text-xs uppercase tracking-wide">Task</h4>
			</div>
			<div class="rounded-md border bg-muted/30 p-3 text-sm">{validInput?.prompt}</div>
		</div>

		{#if validOutput}
			<div class="space-y-2">
				<h4 class="font-medium text-muted-foreground text-xs uppercase tracking-wide">Result</h4>
				{#if validOutput.agentId}
					<div class="flex items-center gap-2 text-xs">
						<span class="text-muted-foreground">Agent ID:</span>
						<code class="font-mono text-foreground">{validOutput.agentId}</code>
					</div>
				{/if}

				{#if validOutput.output_file}
					<div class="flex items-center gap-2 rounded-md bg-muted/50 p-2">
						<FileOutputIcon class="size-4 text-muted-foreground" />
						<code class="font-mono text-foreground text-xs">{shortenPath(validOutput.output_file)}</code>
					</div>
				{/if}
			</div>
		{/if}

		<ToolOutput output={toolPart.output} errorText={toolPart.errorText} />
	</div>
{/if}
