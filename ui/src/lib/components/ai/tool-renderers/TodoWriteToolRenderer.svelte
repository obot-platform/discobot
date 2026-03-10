<script lang="ts">
	import ListTodoIcon from "@lucide/svelte/icons/list-todo";
	import { ToolInput, ToolOutput } from "$lib/components/ai/tool";
	import {
		type TodoWriteToolInput,
		validateTodoWriteInput,
		validateTodoWriteOutput,
	} from "$lib/components/ai/tool-schemas/todowrite-schema";
	import type { ToolRendererComponentProps } from "./types";

	let { toolPart }: ToolRendererComponentProps = $props();

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
		outputValidation?.success ? outputValidation.data : undefined,
	);
</script>

{#if !toolPart.input || typeof toolPart.input !== "object"}
	<div class="p-4 text-muted-foreground text-sm">{isStreaming ? "Loading todos..." : "No input data"}</div>
{:else if !inputValidation.success || !validInput?.todos}
	{#if isStreaming}
		<div class="p-4 text-muted-foreground text-sm">Loading todos...</div>
	{:else}
		<ToolInput input={toolPart.input} />
		<ToolOutput output={toolPart.output} errorText={toolPart.errorText} />
	{/if}
{:else}
	<div class="space-y-4 p-4">
		<div class="flex items-center gap-2">
			<ListTodoIcon class="size-4 text-muted-foreground" />
			<h4 class="font-medium text-muted-foreground text-xs uppercase tracking-wide">Todo write</h4>
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

		<ToolOutput output={validOutput || toolPart.output} errorText={toolPart.errorText} />
	</div>
{/if}
