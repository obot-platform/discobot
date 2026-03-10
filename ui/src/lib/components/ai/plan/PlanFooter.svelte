<script lang="ts">
	import { CardFooter } from "$lib/components/ui/card";
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<CardFooter class={cn(className)} data-slot="plan-footer" {...restProps}>
	{@render children?.()}
</CardFooter>
