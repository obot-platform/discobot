<script lang="ts">
	import type { ButtonProps } from "$lib/components/ui/button";
	import { Button } from "$lib/components/ui/button";
	import { cn } from "$lib/utils";

	type Props = ButtonProps & {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<Button class={cn("h-8 px-3 text-sm", className)} type="button" {...restProps}>
	{@render children?.()}
</Button>
