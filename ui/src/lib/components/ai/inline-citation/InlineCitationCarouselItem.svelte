<script lang="ts">
	import type { ComponentProps } from "svelte";
	import { CarouselItem } from "$lib/components/ui/carousel";
	import { cn } from "$lib/utils";

	type Props = ComponentProps<typeof CarouselItem> & {
		class?: string;
	};

	let { class: className, ...restProps }: Props = $props();
</script>

<CarouselItem
	class={cn("w-full space-y-2 p-4 pl-8", className)}
	{...restProps}
/>
