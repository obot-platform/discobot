<script lang="ts">
	import CircleCheckIcon from "@lucide/svelte/icons/circle-check";
	import CircleIcon from "@lucide/svelte/icons/circle";
	import Loader2Icon from "@lucide/svelte/icons/loader-2";
	import type { SessionStatusValue } from "$lib/shell-types";
	import { cn } from "$lib/utils";

	type Props = {
		status: SessionStatusValue;
		class?: string;
		iconClass?: string;
		labelClass?: string;
		showLabel?: boolean;
	};

	let {
		status,
		class: className,
		iconClass,
		labelClass,
		showLabel = true,
	}: Props = $props();

	function normalizedStatus(status: SessionStatusValue): string {
		return status.toLowerCase();
	}

	function statusLabel(status: SessionStatusValue): string {
		return status
			.replace(/_/g, " ")
			.replace(/\b\w/g, (char) => char.toUpperCase());
	}

	function statusTone(status: SessionStatusValue): string {
		switch (normalizedStatus(status)) {
			case "error":
				return "text-destructive";
			case "ready":
				return "text-green-500";
			case "initializing":
			case "reinitializing":
			case "cloning":
			case "pulling_image":
			case "creating_sandbox":
				return "text-yellow-500";
			case "removing":
				return "text-orange-500";
			default:
				return "text-muted-foreground";
		}
	}

	function isSpinningStatus(status: SessionStatusValue): boolean {
		switch (normalizedStatus(status)) {
			case "initializing":
			case "reinitializing":
			case "cloning":
			case "pulling_image":
			case "creating_sandbox":
			case "removing":
				return true;
			default:
				return false;
		}
	}
</script>

<span
	class={cn("inline-flex items-center", showLabel && "gap-1.5", className)}
	title={statusLabel(status)}
	aria-label={statusLabel(status)}
>
	<span class={cn("inline-flex items-center", statusTone(status), iconClass)}>
		{#if isSpinningStatus(status)}
			<Loader2Icon class="size-3.5 animate-spin" />
		{:else if normalizedStatus(status) === "ready"}
			<CircleCheckIcon class="size-3.5" />
		{:else}
			<CircleIcon class="size-2.5 fill-current" />
		{/if}
	</span>
	{#if showLabel}
		<span class={cn("text-sm text-muted-foreground", labelClass)}>{statusLabel(status)}</span>
	{/if}
</span>
