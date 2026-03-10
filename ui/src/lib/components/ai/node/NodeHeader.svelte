<script lang="ts">
	import { CardHeader } from "$lib/components/ui/card";
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<CardHeader class={cn("gap-0.5 rounded-t-md border-b bg-secondary p-3!", className)} {...restProps}>
	{@render children?.()}
</CardHeader>
