<script lang="ts">
	import ArrowRightIcon from "@lucide/svelte/icons/arrow-right";
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

	const embla = getEmblaContext("<InlineCitationCarouselNext/>");

	function handleClick(event: ButtonMouseEvent) {
		onclick?.(event);
		if (event.defaultPrevented) {
			return;
		}
		embla.scrollNext();
	}
</script>

<button
	aria-label="Next"
	class={cn("shrink-0", className)}
	onclick={handleClick}
	type="button"
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else}
		<ArrowRightIcon class="size-4 text-muted-foreground" />
	{/if}
</button>
