<script lang="ts">
	import { DropdownMenuItem } from "$lib/components/ui/dropdown-menu";

	let { children, ...restProps } = $props();
</script>

<DropdownMenuItem {...restProps}>
	{@render children?.()}
</DropdownMenuItem>
