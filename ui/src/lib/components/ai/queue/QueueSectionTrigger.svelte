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
		"group flex w-full items-center justify-between rounded-md bg-muted/40 px-3 py-2 text-left font-medium text-muted-foreground text-sm transition-colors hover:bg-muted",
		className,
	)}
	type="button"
	{...restProps}
>
	{@render children?.()}
</CollapsibleTrigger>
