<script lang="ts">
	import type { DynamicToolPart } from "$lib/components/ai/types";
	import { ToolInput, ToolOutput } from "$lib/components/ai/tool";
	import { getToolRenderer } from "./registry";

	type Props = {
		toolPart: DynamicToolPart;
		forceRaw?: boolean;
		sessionId?: string | null;
		threadId?: string | null;
	};

	let { toolPart, forceRaw = false, sessionId, threadId }: Props = $props();

	const renderedInput = $derived.by(() => toolPart.input);
	const renderedOutput = $derived.by(() => toolPart.output);
	const renderedError = $derived.by(() => toolPart.errorText);
	const Renderer = $derived.by(() => getToolRenderer(toolPart.toolName));
</script>

{#if !forceRaw && Renderer}
	<Renderer {toolPart} {sessionId} {threadId} />
{:else}
	<ToolInput input={renderedInput} />
	<ToolOutput output={renderedOutput} errorText={renderedError} />
{/if}
