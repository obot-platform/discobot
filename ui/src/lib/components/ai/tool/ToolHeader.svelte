<script lang="ts">
	import CheckCircleIcon from "@lucide/svelte/icons/circle-check";
	import ChevronDownIcon from "@lucide/svelte/icons/chevron-down";
	import CircleIcon from "@lucide/svelte/icons/circle";
	import ClockIcon from "@lucide/svelte/icons/clock";
	import CodeIcon from "@lucide/svelte/icons/code";
	import WrenchIcon from "@lucide/svelte/icons/wrench";
	import XCircleIcon from "@lucide/svelte/icons/circle-x";
	import type { Component } from "svelte";
	import type { ToolState } from "$lib/components/ai/types";
	import { Badge } from "$lib/components/ui/badge";
	import { CollapsibleTrigger } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";

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

	const statusMeta = $derived.by(() => {
		const labels: Record<ToolState, string> = {
			"input-streaming": "Pending",
			"input-available": "Running",
			"approval-requested": "Awaiting Approval",
			"approval-responded": "Responded",
			"output-available": "Completed",
			"output-error": "Error",
			"output-denied": "Denied",
		};

		const icons: Record<ToolState, Component<{ class?: string }>> = {
			"input-streaming": CircleIcon,
			"input-available": ClockIcon,
			"approval-requested": ClockIcon,
			"approval-responded": CheckCircleIcon,
			"output-available": CheckCircleIcon,
			"output-error": XCircleIcon,
			"output-denied": XCircleIcon,
		};

		return {
			label: labels[state],
			Icon: icons[state],
		};
	});
</script>

<CollapsibleTrigger
	class={cn(
		"flex w-full items-center justify-between gap-4",
		showIcon ? "p-3" : "px-3 py-1.5",
		className,
	)}
	{...restProps}
>
	<div class="flex items-center gap-2">
		{#if showIcon}
			<WrenchIcon class="size-4 text-muted-foreground" />
		{/if}
		{#if verb}
			<Badge
				variant="secondary"
				class="rounded-full bg-primary/10 px-2 py-0.5 font-bold text-primary text-xs"
			>
				{verb}
			</Badge>
		{/if}
		<span class="font-medium text-sm">{rest}</span>
		{#if state !== "output-available"}
			<Badge class="gap-1.5 rounded-full text-xs" variant="secondary">
				<statusMeta.Icon class={cn("size-4", state === "input-available" ? "animate-pulse" : "")} />
				{statusMeta.label}
			</Badge>
		{/if}
	</div>

	<div class="flex items-center gap-2">
		{#if onToggleRaw}
			<span
				role="button"
				tabindex="0"
				class="inline-flex size-7 items-center justify-center rounded-md opacity-0 transition-opacity hover:bg-accent hover:text-accent-foreground group-data-[state=open]:opacity-100"
				onclick={(event) => {
					event.stopPropagation();
					onToggleRaw?.();
				}}
				onkeydown={(event) => {
					if (event.key === "Enter" || event.key === " ") {
						event.preventDefault();
						event.stopPropagation();
						onToggleRaw?.();
					}
				}}
				title={isRaw ? "Show optimized view" : "Show raw view"}
			>
				<CodeIcon class="size-4" />
			</span>
		{/if}
		<ChevronDownIcon class="size-4 text-muted-foreground transition-transform group-data-[state=open]:rotate-180" />
	</div>
</CollapsibleTrigger>
