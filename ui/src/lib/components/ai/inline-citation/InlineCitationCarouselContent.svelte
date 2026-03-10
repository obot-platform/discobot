<script lang="ts">
	import type { ComponentProps } from "svelte";
	import { CarouselContent } from "$lib/components/ui/carousel";

	type Props = ComponentProps<typeof CarouselContent>;

	let { ...restProps }: Props = $props();
</script>

<CarouselContent {...restProps} />
