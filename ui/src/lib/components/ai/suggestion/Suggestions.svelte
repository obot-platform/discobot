<script lang="ts">
	import {
		ScrollArea,
		ScrollAreaScrollbar,
	} from "$lib/components/ui/scroll-area";
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children }: Props = $props();
</script>

<ScrollArea class="w-full overflow-x-auto whitespace-nowrap">
	<div class={cn("flex w-max flex-nowrap items-center gap-2", className)}>
		{@render children?.()}
	</div>
	<ScrollAreaScrollbar class="hidden" orientation="horizontal" />
</ScrollArea>
