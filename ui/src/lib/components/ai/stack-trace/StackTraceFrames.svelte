<script lang="ts">
	import { cn } from "$lib/utils";
	import { AT_PREFIX_REGEX } from "./parse";
	import { useStackTraceContext } from "./context";

	type Props = {
		showInternalFrames?: boolean;
		class?: string;
	};

	let { showInternalFrames = true, class: className, ...restProps }: Props = $props();
	const stackTrace = useStackTraceContext();

	const framesToShow = $derived.by(() =>
		showInternalFrames
			? stackTrace.trace.frames
			: stackTrace.trace.frames.filter((frame) => !frame.isInternal),
	);
</script>

<div class={cn("space-y-1 p-3", className)} {...restProps}>
	{#if framesToShow.length === 0}
		<div class="text-xs text-muted-foreground">No stack frames</div>
	{:else}
		{#each framesToShow as frame, index (`${frame.raw}-${index}`)}
			<div class={cn("text-xs", frame.isInternal ? "text-muted-foreground/50" : "text-foreground/90")}>
				<span class="text-muted-foreground">at </span>
				{#if frame.functionName}
					<span class={frame.isInternal ? "" : "text-foreground"}>{frame.functionName} </span>
				{/if}
				{#if frame.filePath}
					<span class="text-muted-foreground">(</span>
					<button
						type="button"
						disabled={!stackTrace.onFilePathClick}
						onclick={() => {
							if (frame.filePath) {
								stackTrace.onFilePathClick?.(
									frame.filePath,
									frame.lineNumber ?? undefined,
									frame.columnNumber ?? undefined,
								);
							}
						}}
						class={cn(
							"underline decoration-dotted hover:text-primary",
							stackTrace.onFilePathClick ? "cursor-pointer" : "",
						)}
					>
						{frame.filePath}
						{#if frame.lineNumber !== null}
							:{frame.lineNumber}
						{/if}
						{#if frame.columnNumber !== null}
							:{frame.columnNumber}
						{/if}
					</button>
					<span class="text-muted-foreground">)</span>
				{:else if !frame.functionName}
					<span>{frame.raw.replace(AT_PREFIX_REGEX, "")}</span>
				{/if}
			</div>
		{/each}
	{/if}
</div>
