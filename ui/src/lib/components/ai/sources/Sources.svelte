<script lang="ts">
	import { Collapsible } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";

	type Props = {
		open?: boolean;
		onOpenChange?: (open: boolean) => void;
		class?: string;
		children?: () => any;
	};

	let { open = false, class: className, children, ...restProps }: Props = $props();
</script>

<Collapsible bind:open class={cn("not-prose mb-4 text-primary text-xs", className)} {...restProps}>
	{@render children?.()}
</Collapsible>
