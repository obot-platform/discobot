<script lang="ts">
	import type { ComponentProps } from "svelte";
	import { SelectContent } from "$lib/components/ui/select";

	type Props = ComponentProps<typeof SelectContent> & {
		children?: () => any;
	};

	let { children, ...restProps }: Props = $props();
</script>

<SelectContent {...restProps}>
	{@render children?.()}
</SelectContent>
