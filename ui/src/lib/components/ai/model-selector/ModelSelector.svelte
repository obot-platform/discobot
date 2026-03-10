<script lang="ts">
	import { Dialog } from "$lib/components/ui/dialog";

	type Props = {
		open?: boolean;
		children?: () => any;
	};

	let { open = $bindable(false), children, ...restProps }: Props = $props();
</script>

<Dialog bind:open {...restProps}>
	{@render children?.()}
</Dialog>
