<script lang="ts">
	import BotIcon from "@lucide/svelte/icons/bot";
	import type { HTMLAttributes } from "svelte/elements";
	import { Badge } from "$lib/components/ui/badge";
	import { cn } from "$lib/utils";

	type Props = HTMLAttributes<HTMLDivElement> & {
		name: string;
		model?: string;
		class?: string;
	};

	let {
		name,
		model,
		class: className,
		...restProps
	}: Props = $props();
</script>

<div
	class={cn("flex w-full items-center justify-between gap-4 p-3", className)}
	{...restProps}
>
	<div class="flex items-center gap-2">
		<BotIcon class="size-4 text-muted-foreground" />
		<span class="font-medium text-sm">{name}</span>
		{#if model}
			<Badge class="font-mono text-xs" variant="secondary">
				{model}
			</Badge>
		{/if}
	</div>
</div>
