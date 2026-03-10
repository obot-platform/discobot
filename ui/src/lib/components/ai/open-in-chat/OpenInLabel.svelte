<script lang="ts">
	import { DropdownMenuLabel } from "$lib/components/ui/dropdown-menu";

	let { children, ...restProps } = $props();
</script>

<DropdownMenuLabel {...restProps}>
	{@render children?.()}
</DropdownMenuLabel>
