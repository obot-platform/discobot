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

<Collapsible
	bind:open
	class={cn(
		"not-prose group mb-4 w-full overflow-hidden rounded-md border",
		className,
	)}
	{...restProps}
>
	{@render children?.()}
</Collapsible>
