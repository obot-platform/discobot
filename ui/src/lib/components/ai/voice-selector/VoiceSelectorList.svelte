<script lang="ts">
	import { CommandList } from "$lib/components/ui/command";
	type Props = { children?: () => any };
	let { children, ...restProps }: Props = $props();
</script>

<CommandList {...restProps}>
	{@render children?.()}
</CommandList>
