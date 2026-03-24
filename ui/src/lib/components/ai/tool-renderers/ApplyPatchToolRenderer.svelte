<script lang="ts">
	import FilePenIcon from "@lucide/svelte/icons/file-pen";
	import {
		ToolContent,
		ToolHeaderControls,
		ToolHeaderStatus,
	} from "$lib/components/ai/tool";
	import { CollapsibleTrigger } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";
	import type { ToolRendererComponentProps } from "./types";
	import {
		extractApplyPatchInput,
		getApplyPatchDisplayPath,
		parseApplyPatchInput,
		parseApplyPatchOutput,
		type ApplyPatchOperation,
	} from "./apply-patch";
	import { renderToolValue, shortenPath } from "./utils";

	let { toolPart, isRaw, onToggleRaw }: ToolRendererComponentProps = $props();

	const isStreaming = $derived.by(
		() =>
			toolPart.state === "input-streaming" ||
			toolPart.state === "input-available",
	);
	const rawPatch = $derived.by(() => extractApplyPatchInput(toolPart.input));
	const parsedPatch = $derived.by(() => parseApplyPatchInput(toolPart.input));
	const parsedOutput = $derived.by(() =>
		parseApplyPatchOutput(toolPart.output),
	);
	const rawOutputText = $derived.by(() => renderToolValue(toolPart.output));
	const headline = $derived.by(() => {
		const firstOperation = parsedPatch.operations[0];
		if (!firstOperation) {
			return isStreaming ? "Loading patch details..." : "Apply patch";
		}
		const path = shortenPath(getApplyPatchDisplayPath(firstOperation));
		return parsedPatch.operations.length > 1
			? `${path} (+${parsedPatch.operations.length - 1})`
			: path;
	});

	function getOperationLabel(operation: ApplyPatchOperation): string {
		switch (operation.kind) {
			case "add":
				return "Add file";
			case "delete":
				return operation.movePath ? "Rename file" : "Delete file";
			default:
				return operation.movePath ? "Move + edit" : "Update file";
		}
	}

	function getOperationBadgeClass(operation: ApplyPatchOperation): string {
		switch (operation.kind) {
			case "add":
				return "border-emerald-200 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300";
			case "delete":
				return "border-red-200 bg-red-500/10 text-red-700 dark:text-red-300";
			default:
				return operation.movePath
					? "border-sky-200 bg-sky-500/10 text-sky-700 dark:text-sky-300"
					: "border-blue-200 bg-blue-500/10 text-blue-700 dark:text-blue-300";
		}
	}

	function getMarkerClass(marker: string): string {
		switch (marker) {
			case "+":
				return "text-emerald-700 dark:text-emerald-300";
			case "-":
				return "text-red-700 dark:text-red-300";
			default:
				return "text-muted-foreground";
		}
	}

	function getRowClass(marker: string): string {
		switch (marker) {
			case "+":
				return "bg-emerald-500/10";
			case "-":
				return "bg-red-500/10";
			default:
				return "bg-background/60";
		}
	}

	function getResultBadgeClass(marker: string): string {
		switch (marker) {
			case "A":
				return "border-emerald-200 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300";
			case "D":
				return "border-red-200 bg-red-500/10 text-red-700 dark:text-red-300";
			default:
				return "border-blue-200 bg-blue-500/10 text-blue-700 dark:text-blue-300";
		}
	}
</script>

<div class="flex items-center justify-between gap-4 px-4 pt-4">
	<CollapsibleTrigger class="flex min-w-0 flex-1 items-center gap-2 text-left">
		<FilePenIcon class="size-4 shrink-0 text-muted-foreground" />
		<span class="truncate font-medium text-sm">{headline}</span>
		<ToolHeaderStatus state={toolPart.state} />
	</CollapsibleTrigger>
	<ToolHeaderControls {isRaw} {onToggleRaw} />
</div>

<ToolContent>
	{#if !rawPatch}
		<div class="p-4 pt-3 text-muted-foreground text-sm">
			{isStreaming
				? "Loading patch details..."
				: "Patch details are unavailable."}
		</div>
	{:else}
		<div class="space-y-4 p-4 pt-3">
			<div class="rounded-md border bg-muted/20 p-3">
				<div class="flex flex-wrap gap-2 text-xs">
					<span class="rounded-full bg-muted px-2 py-0.5 text-muted-foreground">
						{parsedPatch.stats.files}
						{parsedPatch.stats.files === 1 ? "file" : "files"}
					</span>
					<span class="rounded-full bg-muted px-2 py-0.5 text-muted-foreground">
						{parsedPatch.stats.additions} additions
					</span>
					<span class="rounded-full bg-muted px-2 py-0.5 text-muted-foreground">
						{parsedPatch.stats.removals} removals
					</span>
					{#if parsedPatch.incomplete}
						<span
							class="rounded-full bg-yellow-500/10 px-2 py-0.5 text-yellow-700 dark:text-yellow-300"
						>
							Streaming
						</span>
					{/if}
				</div>
				{#if parsedPatch.error && parsedPatch.operations.length > 0}
					<p class="mt-3 text-sm text-yellow-700 dark:text-yellow-300">
						Showing the parsed portion of the patch. {parsedPatch.error}
					</p>
				{/if}
			</div>

			{#if parsedPatch.operations.length === 0}
				<div class="rounded-md border border-dashed bg-muted/20 p-3">
					<p class="text-muted-foreground text-sm">
						{parsedPatch.error ?? "Could not parse patch details."}
					</p>
					<pre
						class="mt-3 overflow-x-auto whitespace-pre-wrap break-words font-mono text-xs"><code
							>{rawPatch}</code
						></pre>
				</div>
			{:else}
				<div class="space-y-3">
					{#each parsedPatch.operations as operation}
						<div class="overflow-hidden rounded-md border bg-muted/20">
							<div
								class="flex flex-wrap items-start justify-between gap-3 border-b bg-background/70 px-3 py-3"
							>
								<div class="min-w-0 space-y-1">
									<div class="flex flex-wrap items-center gap-2">
										<span
											class={cn(
												"rounded-full border px-2 py-0.5 font-medium text-xs",
												getOperationBadgeClass(operation),
											)}
										>
											{getOperationLabel(operation)}
										</span>
										{#if operation.movePath}
											<span
												class="rounded-full bg-muted px-2 py-0.5 text-muted-foreground text-xs"
											>
												Rename
											</span>
										{/if}
									</div>
									<div class="font-mono text-xs text-muted-foreground">
										{#if operation.movePath}
											<span>{shortenPath(operation.path)}</span>
											<span class="px-1">→</span>
											<span>{shortenPath(operation.movePath)}</span>
										{:else}
											{shortenPath(operation.path)}
										{/if}
									</div>
								</div>
								<div class="flex flex-wrap gap-2 text-xs text-muted-foreground">
									{#if operation.kind === "add"}
										<span>{operation.addLines.length} lines</span>
									{:else if operation.kind === "delete"}
										<span>File removal</span>
									{:else}
										<span
											>{operation.chunks.length}
											{operation.chunks.length === 1 ? "hunk" : "hunks"}</span
										>
										<span>+{operation.stats.additions}</span>
										<span>-{operation.stats.removals}</span>
									{/if}
								</div>
							</div>

							<div class="space-y-3 p-3">
								{#if operation.kind === "delete"}
									<div
										class="rounded-md border border-dashed px-3 py-2 text-muted-foreground text-sm"
									>
										This patch deletes the file.
									</div>
								{:else if operation.kind === "add"}
									{#if operation.addLines.length === 0}
										<div
											class="rounded-md border border-dashed px-3 py-2 text-muted-foreground text-sm"
										>
											This patch creates an empty file.
										</div>
									{:else}
										<div
											class="overflow-hidden rounded-md border bg-background/70"
										>
											{#each operation.addLines as line}
												<div
													class="grid grid-cols-[1.5rem_minmax(0,1fr)] border-border/50 border-b last:border-b-0 bg-emerald-500/10 font-mono text-xs"
												>
													<div
														class="px-2 py-1 text-emerald-700 dark:text-emerald-300"
													>
														+
													</div>
													<div
														class="overflow-x-auto px-2 py-1 text-foreground"
													>
														{line || " "}
													</div>
												</div>
											{/each}
										</div>
									{/if}
								{:else}
									{#each operation.chunks as chunk, chunkIndex}
										<div class="space-y-2">
											<div
												class="flex flex-wrap items-center gap-2 text-xs text-muted-foreground"
											>
												<span class="font-medium uppercase tracking-wide"
													>Hunk {chunkIndex + 1}</span
												>
												{#if chunk.context !== null}
													<span
														class="rounded-full bg-muted px-2 py-0.5 font-mono"
														>@@ {chunk.context}</span
													>
												{/if}
												{#if chunk.isEndOfFile}
													<span class="rounded-full bg-muted px-2 py-0.5"
														>EOF</span
													>
												{/if}
											</div>
											<div
												class="overflow-hidden rounded-md border bg-background/70"
											>
												{#each chunk.lines as line}
													<div
														class={cn(
															"grid grid-cols-[1.5rem_minmax(0,1fr)] border-border/50 border-b font-mono text-xs last:border-b-0",
															getRowClass(line.marker),
														)}
													>
														<div
															class={cn(
																"px-2 py-1",
																getMarkerClass(line.marker),
															)}
														>
															{line.marker}
														</div>
														<div
															class="overflow-x-auto px-2 py-1 text-foreground"
														>
															{line.content || " "}
														</div>
													</div>
												{/each}
											</div>
										</div>
									{/each}
								{/if}
							</div>
						</div>
					{/each}
				</div>
			{/if}

			{#if parsedOutput.entries.length > 0}
				<div class="space-y-2 rounded-md border bg-muted/20 p-3">
					<div class="flex items-center justify-between gap-3">
						<h4
							class="font-medium text-muted-foreground text-xs uppercase tracking-wide"
						>
							Updated files
						</h4>
						<span class="text-muted-foreground text-xs"
							>{parsedOutput.entries.length} entries</span
						>
					</div>
					<div class="grid gap-2 md:grid-cols-2">
						{#each parsedOutput.entries as entry}
							<div
								class="flex items-center gap-2 rounded-md border bg-background/70 px-3 py-2 text-sm"
							>
								<span
									class={cn(
										"inline-flex rounded-full border px-2 py-0.5 font-medium text-xs",
										getResultBadgeClass(entry.marker),
									)}
								>
									{entry.marker}
								</span>
								<span class="truncate font-mono text-xs text-muted-foreground"
									>{shortenPath(entry.path)}</span
								>
							</div>
						{/each}
					</div>
				</div>
			{/if}

			{#if toolPart.errorText}
				<div
					class="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-destructive text-sm"
				>
					{toolPart.errorText}
				</div>
			{/if}

			{#if rawOutputText && parsedOutput.entries.length === 0 && !toolPart.errorText}
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
