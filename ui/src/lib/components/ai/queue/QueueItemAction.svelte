<script lang="ts">
	import { Button } from "$lib/components/ui/button";
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		children?: () => any;
		[key: string]: unknown;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<Button
	class={cn(
		"size-auto rounded p-1 text-muted-foreground opacity-0 transition-opacity hover:bg-muted-foreground/10 hover:text-foreground group-hover:opacity-100",
		className,
	)}
	size="icon"
	type="button"
	variant="ghost"
	{...restProps}
>
	{@render children?.()}
</Button>
