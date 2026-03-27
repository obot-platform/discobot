<script lang="ts">
	import BotIcon from "@lucide/svelte/icons/bot";
	import FileOutputIcon from "@lucide/svelte/icons/file-output";
	import PlayIcon from "@lucide/svelte/icons/play";
	import { MessageResponse } from "$lib/components/ai/message";
	import {
		ToolContent,
		ToolHeaderControls,
		ToolHeaderStatus,
	} from "$lib/components/ai/tool";
	import {
		type TaskToolOutput,
		validateTaskInput,
		validateTaskOutput,
	} from "$lib/components/ai/tool-schemas/task-schema";
	import { CollapsibleTrigger } from "$lib/components/ui/collapsible";
	import type { ToolRendererComponentProps } from "./types";
	import { getToolInputString, renderToolValue, shortenPath } from "./utils";

	let { toolPart, isRaw, onToggleRaw }: ToolRendererComponentProps = $props();

	const isStreaming = $derived.by(
		() =>
			toolPart.state === "input-streaming" ||
			toolPart.state === "input-available",
	);
	const headerDescription = $derived.by(() =>
		getToolInputString(toolPart.input, "description"),
	);
	const headerSubagentType = $derived.by(() =>
		getToolInputString(toolPart.input, "subagent_type"),
	);
	const inputValidation = $derived.by(() => validateTaskInput(toolPart.input));
	const validInput = $derived.by(() =>
		inputValidation.success ? inputValidation.data : undefined,
	);
	const outputValidation = $derived.by(() =>
		toolPart.output ? validateTaskOutput(toolPart.output) : null,
	);
	const validOutput = $derived.by(() =>
		outputValidation?.success
			? (outputValidation.data as TaskToolOutput)
			: undefined,
	);
	const taskError = $derived.by(() => toolPart.errorText || validOutput?.error);
	const rawOutputText = $derived.by(() => renderToolValue(toolPart.output));
</script>

<div class="flex items-center justify-between gap-4 px-4 pt-4">
	<CollapsibleTrigger
		class="flex min-w-0 flex-1 items-center gap-2 text-left text-muted-foreground"
	>
		<BotIcon class="size-4 shrink-0 text-muted-foreground" />
		<span class="truncate font-medium text-sm">
			{headerDescription ||
				headerSubagentType ||
				(isStreaming ? "Loading task details..." : "Sub-agent task")}
		</span>
		<ToolHeaderStatus state={toolPart.state} />
	</CollapsibleTrigger>
	<ToolHeaderControls {isRaw} {onToggleRaw} />
</div>

<ToolContent>
	{#if !toolPart.input || typeof toolPart.input !== "object"}
		<div class="p-4 pt-3 text-muted-foreground text-sm">
			{isStreaming
				? "Loading task details..."
				: "Task details are unavailable."}
		</div>
	{:else if !inputValidation.success}
		<div class="space-y-3 p-4 pt-3">
			<p class="text-muted-foreground text-sm">
				{isStreaming
					? "Loading task details..."
					: "Could not parse task details."}
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
				<div class="flex flex-wrap items-center gap-2">
					<code
						class="rounded bg-muted px-2 py-1 font-mono text-foreground text-sm"
						>{validInput?.subagent_type}</code
					>
					{#if validInput?.run_in_background}
						<span
							class="rounded-full bg-muted px-2 py-0.5 text-muted-foreground text-xs"
							>Background</span
						>
					{/if}
					{#if validInput?.model}
						<span
							class="rounded-full bg-muted px-2 py-0.5 text-muted-foreground text-xs"
							>{validInput.model}</span
						>
					{/if}
					{#if validInput?.max_turns}
						<span class="text-muted-foreground text-xs"
							>max {validInput.max_turns} turns</span
						>
					{/if}
				</div>
				{#if validInput?.description}
					<p class="text-foreground text-sm">{validInput.description}</p>
				{/if}
			</div>

			<div class="space-y-2">
				<div class="flex items-center gap-2">
					<PlayIcon class="size-4 text-muted-foreground" />
					<h4
						class="font-medium text-muted-foreground text-xs uppercase tracking-wide"
					>
						Task
					</h4>
				</div>
				<div class="rounded-md border bg-muted/30 p-3 text-sm">
					{validInput?.prompt}
				</div>
			</div>

			{#if validOutput?.agentId || validOutput?.output_file}
				<div class="space-y-2">
					<h4
						class="font-medium text-muted-foreground text-xs uppercase tracking-wide"
					>
						Runtime
					</h4>
					<div class="space-y-2 rounded-md border bg-muted/20 p-3 text-xs">
						{#if validOutput?.agentId}
							<div class="flex items-center gap-2">
								<span class="text-muted-foreground">Agent ID:</span>
								<code class="font-mono text-foreground"
									>{validOutput.agentId}</code
								>
							</div>
						{/if}
						{#if validOutput?.output_file}
							<div class="flex items-center gap-2">
								<FileOutputIcon class="size-4 text-muted-foreground" />
								<code class="font-mono text-foreground"
									>{shortenPath(validOutput.output_file)}</code
								>
							</div>
						{/if}
					</div>
				</div>
			{/if}

			{#if validOutput?.result}
				<div class="space-y-2">
					<h4
						class="font-medium text-muted-foreground text-xs uppercase tracking-wide"
					>
						Result
					</h4>
					<div class="rounded-md border bg-muted/20 p-3 text-sm">
						<MessageResponse text={validOutput.result} />
					</div>
				</div>
			{:else if outputValidation?.success && !validOutput?.output_file && !taskError}
				<div
					class="rounded-md border border-dashed px-3 py-2 text-muted-foreground text-sm"
				>
					Task launched without an inline result.
				</div>
			{/if}

			{#if taskError}
				<div
					class="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-destructive text-sm"
				>
					{taskError}
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
