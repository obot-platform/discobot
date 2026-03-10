<script lang="ts">
	import { CommandGroup } from "$lib/components/ui/command";

	let { children, ...restProps } = $props();
</script>

<CommandGroup {...restProps}>
	{@render children?.()}
</CommandGroup>
