<script lang="ts">
	import { CardContent } from "$lib/components/ui/card";
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<CardContent class={cn("p-3", className)} {...restProps}>
	{@render children?.()}
</CardContent>
