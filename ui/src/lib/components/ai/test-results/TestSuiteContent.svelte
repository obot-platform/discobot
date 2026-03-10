<script lang="ts">
	import { CollapsibleContent } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
</script>

<CollapsibleContent class={cn("border-t", className)} {...restProps}>
	<div class="divide-y">
		{@render children?.()}
	</div>
</CollapsibleContent>
