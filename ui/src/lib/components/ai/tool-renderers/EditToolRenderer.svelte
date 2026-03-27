<script lang="ts">
	import FilePenIcon from "@lucide/svelte/icons/file-pen";
	import { buildEditDiffRows, type EditDiffRow } from "$lib/diff-utils";
	import {
		ToolContent,
		ToolHeaderControls,
		ToolHeaderStatus,
	} from "$lib/components/ai/tool";
	import {
		type EditToolOutput,
		validateEditInput,
		validateEditOutput,
	} from "$lib/components/ai/tool-schemas/edit-schema";
	import { CollapsibleTrigger } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";
	import type { ToolRendererComponentProps } from "./types";
	import {
		countLines,
		getToolInputString,
		renderToolValue,
		shortenPath,
	} from "./utils";

	let { toolPart, isRaw, onToggleRaw }: ToolRendererComponentProps = $props();

	const isStreaming = $derived.by(
		() =>
			toolPart.state === "input-streaming" ||
			toolPart.state === "input-available",
	);
	const headerFilePath = $derived.by(() =>
		getToolInputString(toolPart.input, "file_path"),
	);
	const inputValidation = $derived.by(() => validateEditInput(toolPart.input));
	const validInput = $derived.by(() =>
		inputValidation.success ? inputValidation.data : undefined,
	);
	const outputValidation = $derived.by(() =>
		toolPart.output ? validateEditOutput(toolPart.output) : null,
	);
	const validOutput = $derived.by(() =>
		outputValidation?.success
			? (outputValidation.data as EditToolOutput)
			: undefined,
	);
	const editError = $derived.by(() => toolPart.errorText || validOutput?.error);
	const rawOutputText = $derived.by(() => renderToolValue(toolPart.output));
	const diffRows = $derived.by(() =>
		buildEditDiffRows(
			validInput?.old_string ?? "",
			validInput?.new_string ?? "",
		),
	);
	const changedRowCount = $derived.by(
		() => diffRows.filter((row) => row.kind !== "context").length,
	);

	function getRowClasses(kind: EditDiffRow["kind"]): string {
		if (kind === "add") {
			return "bg-green-500/5";
		}
		if (kind === "remove") {
			return "bg-red-500/5";
		}
		return "bg-background/70";
	}

	function getMarker(kind: EditDiffRow["kind"]): string {
		if (kind === "add") {
			return "+";
		}
		if (kind === "remove") {
			return "-";
		}
		return " ";
	}

	function getMarkerClasses(kind: EditDiffRow["kind"]): string {
		if (kind === "add") {
			return "text-green-700 dark:text-green-400";
		}
		if (kind === "remove") {
			return "text-red-700 dark:text-red-400";
		}
		return "text-muted-foreground";
	}

	function getSegmentClasses(
		kind: EditDiffRow["kind"],
		changed: boolean,
	): string {
		if (!changed) {
			return "";
		}
		if (kind === "add") {
			return "rounded-sm bg-green-500/15 text-green-950 dark:text-green-50";
		}
		if (kind === "remove") {
			return "rounded-sm bg-red-500/15 text-red-950 dark:text-red-50";
		}
		return "";
	}
</script>

<div class="flex items-center justify-between gap-4 px-4 pt-4">
	<CollapsibleTrigger
		class="flex min-w-0 flex-1 items-center gap-2 text-left text-muted-foreground"
	>
		<FilePenIcon class="size-4 shrink-0 text-muted-foreground" />
		<span class="truncate font-medium text-sm">
			{headerFilePath
				? shortenPath(headerFilePath)
				: isStreaming
					? "Loading edit details..."
					: "Edit file"}
		</span>
		<ToolHeaderStatus state={toolPart.state} />
	</CollapsibleTrigger>
	<ToolHeaderControls {isRaw} {onToggleRaw} />
</div>

<ToolContent>
	{#if !toolPart.input || typeof toolPart.input !== "object"}
		<div class="p-4 pt-3 text-muted-foreground text-sm">
			{isStreaming
				? "Loading edit details..."
				: "Edit details are unavailable."}
		</div>
	{:else if !inputValidation.success || !validInput?.file_path}
		<div class="space-y-3 p-4 pt-3">
			<p class="text-muted-foreground text-sm">
				{isStreaming
					? "Loading edit details..."
					: "Could not parse edit details."}
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
			<div class="space-y-3 rounded-md border bg-muted/20 p-3 text-sm">
				<div>
					<p class="font-mono text-muted-foreground text-xs">
						{shortenPath(validInput.file_path)}
					</p>
					<div class="mt-2 flex flex-wrap gap-2 text-xs">
						<span
							class="rounded-full bg-muted px-2 py-0.5 text-muted-foreground"
						>
							{validInput.replace_all ? "Replace all" : "Single replace"}
						</span>
						<span
							class="rounded-full bg-muted px-2 py-0.5 text-muted-foreground"
						>
							old {countLines(validInput.old_string ?? "")} lines
						</span>
						<span
							class="rounded-full bg-muted px-2 py-0.5 text-muted-foreground"
						>
							new {countLines(validInput.new_string ?? "")} lines
						</span>
						{#if changedRowCount > 0}
							<span
								class="rounded-full bg-muted px-2 py-0.5 text-muted-foreground"
							>
								{changedRowCount} changed {changedRowCount === 1
									? "row"
									: "rows"}
							</span>
						{/if}
					</div>
				</div>

				<div class="space-y-1.5">
					<h5
						class="font-medium text-muted-foreground text-xs uppercase tracking-wide"
					>
						Changes
					</h5>
					<div class="overflow-auto rounded-md border bg-background/80">
						{#if changedRowCount === 0}
							<div class="p-3 text-muted-foreground text-xs">
								No text changes to preview.
							</div>
						{:else}
							<div class="min-w-max font-mono text-xs">
								<div
									class="grid grid-cols-[3rem_3rem_1.5rem_minmax(0,1fr)] border-b bg-muted/40 text-[11px] text-muted-foreground uppercase tracking-wide"
								>
									<div class="border-r px-2 py-1 text-right">Old</div>
									<div class="border-r px-2 py-1 text-right">New</div>
									<div class="border-r px-1 py-1 text-center">±</div>
									<div class="px-3 py-1">Content</div>
								</div>
								{#each diffRows as row, rowIndex (`${row.kind}-${row.oldLineNumber ?? "x"}-${row.newLineNumber ?? "x"}-${rowIndex}`)}
									<div
										class={cn(
											"grid grid-cols-[3rem_3rem_1.5rem_minmax(0,1fr)] border-t first:border-t-0",
											getRowClasses(row.kind),
										)}
									>
										<div
											class="border-r px-2 py-1 text-right text-muted-foreground"
										>
											{row.oldLineNumber ?? ""}
										</div>
										<div
											class="border-r px-2 py-1 text-right text-muted-foreground"
										>
											{row.newLineNumber ?? ""}
										</div>
										<div
											class={cn(
												"border-r px-1 py-1 text-center font-semibold",
												getMarkerClasses(row.kind),
											)}
										>
											{getMarker(row.kind)}
										</div>
										<div class="min-h-6 px-3 py-1 whitespace-pre">
											{#if row.segments.every((segment) => segment.text.length === 0)}
												<span aria-hidden="true" class="opacity-0">·</span>
											{:else}
												{#each row.segments as segment, segmentIndex (`${rowIndex}-${segmentIndex}`)}
													<span
														class={cn(
															getSegmentClasses(row.kind, segment.changed),
															segment.changed && "px-0.5",
														)}
													>
														{segment.text}
													</span>
												{/each}
											{/if}
										</div>
									</div>
								{/each}
							</div>
						{/if}
					</div>
				</div>
			</div>

			<div class="flex flex-wrap items-center gap-3 text-xs">
				{#if validOutput?.success !== undefined}
					<span
						class={validOutput.success ? "text-green-700" : "text-yellow-700"}
					>
						{validOutput.success ? "Edit applied" : "Edit returned warnings"}
					</span>
				{/if}
				{#if validOutput?.replacements !== undefined}
					<span class="text-muted-foreground"
						>{validOutput.replacements} replacements applied</span
					>
				{/if}
			</div>

			{#if editError}
				<div
					class="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-destructive text-sm"
				>
					{editError}
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
