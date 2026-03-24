<script lang="ts">
	import CheckCircleIcon from "@lucide/svelte/icons/circle-check";
	import ChevronDownIcon from "@lucide/svelte/icons/chevron-down";
	import CircleIcon from "@lucide/svelte/icons/circle";
	import ClockIcon from "@lucide/svelte/icons/clock";
	import CodeIcon from "@lucide/svelte/icons/code";
	import XCircleIcon from "@lucide/svelte/icons/circle-x";
	import type { Component, ComponentProps } from "svelte";
	import type { ToolState } from "$lib/components/ai/types";
	import { Badge } from "$lib/components/ui/badge";
	import { CollapsibleTrigger } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";

	type Props = ComponentProps<typeof CollapsibleTrigger> & {
		title?: string;
		state: ToolState;
		class?: string;
	};

	let {
		title = "Sandbox",
		state,
		class: className,
		...restProps
	}: Props = $props();

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
	class={cn("flex w-full items-center justify-between gap-4 p-3", className)}
	{...restProps}
>
	<div class="flex items-center gap-2">
		<CodeIcon class="size-4 text-muted-foreground" />
		<span class="font-medium text-sm">{title}</span>
		<Badge class="gap-1.5 rounded-full text-xs" variant="secondary">
			<statusMeta.Icon
				class={cn("size-4", state === "input-available" ? "animate-pulse" : "")}
			/>
			{statusMeta.label}
		</Badge>
	</div>
	<ChevronDownIcon
		class="size-4 text-muted-foreground transition-transform group-data-[state=open]:rotate-180"
	/>
</CollapsibleTrigger>
