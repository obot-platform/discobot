<script lang="ts">
	import { DropdownMenu } from "$lib/components/ui/dropdown-menu";

	type Props = { children?: () => any };
	let { children, ...restProps }: Props = $props();
</script>

<DropdownMenu {...restProps}>
	{@render children?.()}
</DropdownMenu>
