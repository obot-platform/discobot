<script lang="ts">
	import { CommandItem } from "$lib/components/ui/command";

	let { children, ...restProps } = $props();
</script>

<CommandItem {...restProps}>
	{@render children?.()}
</CommandItem>
