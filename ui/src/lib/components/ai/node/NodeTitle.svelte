<script lang="ts">
	import { CardTitle } from "$lib/components/ui/card";

	let { children, ...restProps } = $props();
</script>

<CardTitle {...restProps}>
	{@render children?.()}
</CardTitle>
