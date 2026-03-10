<script lang="ts">
	import { CardAction } from "$lib/components/ui/card";

	type Props = {
		children?: () => any;
	};

	let { children, ...restProps }: Props = $props();
</script>

<CardAction data-slot="plan-action" {...restProps}>
	{@render children?.()}
</CardAction>
