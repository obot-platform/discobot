<script lang="ts">
	import { HoverCardContent } from "$lib/components/ui/hover-card";
	import { cn } from "$lib/utils";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
</script>

<HoverCardContent class={cn("min-w-60 divide-y overflow-hidden p-0", className)} {...restProps}>
	{@render children?.()}
</HoverCardContent>
