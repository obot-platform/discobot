<script lang="ts">
	import CircleCheckIcon from "@lucide/svelte/icons/circle-check";
	import CircleXIcon from "@lucide/svelte/icons/circle-x";
	import TerminalIcon from "@lucide/svelte/icons/terminal";
	import { ToolInput, ToolOutput } from "$lib/components/ai/tool";
	import {
		type BashToolOutput,
		validateBashInput,
		validateBashOutput,
	} from "$lib/components/ai/tool-schemas/bash-schema";
	import { cn } from "$lib/utils";
	import type { ToolRendererComponentProps } from "./types";

	let { toolPart }: ToolRendererComponentProps = $props();

	const isStreaming = $derived.by(
		() =>
			toolPart.state === "input-streaming" || toolPart.state === "input-available",
	);

	const inputValidation = $derived.by(() => validateBashInput(toolPart.input));
	const validInput = $derived.by(() =>
		inputValidation.success ? inputValidation.data : undefined,
	);

	const outputValidation = $derived.by(() =>
		toolPart.output ? validateBashOutput(toolPart.output) : null,
	);
	const validOutput = $derived.by(() =>
		outputValidation?.success ? (outputValidation.data as BashToolOutput) : undefined,
	);
</script>

{#if !toolPart.input || typeof toolPart.input !== "object"}
	<div class="p-4 text-muted-foreground text-sm">
		{isStreaming ? "Loading command details..." : "No input data"}
	</div>
{:else if !inputValidation.success}
	{#if isStreaming}
		<div class="p-4 text-muted-foreground text-sm">Loading command details...</div>
	{:else}
		<ToolInput input={toolPart.input} />
		<ToolOutput output={toolPart.output} errorText={toolPart.errorText} />
	{/if}
{:else}
	<div class="space-y-4 p-4">
		<div class="space-y-2">
			<div class="flex items-center gap-2">
				<TerminalIcon class="size-4 text-muted-foreground" />
				<h4 class="font-medium text-muted-foreground text-xs uppercase tracking-wide">Command</h4>
				{#if validInput?.run_in_background}
					<span class="rounded-full bg-muted px-2 py-0.5 text-muted-foreground text-xs">Background</span>
				{/if}
				{#if validInput?.timeout !== undefined}
					<span class="text-muted-foreground text-xs">{validInput.timeout}ms</span>
				{/if}
			</div>

			{#if validInput?.description}
				<p class="italic text-muted-foreground text-sm">{validInput.description}</p>
			{/if}

			<div class="overflow-hidden rounded-md border bg-muted/50 font-mono text-sm">
				<div class="border-border border-b bg-muted/30 px-3 py-2 text-muted-foreground text-xs">$</div>
				<div class="px-3 py-2"><code class="text-foreground">{validInput?.command}</code></div>
			</div>
		</div>

		{#if validOutput || toolPart.errorText}
			<div class="space-y-2">
				<div class="flex items-center gap-2">
					{#if toolPart.errorText}
						<CircleXIcon class="size-4 text-destructive" />
					{:else if validOutput?.exitCode === 0 || validOutput?.exitCode === undefined}
						<CircleCheckIcon class="size-4 text-green-600" />
					{:else}
						<CircleXIcon class="size-4 text-yellow-600" />
					{/if}
					<h4 class="font-medium text-muted-foreground text-xs uppercase tracking-wide">
						{toolPart.errorText ? "Error" : "Output"}
					</h4>
					{#if validOutput?.exitCode !== undefined}
						<span
							class={cn(
								"rounded px-2 py-0.5 font-mono text-xs",
								validOutput.exitCode === 0
									? "bg-green-100 text-green-700"
									: "bg-yellow-100 text-yellow-700",
							)}
						>
							exit {validOutput.exitCode}
						</span>
					{/if}
				</div>

				<ToolOutput
					output={validOutput?.output || validOutput?.stdout || toolPart.output}
					errorText={toolPart.errorText}
				/>

				{#if validOutput?.stderr}
					<div class="rounded-md border border-yellow-200 bg-yellow-50 p-3">
						<h5 class="mb-2 font-medium text-yellow-800 text-xs">STDERR</h5>
						<pre class="whitespace-pre-wrap font-mono text-xs text-yellow-700"><code>{validOutput.stderr}</code></pre>
					</div>
				{/if}
			</div>
		{/if}
	</div>
{/if}
