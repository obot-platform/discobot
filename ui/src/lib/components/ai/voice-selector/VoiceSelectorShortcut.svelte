<script lang="ts">
	import { CommandShortcut } from "$lib/components/ui/command";
	type Props = { children?: () => any };
	let { children, ...restProps }: Props = $props();
</script>

<CommandShortcut {...restProps}>
	{@render children?.()}
</CommandShortcut>
