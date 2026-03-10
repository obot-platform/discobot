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
		"flex w-full cursor-pointer items-center gap-3 p-3 text-left transition-colors hover:bg-muted/50",
		className,
	)}
	{...restProps}
>
	{@render children?.()}
</CollapsibleTrigger>
