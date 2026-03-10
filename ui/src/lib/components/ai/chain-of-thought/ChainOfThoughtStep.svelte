<script lang="ts">
	import DotIcon from "@lucide/svelte/icons/dot";
	import type { Component } from "svelte";
	import { cn } from "$lib/utils";

	type StepStatus = "complete" | "active" | "pending";

	type Props = {
		icon?: Component<{ class?: string }>;
		label: string;
		description?: string;
		status?: StepStatus;
		class?: string;
		children?: () => any;
	};

	let {
		icon,
		label,
		description,
		status = "complete",
		class: className,
		children,
		...restProps
	}: Props = $props();

	const Icon = $derived.by(() => icon ?? DotIcon);
	const statusClass = $derived.by(() => {
		switch (status) {
			case "active":
				return "text-foreground";
			case "pending":
				return "text-muted-foreground/50";
			default:
				return "text-muted-foreground";
		}
	});
</script>

<div
	class={cn(
		"fade-in-0 slide-in-from-top-2 flex gap-2 text-sm animate-in",
		statusClass,
		className,
	)}
	{...restProps}
>
	<div class="relative mt-0.5">
		<Icon class="size-4" />
		<div class="absolute top-7 bottom-0 left-1/2 -mx-px w-px bg-border"></div>
	</div>
	<div class="flex-1 space-y-2 overflow-hidden">
		<div>{label}</div>
		{#if description}
			<div class="text-muted-foreground text-xs">{description}</div>
		{/if}
		{@render children?.()}
	</div>
</div>
