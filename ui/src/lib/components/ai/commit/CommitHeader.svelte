<script lang="ts">
	import { CollapsibleTrigger } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<CollapsibleTrigger
	class={cn(
		"group flex cursor-pointer items-center justify-between gap-4 p-3 text-left transition-colors hover:opacity-80",
		className,
	)}
	{...restProps}
>
	{@render children?.()}
</CollapsibleTrigger>
