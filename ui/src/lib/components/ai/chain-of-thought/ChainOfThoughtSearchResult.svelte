<script lang="ts">
	import { Badge } from "$lib/components/ui/badge";
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<Badge
	class={cn("gap-1 px-2 py-0.5 font-normal text-xs", className)}
	variant="secondary"
	{...restProps}
>
	{@render children?.()}
</Badge>
