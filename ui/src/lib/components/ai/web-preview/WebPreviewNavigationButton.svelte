<script lang="ts">
	import { Button } from "$lib/components/ui/button";

	type Props = {
		tooltip?: string;
		disabled?: boolean;
		onclick?: (event: MouseEvent) => void;
		class?: string;
		children?: () => any;
	};

	let {
		tooltip,
		disabled = false,
		onclick,
		class: className,
		children,
		...restProps
	}: Props = $props();
</script>

<Button
	class={`h-8 w-8 p-0 hover:text-foreground ${className ?? ""}`}
	size="sm"
	variant="ghost"
	{disabled}
	{onclick}
	title={tooltip}
	aria-label={tooltip}
	{...restProps}
>
	{@render children?.()}
</Button>
