<script lang="ts">
	import {
		Tool,
		ToolContent,
		ToolHeader,
		ToolInput,
		ToolOutput,
	} from "$lib/components/ai/tool";
	import type { DynamicToolPart } from "$lib/components/ai/types";
	import { getToolRenderer, getToolTitle } from "./registry";

	type Props = {
		toolPart: DynamicToolPart;
		forceRaw?: boolean;
		defaultOpen?: boolean;
		sessionId?: string | null;
		threadId?: string | null;
		onToolApprovalResponse?: (payload: {
			id: string;
			approved: boolean;
			reason?: string;
		}) => void;
	};

	let {
		toolPart,
		forceRaw = false,
		defaultOpen = false,
		sessionId,
		threadId,
		onToolApprovalResponse,
	}: Props = $props();

	const getInitialOpen = () =>
		defaultOpen || toolPart.toolName === "AskUserQuestion";

	let isRaw = $state(false);
	let open = $state(getInitialOpen());

	$effect(() => {
		isRaw = forceRaw;
	});

	$effect(() => {
		if (toolPart.toolName === "AskUserQuestion") {
			open = true;
		}
	});

	const renderedInput = $derived.by(() => toolPart.input);
	const renderedOutput = $derived.by(() => toolPart.output);
	const renderedError = $derived.by(() => toolPart.errorText);
	const Renderer = $derived.by(() => getToolRenderer(toolPart.toolName));
	const hasOptimizedView = $derived.by(() => Boolean(Renderer));
	const isAlwaysExpanded = $derived.by(
		() => toolPart.toolName === "AskUserQuestion",
	);
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
			onToggleRaw={hasOptimizedView ? () => (isRaw = !isRaw) : undefined}
			canCollapse={!isAlwaysExpanded}
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
			{onToolApprovalResponse}
			{isRaw}
			onToggleRaw={() => (isRaw = !isRaw)}
		/>
	</Tool>
{/if}
