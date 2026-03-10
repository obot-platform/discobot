<script lang="ts">
	import { DropdownMenuContent } from "$lib/components/ui/dropdown-menu";
	import { cn } from "$lib/utils";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
</script>

<DropdownMenuContent align="start" class={cn(className)} {...restProps}>
	{@render children?.()}
</DropdownMenuContent>
