<script lang="ts">
	import { CardDescription } from "$lib/components/ui/card";

	let { children, ...restProps } = $props();
</script>

<CardDescription {...restProps}>
	{@render children?.()}
</CardDescription>
