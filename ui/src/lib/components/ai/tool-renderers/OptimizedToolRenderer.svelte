<script lang="ts">
	import type { DynamicToolPart } from "$lib/components/ai/types";
	import { ToolInput, ToolOutput } from "$lib/components/ai/tool";
	import { getToolRenderer } from "./registry";

	type Props = {
		toolPart: DynamicToolPart;
		forceRaw?: boolean;
	};

	let { toolPart, forceRaw = false }: Props = $props();

	const renderedInput = $derived.by(() => toolPart.input);
	const renderedOutput = $derived.by(() => toolPart.output);
	const renderedError = $derived.by(() => toolPart.errorText);
	const Renderer = $derived.by(() => getToolRenderer(toolPart.toolName));
</script>

{#if !forceRaw && Renderer}
	<Renderer {toolPart} />
{:else}
	<ToolInput input={renderedInput} />
	<ToolOutput output={renderedOutput} errorText={renderedError} />
{/if}
