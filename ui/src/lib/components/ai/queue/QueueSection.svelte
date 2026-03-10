<script lang="ts">
	import { Collapsible } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";

	type Props = {
		open?: boolean;
		defaultOpen?: boolean;
		class?: string;
		children?: () => any;
	};

	let {
		defaultOpen = true,
		open = $bindable(defaultOpen),
		class: className,
		children,
		...restProps
	}: Props = $props();
</script>

<Collapsible bind:open class={cn(className)} {...restProps}>
	{@render children?.()}
</Collapsible>
