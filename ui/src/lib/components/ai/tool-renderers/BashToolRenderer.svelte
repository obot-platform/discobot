<script lang="ts">
	import CircleCheckIcon from "@lucide/svelte/icons/circle-check";
	import CircleXIcon from "@lucide/svelte/icons/circle-x";
	import TerminalIcon from "@lucide/svelte/icons/terminal";
	import { ToolContent, ToolHeaderControls, ToolHeaderStatus } from "$lib/components/ai/tool";
	import {
		type BashToolOutput,
		validateBashInput,
		validateBashOutput,
	} from "$lib/components/ai/tool-schemas/bash-schema";
	import { CollapsibleTrigger } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";
	import type { ToolRendererComponentProps } from "./types";
	import { countLines, renderToolValue } from "./utils";

	let { toolPart, isRaw, onToggleRaw }: ToolRendererComponentProps = $props();

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
	const stdout = $derived.by(() => validOutput?.output || validOutput?.stdout || "");
	const rawOutputText = $derived.by(() => renderToolValue(toolPart.output));
	const executionError = $derived.by(() => toolPart.errorText || validOutput?.stderr);
</script>

<div class="flex items-center justify-between gap-4 px-4 pt-4">
	<CollapsibleTrigger class="flex min-w-0 flex-1 items-center gap-2 text-left">
		<TerminalIcon class="size-4 shrink-0 text-muted-foreground" />
		<span class="truncate font-medium text-sm">
			{validInput?.description || validInput?.command || (isStreaming ? "Loading command details..." : "Command")}
		</span>
		<ToolHeaderStatus state={toolPart.state} />
	</CollapsibleTrigger>
	<ToolHeaderControls {isRaw} {onToggleRaw} />
</div>

<ToolContent>
	{#if !toolPart.input || typeof toolPart.input !== "object"}
		<div class="p-4 pt-3 text-muted-foreground text-sm">
			{isStreaming ? "Loading command details..." : "Command details are unavailable."}
		</div>
	{:else if !inputValidation.success}
		<div class="space-y-3 p-4 pt-3">
			<p class="text-muted-foreground text-sm">
				{isStreaming ? "Loading command details..." : "Could not parse command details."}
			</p>
			{#if rawOutputText}
				<div class="rounded-md border border-dashed bg-muted/20 p-3">
					<pre class="overflow-x-auto whitespace-pre-wrap break-words font-mono text-xs"><code>{rawOutputText}</code></pre>
				</div>
			{/if}
		</div>
	{:else}
		<div class="space-y-4 p-4 pt-3">
			<div class="space-y-2">
				<div class="flex flex-wrap items-center gap-2">
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
					<div class="px-3 py-2"><code class="break-all text-foreground">{validInput?.command}</code></div>
				</div>
			</div>

			<div class="space-y-2">
				<div class="flex items-center gap-2">
					{#if executionError}
						<CircleXIcon class="size-4 text-destructive" />
					{:else if validOutput?.exitCode === 0 || validOutput?.exitCode === undefined}
						<CircleCheckIcon class="size-4 text-green-600" />
					{:else}
						<CircleXIcon class="size-4 text-yellow-600" />
					{/if}
					<h4 class="font-medium text-muted-foreground text-xs uppercase tracking-wide">
						{executionError ? "Error" : "Output"}
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

				{#if stdout}
					<div class="rounded-md border bg-muted/30">
						<div class="flex items-center justify-between border-b px-3 py-2 text-muted-foreground text-xs uppercase tracking-wide">
							<span>Stdout</span>
							<span>{countLines(stdout)} lines</span>
						</div>
						<pre class="overflow-x-auto whitespace-pre-wrap break-words p-3 font-mono text-xs text-foreground"><code>{stdout}</code></pre>
					</div>
				{:else if !executionError && outputValidation?.success}
					<div class="rounded-md border border-dashed px-3 py-2 text-muted-foreground text-sm">
						Command completed without output.
					</div>
				{/if}

				{#if validOutput?.stderr && !toolPart.errorText}
					<div class="rounded-md border border-yellow-200 bg-yellow-50 p-3">
						<h5 class="mb-2 font-medium text-yellow-800 text-xs uppercase tracking-wide">Stderr</h5>
						<pre class="whitespace-pre-wrap break-words font-mono text-xs text-yellow-700"><code>{validOutput.stderr}</code></pre>
					</div>
				{/if}

				{#if toolPart.errorText}
					<div class="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-destructive text-sm">
						{toolPart.errorText}
					</div>
				{/if}

				{#if outputValidation && !outputValidation.success && rawOutputText}
					<div class="rounded-md border border-dashed bg-muted/20 p-3">
						<h5 class="mb-2 font-medium text-muted-foreground text-xs uppercase tracking-wide">
							Unparsed output
						</h5>
						<pre class="overflow-x-auto whitespace-pre-wrap break-words font-mono text-xs"><code>{rawOutputText}</code></pre>
					</div>
				{/if}
			</div>
		</div>
	{/if}
</ToolContent>
