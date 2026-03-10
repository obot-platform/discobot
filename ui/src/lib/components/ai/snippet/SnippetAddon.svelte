<script lang="ts">
	import type { ComponentProps } from "svelte";
	import { InputGroupAddon } from "$lib/components/ui/input-group";

	let { children, ...restProps }: ComponentProps<typeof InputGroupAddon> = $props();
</script>

<InputGroupAddon {...restProps}>
	{@render children?.()}
</InputGroupAddon>
