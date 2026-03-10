<script lang="ts">
	import type { ButtonProps } from "$lib/components/ui/button";
	import { Button } from "$lib/components/ui/button";
	import { cn } from "$lib/utils";

	type Props = ButtonProps & {
		tooltip?: string;
		class?: string;
		children?: () => any;
	};

	let {
		children,
		class: className,
		variant = "ghost",
		size = "sm",
		tooltip,
		...restProps
	}: Props = $props();
</script>

<Button class={cn(className)} {size} type="button" {variant} title={tooltip} {...restProps}>
	{@render children?.()}
</Button>
