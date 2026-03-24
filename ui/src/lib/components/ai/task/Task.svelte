<script lang="ts">
	import { Collapsible } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";

	type Props = {
		open?: boolean;
		class?: string;
		children?: () => any;
	};

	let {
		open = $bindable(true),
		class: className,
		children,
		...restProps
	}: Props = $props();
</script>

<Collapsible bind:open class={cn(className)} {...restProps}>
	{@render children?.()}
</Collapsible>
