<script lang="ts">
	import type { ComponentProps } from "svelte";
	import { HoverCard } from "$lib/components/ui/hover-card";

	type Props = ComponentProps<typeof HoverCard> & {
		children?: () => any;
	};

	let { children, ...restProps }: Props = $props();
</script>

<HoverCard closeDelay={0} openDelay={0} {...restProps}>
	{@render children?.()}
</HoverCard>
