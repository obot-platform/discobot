<script lang="ts">
	import { Collapsible } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";

	type Props = {
		open?: boolean;
		defaultOpen?: boolean;
		showBorder?: boolean;
		class?: string;
		children?: () => any;
	};

	let {
		defaultOpen = false,
		open = $bindable(defaultOpen),
		showBorder = true,
		class: className,
		children,
		...restProps
	}: Props = $props();
</script>

<Collapsible
	bind:open
	class={cn("group not-prose mb-4 w-full rounded-md", showBorder ? "border" : "", className)}
	{...restProps}
>
	{@render children?.()}
</Collapsible>
