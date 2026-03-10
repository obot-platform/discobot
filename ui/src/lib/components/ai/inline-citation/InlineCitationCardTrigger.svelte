<script lang="ts">
	import type { ComponentProps } from "svelte";
	import { Badge } from "$lib/components/ui/badge";
	import { HoverCardTrigger } from "$lib/components/ui/hover-card";
	import { cn } from "$lib/utils";

	type Props = ComponentProps<typeof Badge> & {
		sources: string[];
		class?: string;
		children?: () => any;
	};

	let {
		sources,
		class: className,
		children,
		...restProps
	}: Props = $props();

	const sourceLabel = $derived.by(() => {
		const firstSource = sources[0];
		if (!firstSource) {
			return "unknown";
		}

		try {
			const hostname = new URL(firstSource).hostname;
			return sources.length > 1
				? `${hostname} +${sources.length - 1}`
				: hostname;
		} catch {
			return sources.length > 1
				? `${firstSource} +${sources.length - 1}`
				: firstSource;
		}
	});
</script>

<HoverCardTrigger>
	<Badge
		class={cn("ml-1 rounded-full", className)}
		variant="secondary"
		{...restProps}
	>
		{#if children}
			{@render children()}
		{:else}
			{sourceLabel}
		{/if}
	</Badge>
</HoverCardTrigger>
