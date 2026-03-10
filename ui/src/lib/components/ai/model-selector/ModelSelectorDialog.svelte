<script lang="ts">
	import { CommandDialog } from "$lib/components/ui/command";

	type Props = {
		children?: () => any;
	};

	let { children, ...restProps }: Props = $props();
</script>

<CommandDialog {...restProps}>
	{@render children?.()}
</CommandDialog>
