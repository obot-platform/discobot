<script lang="ts">
	import CheckCircleIcon from "@lucide/svelte/icons/circle-check";
	import ClockIcon from "@lucide/svelte/icons/clock";
	import LoaderCircleIcon from "@lucide/svelte/icons/loader-circle";
	import XCircleIcon from "@lucide/svelte/icons/circle-x";
	import type { Component } from "svelte";
	import { Shimmer } from "$lib/components/ai";
	import type { ToolState } from "$lib/components/ai/types";
	import { Badge } from "$lib/components/ui/badge";
	import { cn } from "$lib/utils";
	import {
		getToolStatusLabel,
		isToolPreparingState,
		isToolRunningState,
	} from "./tool-status";

	type Props = {
		state: ToolState;
		class?: string;
	};

	let { state, class: className }: Props = $props();

	const statusMeta = $derived.by(() => {
		const icons: Record<
			Exclude<ToolState, "output-available">,
			Component<{ class?: string }>
		> = {
			"input-streaming": LoaderCircleIcon,
			"input-available": LoaderCircleIcon,
			"approval-requested": ClockIcon,
			"approval-responded": CheckCircleIcon,
			"output-error": XCircleIcon,
			"output-denied": XCircleIcon,
		};

		if (state === "output-available") {
			return null;
		}

		return {
			label: getToolStatusLabel(state),
			Icon: icons[state],
			preparing: isToolPreparingState(state),
			spinning: isToolRunningState(state),
		};
	});
</script>

{#if statusMeta}
	<Badge
		class={cn("gap-1.5 rounded-full text-xs", className)}
		variant="secondary"
	>
		<statusMeta.Icon
			class={cn("size-4 shrink-0", statusMeta.spinning ? "animate-spin" : "")}
		/>
		{#if statusMeta.preparing}
			<Shimmer as="span" text={statusMeta.label} class="font-medium" />
		{:else}
			{statusMeta.label}
		{/if}
	</Badge>
{/if}
