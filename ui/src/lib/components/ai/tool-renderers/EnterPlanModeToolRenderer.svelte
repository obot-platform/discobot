<script lang="ts">
	import FilePenLineIcon from "@lucide/svelte/icons/file-pen-line";
	import {
		ToolContent,
		ToolHeaderControls,
		ToolHeaderStatus,
	} from "$lib/components/ai/tool";
	import { CollapsibleTrigger } from "$lib/components/ui/collapsible";
	import { getPlanToolState } from "$lib/session/domains/session-domain.helpers";
	import type { ToolRendererComponentProps } from "./types";
	import { shortenPath } from "./utils";

	let { toolPart, isRaw, onToggleRaw }: ToolRendererComponentProps = $props();

	const planState = $derived.by(() => getPlanToolState(toolPart));
</script>

<div class="flex items-center justify-between gap-4 px-4 pt-4">
	<CollapsibleTrigger
		class="flex min-w-0 flex-1 items-center gap-2 text-left text-muted-foreground"
	>
		<FilePenLineIcon class="size-4 shrink-0 text-muted-foreground" />
		<span class="truncate font-medium text-sm">Enter Plan Mode</span>
		<ToolHeaderStatus state={toolPart.state} />
	</CollapsibleTrigger>
	<ToolHeaderControls {isRaw} {onToggleRaw} />
</div>

<ToolContent>
	<div class="space-y-4 p-4 pt-3">
		<div class="rounded-md border bg-muted/20 p-3">
			<p class="font-medium text-sm">Plan mode is active.</p>
			<p class="mt-1 text-muted-foreground text-sm">
				The agent is exploring and drafting a plan before implementation.
			</p>
		</div>

		{#if planState?.planFilePath}
			<div class="space-y-1.5">
				<h4
					class="font-medium text-muted-foreground text-xs uppercase tracking-wide"
				>
					Plan file
				</h4>
				<code
					class="block overflow-x-auto rounded-md border bg-muted/20 px-3 py-2 font-mono text-xs text-foreground"
				>
					{shortenPath(planState.planFilePath)}
				</code>
			</div>
		{:else}
			<p class="text-muted-foreground text-sm">
				Waiting for the plan file location.
			</p>
		{/if}

		{#if toolPart.errorText}
			<div
				class="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-destructive text-sm"
			>
				{toolPart.errorText}
			</div>
		{/if}
	</div>
</ToolContent>
