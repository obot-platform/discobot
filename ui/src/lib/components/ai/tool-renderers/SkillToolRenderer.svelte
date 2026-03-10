<script lang="ts">
	import SparklesIcon from "@lucide/svelte/icons/sparkles";
	import { ToolInput, ToolOutput } from "$lib/components/ai/tool";
	import {
		type SkillToolOutput,
		validateSkillInput,
		validateSkillOutput,
	} from "$lib/components/ai/tool-schemas/skill-schema";
	import type { ToolRendererComponentProps } from "./types";

	let { toolPart }: ToolRendererComponentProps = $props();

	const isStreaming = $derived.by(
		() =>
			toolPart.state === "input-streaming" || toolPart.state === "input-available",
	);
	const inputValidation = $derived.by(() => validateSkillInput(toolPart.input));
	const validInput = $derived.by(() =>
		inputValidation.success ? inputValidation.data : undefined,
	);
	const outputValidation = $derived.by(() =>
		toolPart.output ? validateSkillOutput(toolPart.output) : null,
	);
	const validOutput = $derived.by(() =>
		outputValidation?.success
			? (outputValidation.data as SkillToolOutput)
			: undefined,
	);
</script>

{#if !toolPart.input || typeof toolPart.input !== "object"}
	<div class="p-4 text-muted-foreground text-sm">{isStreaming ? "Loading skill..." : "No input data"}</div>
{:else if !inputValidation.success || !validInput?.skill}
	{#if isStreaming}
		<div class="p-4 text-muted-foreground text-sm">Loading skill...</div>
	{:else}
		<ToolInput input={toolPart.input} />
		<ToolOutput output={toolPart.output} errorText={toolPart.errorText} />
	{/if}
{:else}
	<div class="space-y-4 p-4">
		<div class="flex items-center gap-2">
			<SparklesIcon class="size-4 text-muted-foreground" />
			<h4 class="font-medium text-muted-foreground text-xs uppercase tracking-wide">Skill</h4>
		</div>

		<div class="rounded-md border bg-muted/30 p-3 text-xs">
			<p><span class="text-muted-foreground">name:</span> <code>{validInput.skill}</code></p>
			{#if validInput.args}
				<p class="mt-2"><span class="text-muted-foreground">args:</span> <code>{validInput.args}</code></p>
			{/if}
		</div>

		{#if validOutput?.result}
			<div class="rounded-md border bg-muted/20 p-3">
				<pre class="max-h-56 overflow-auto whitespace-pre-wrap text-xs"><code>{validOutput.result}</code></pre>
			</div>
		{/if}

		<ToolOutput output={toolPart.output} errorText={toolPart.errorText} />
	</div>
{/if}
