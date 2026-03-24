<script lang="ts">
	import CheckCircleIcon from "@lucide/svelte/icons/circle-check";
	import ClockIcon from "@lucide/svelte/icons/clock";
	import LoaderCircleIcon from "@lucide/svelte/icons/loader-circle";
	import XCircleIcon from "@lucide/svelte/icons/circle-x";
	import type { Component } from "svelte";
	import type { ToolState } from "$lib/components/ai/types";
	import { Badge } from "$lib/components/ui/badge";
	import { cn } from "$lib/utils";

	type Props = {
		state: ToolState;
		class?: string;
	};

	let { state, class: className }: Props = $props();

	const statusMeta = $derived.by(() => {
		const labels: Record<Exclude<ToolState, "output-available">, string> = {
			"input-streaming": "Pending",
			"input-available": "Running",
			"approval-requested": "Awaiting Approval",
			"approval-responded": "Responded",
			"output-error": "Error",
			"output-denied": "Denied",
		};

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
			label: labels[state],
			Icon: icons[state],
			spinning: state === "input-streaming" || state === "input-available",
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
		{statusMeta.label}
	</Badge>
{/if}
