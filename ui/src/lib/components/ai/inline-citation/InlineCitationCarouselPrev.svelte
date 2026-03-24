<script lang="ts">
	import ArrowLeftIcon from "@lucide/svelte/icons/arrow-left";
	import type { HTMLButtonAttributes } from "svelte/elements";
	import { getEmblaContext } from "$lib/components/ui/carousel/context.js";
	import { cn } from "$lib/utils";

	type Props = HTMLButtonAttributes & {
		class?: string;
		children?: () => any;
	};

	type ButtonMouseEvent = MouseEvent & {
		currentTarget: EventTarget & HTMLButtonElement;
	};

	let { class: className, onclick, children, ...restProps }: Props = $props();

	const embla = getEmblaContext("<InlineCitationCarouselPrev/>");

	function handleClick(event: ButtonMouseEvent) {
		onclick?.(event);
		if (event.defaultPrevented) {
			return;
		}
		embla.scrollPrev();
	}
</script>

<button
	aria-label="Previous"
	class={cn("shrink-0", className)}
	onclick={handleClick}
	type="button"
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else}
		<ArrowLeftIcon class="size-4 text-muted-foreground" />
	{/if}
</button>
