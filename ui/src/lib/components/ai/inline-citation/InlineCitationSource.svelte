<script lang="ts">
	import type { HTMLAttributes } from "svelte/elements";
	import { cn } from "$lib/utils";

	type Props = HTMLAttributes<HTMLDivElement> & {
		title?: string;
		url?: string;
		description?: string;
		class?: string;
		children?: () => any;
	};

	let {
		title,
		url,
		description,
		class: className,
		children,
		...restProps
	}: Props = $props();
</script>

<div class={cn("space-y-1", className)} {...restProps}>
	{#if title}
		<h4 class="truncate font-medium text-sm leading-tight">{title}</h4>
	{/if}
	{#if url}
		<p class="truncate break-all text-muted-foreground text-xs">{url}</p>
	{/if}
	{#if description}
		<p class="line-clamp-3 text-muted-foreground text-sm leading-relaxed">
			{description}
		</p>
	{/if}
	{@render children?.()}
</div>
