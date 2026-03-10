<script lang="ts">
	import { ScrollArea } from "$lib/components/ui/scroll-area";
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<ScrollArea class={cn("mt-2 -mb-1", className)} {...restProps}>
	<div class="max-h-40 pr-4">
		<ul>{@render children?.()}</ul>
	</div>
</ScrollArea>
