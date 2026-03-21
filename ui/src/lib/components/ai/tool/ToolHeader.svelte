<script lang="ts">
	import WrenchIcon from "@lucide/svelte/icons/wrench";
	import type { ToolState } from "$lib/components/ai/types";
	import { Badge } from "$lib/components/ui/badge";
	import { CollapsibleTrigger } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";
	import ToolHeaderControls from "./ToolHeaderControls.svelte";
	import ToolHeaderStatus from "./ToolHeaderStatus.svelte";

	type Props = {
		type: string;
		state: ToolState;
		title?: string;
		toolName?: string;
		showIcon?: boolean;
		isRaw?: boolean;
		onToggleRaw?: () => void;
		class?: string;
	};

	let {
		type,
		state,
		title,
		toolName,
		showIcon = true,
		isRaw,
		onToggleRaw,
		class: className,
		...restProps
	}: Props = $props();

	const derivedName = $derived.by(() =>
		type === "dynamic-tool" ? (toolName ?? "tool") : type.split("-").slice(1).join("-"),
	);
	const displayText = $derived.by(() => title ?? derivedName);
	const colonIndex = $derived.by(() => displayText.indexOf(": "));
	const hasVerb = $derived.by(() => colonIndex !== -1);
	const verb = $derived.by(() => (hasVerb ? displayText.slice(0, colonIndex) : null));
	const rest = $derived.by(() => (hasVerb ? displayText.slice(colonIndex + 2) : displayText));
</script>

<div class="flex items-center justify-between gap-4">
	<CollapsibleTrigger
		class={cn(
			"flex w-full min-w-0 items-center gap-2 text-left",
			showIcon ? "p-3" : "px-3 py-1.5",
			className,
		)}
		{...restProps}
	>
		<div class="flex min-w-0 items-center gap-2">
			{#if showIcon}
				<WrenchIcon class="size-4 shrink-0 text-muted-foreground" />
			{/if}
			{#if verb}
				<Badge
					variant="secondary"
					class="rounded-full bg-primary/10 px-2 py-0.5 font-bold text-primary text-xs"
				>
					{verb}
				</Badge>
			{/if}
			<span class="truncate font-medium text-sm">{rest}</span>
			<ToolHeaderStatus {state} />
		</div>
	</CollapsibleTrigger>
	<ToolHeaderControls {isRaw} {onToggleRaw} />
</div>
