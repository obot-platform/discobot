<script lang="ts">
	import { InputGroupAddon } from "$lib/components/ui/input-group";
	import { cn } from "$lib/utils";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
</script>

<InputGroupAddon align="block-start" class={cn("order-first flex-wrap gap-1", className)} {...restProps}>
	{@render children?.()}
</InputGroupAddon>
