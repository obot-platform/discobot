<script lang="ts">
	import { HoverCardContent } from "$lib/components/ui/hover-card";
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<HoverCardContent
	align="start"
	class={cn("w-auto p-2", className)}
	{...restProps}
>
	{@render children?.()}
</HoverCardContent>
