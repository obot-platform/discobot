<script lang="ts">
	import { CardContent } from "$lib/components/ui/card";
	import { CollapsibleContent } from "$lib/components/ui/collapsible";

	type Props = {
		children?: () => any;
	};

	let { children, ...restProps }: Props = $props();
</script>

<CollapsibleContent>
	<CardContent data-slot="plan-content" {...restProps}>
		{@render children?.()}
	</CardContent>
</CollapsibleContent>
