<script lang="ts">
	import type { HTMLAttributes } from "svelte/elements";
	import { getEmblaContext } from "$lib/components/ui/carousel/context.js";
	import { cn } from "$lib/utils";

	type Props = HTMLAttributes<HTMLDivElement> & {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();

	const embla = getEmblaContext("<InlineCitationCarouselIndex/>");

	const indexLabel = $derived.by(() => {
		const current = embla.selectedIndex + 1;
		const count = embla.scrollSnaps.length;
		return `${current}/${count}`;
	});
</script>

<div
	class={cn(
		"flex flex-1 items-center justify-end px-3 py-1 text-muted-foreground text-xs",
		className,
	)}
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else}
		{indexLabel}
	{/if}
</div>
