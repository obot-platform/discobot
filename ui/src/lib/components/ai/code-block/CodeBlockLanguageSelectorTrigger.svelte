<script lang="ts">
	import type { ComponentProps } from "svelte";
	import { SelectTrigger } from "$lib/components/ui/select";
	import { cn } from "$lib/utils";

	type Props = ComponentProps<typeof SelectTrigger> & {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<SelectTrigger
	class={cn(
		"h-7 border-none bg-transparent px-2 text-xs shadow-none",
		className,
	)}
	size="sm"
	{...restProps}
>
	{@render children?.()}
</SelectTrigger>
