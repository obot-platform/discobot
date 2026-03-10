<script lang="ts">
	import { DialogTrigger } from "$lib/components/ui/dialog";

	type Props = {
		children?: () => any;
	};

	let { children, ...restProps }: Props = $props();
</script>

<DialogTrigger {...restProps}>
	{@render children?.()}
</DialogTrigger>
