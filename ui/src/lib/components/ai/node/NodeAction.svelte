<script lang="ts">
	import { CardAction } from "$lib/components/ui/card";

	let { children, ...restProps } = $props();
</script>

<CardAction {...restProps}>
	{@render children?.()}
</CardAction>
