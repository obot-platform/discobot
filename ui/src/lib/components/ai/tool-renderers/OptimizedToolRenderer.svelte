<script lang="ts">
	import { Tool, ToolContent, ToolHeader, ToolInput, ToolOutput } from "$lib/components/ai/tool";
	import type { DynamicToolPart } from "$lib/components/ai/types";
	import { getToolRenderer, getToolTitle } from "./registry";

	type Props = {
		toolPart: DynamicToolPart;
		forceRaw?: boolean;
		defaultOpen?: boolean;
		sessionId?: string | null;
		threadId?: string | null;
	};

	let { toolPart, forceRaw = false, defaultOpen = false, sessionId, threadId }: Props = $props();

	let isRaw = $state(false);
	let open = $state(defaultOpen);

	$effect(() => {
		isRaw = forceRaw;
	});

	const renderedInput = $derived.by(() => toolPart.input);
	const renderedOutput = $derived.by(() => toolPart.output);
	const renderedError = $derived.by(() => toolPart.errorText);
	const Renderer = $derived.by(() => getToolRenderer(toolPart.toolName));
	const hasOptimizedView = $derived.by(() => Boolean(Renderer));
	const showRaw = $derived.by(() => !hasOptimizedView || isRaw);
	const title = $derived.by(() => getToolTitle(toolPart));
</script>

{#if showRaw}
	<Tool bind:open {defaultOpen} showBorder={false}>
		<ToolHeader
			type="dynamic-tool"
			toolName={toolPart.toolName}
			state={toolPart.state}
			{title}
			isRaw={showRaw}
			onToggleRaw={hasOptimizedView ? (() => (isRaw = !isRaw)) : undefined}
		/>
		<ToolContent>
			<ToolInput input={renderedInput} />
			<ToolOutput output={renderedOutput} errorText={renderedError} />
		</ToolContent>
	</Tool>
{:else if Renderer}
	<Tool bind:open {defaultOpen} showBorder={false}>
		<Renderer
			{toolPart}
			{sessionId}
			{threadId}
			isRaw={isRaw}
			onToggleRaw={() => (isRaw = !isRaw)}
		/>
	</Tool>
{/if}
