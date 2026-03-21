<script lang="ts">
	import ListTodoIcon from "@lucide/svelte/icons/list-todo";
	import { ToolContent, ToolHeaderControls, ToolHeaderStatus } from "$lib/components/ai/tool";
	import {
		type TodoWriteToolInput,
		type TodoWriteToolOutput,
		validateTodoWriteInput,
		validateTodoWriteOutput,
	} from "$lib/components/ai/tool-schemas/todowrite-schema";
	import { CollapsibleTrigger } from "$lib/components/ui/collapsible";
	import type { ToolRendererComponentProps } from "./types";
	import { renderToolValue } from "./utils";

	let { toolPart, isRaw, onToggleRaw }: ToolRendererComponentProps = $props();

	const isStreaming = $derived.by(
		() =>
			toolPart.state === "input-streaming" || toolPart.state === "input-available",
	);
	const inputValidation = $derived.by(() =>
		validateTodoWriteInput(toolPart.input),
	);
	const validInput = $derived.by(() =>
		inputValidation.success
			? (inputValidation.data as TodoWriteToolInput)
			: undefined,
	);
	const outputValidation = $derived.by(() =>
		toolPart.output ? validateTodoWriteOutput(toolPart.output) : null,
	);
	const validOutput = $derived.by(() =>
		outputValidation?.success
			? (outputValidation.data as TodoWriteToolOutput)
			: undefined,
	);
	const todoCounts = $derived.by(() => {
		const counts = { completed: 0, in_progress: 0, pending: 0 };
		for (const todo of validInput?.todos ?? []) {
			if (todo.status === "completed") {
				counts.completed += 1;
			} else if (todo.status === "in_progress") {
				counts.in_progress += 1;
			} else {
				counts.pending += 1;
			}
		}
		return counts;
	});
	const todoError = $derived.by(() => toolPart.errorText || validOutput?.error);
	const rawOutputText = $derived.by(() => renderToolValue(toolPart.output));
</script>

<div class="flex items-center justify-between gap-4 px-4 pt-4">
	<CollapsibleTrigger class="flex min-w-0 flex-1 items-center gap-2 text-left">
		<ListTodoIcon class="size-4 shrink-0 text-muted-foreground" />
		<span class="truncate font-medium text-sm">
			{validInput?.todos ? `${validInput.todos.length} todos` : isStreaming ? "Loading todos..." : "Todo write"}
		</span>
		<ToolHeaderStatus state={toolPart.state} />
	</CollapsibleTrigger>
	<ToolHeaderControls {isRaw} {onToggleRaw} />
</div>

<ToolContent>
	{#if !toolPart.input || typeof toolPart.input !== "object"}
		<div class="p-4 pt-3 text-muted-foreground text-sm">{isStreaming ? "Loading todos..." : "Todo details are unavailable."}</div>
	{:else if !inputValidation.success || !validInput?.todos}
		<div class="space-y-3 p-4 pt-3">
			<p class="text-muted-foreground text-sm">{isStreaming ? "Loading todos..." : "Could not parse todo details."}</p>
			{#if rawOutputText}
				<div class="rounded-md border border-dashed bg-muted/20 p-3">
					<pre class="overflow-x-auto whitespace-pre-wrap break-words font-mono text-xs"><code>{rawOutputText}</code></pre>
				</div>
			{/if}
		</div>
	{:else}
		<div class="space-y-4 p-4 pt-3">
			<div class="flex flex-wrap gap-2 text-xs">
				<span class="rounded-full bg-muted px-2 py-0.5 text-muted-foreground">
					{todoCounts.completed} completed
				</span>
				<span class="rounded-full bg-muted px-2 py-0.5 text-muted-foreground">
					{todoCounts.in_progress} in progress
				</span>
				<span class="rounded-full bg-muted px-2 py-0.5 text-muted-foreground">
					{todoCounts.pending} pending
				</span>
			</div>

			<ul class="space-y-1 rounded-md border bg-muted/20 p-3">
				{#each validInput.todos as todo}
					<li class="flex items-start gap-2 text-xs">
						<span class="mt-0.5">{todo.status === "completed" ? "✓" : todo.status === "in_progress" ? "•" : "○"}</span>
						<div>
							<div class={todo.status === "completed" ? "line-through text-muted-foreground" : ""}>
								{todo.content || "Untitled task"}
							</div>
							{#if todo.activeForm}
								<div class="text-muted-foreground">{todo.activeForm}</div>
							{/if}
						</div>
					</li>
				{/each}
			</ul>

			{#if validOutput?.success}
				<p class="text-green-700 text-xs">Todo list updated.</p>
			{/if}

			{#if todoError}
				<div class="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-destructive text-sm">
					{todoError}
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
	{/if}
</ToolContent>
