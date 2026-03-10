<script lang="ts">
	import { HoverCardTrigger } from "$lib/components/ui/hover-card";

	type Props = {
		children?: () => any;
	};

	let { children, ...restProps }: Props = $props();
</script>

<HoverCardTrigger {...restProps}>
	{@render children?.()}
</HoverCardTrigger>
