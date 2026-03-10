<script lang="ts">
	import { cn } from "$lib/utils";
	import { setChainOfThoughtContext } from "./context";

	type Props = {
		open?: boolean;
		defaultOpen?: boolean;
		onOpenChange?: (open: boolean) => void;
		class?: string;
		children?: () => any;
	};

	let {
		defaultOpen = false,
		open = $bindable(defaultOpen),
		onOpenChange,
		class: className,
		children,
		...restProps
	}: Props = $props();

	const chainOfThought = $state({
		isOpen: false,
		setIsOpen: (next: boolean) => {
			open = next;
		},
	});

	$effect(() => {
		chainOfThought.isOpen = open;
	});

	$effect(() => {
		onOpenChange?.(open);
	});

	setChainOfThoughtContext(chainOfThought);
</script>

<div class={cn("not-prose max-w-prose space-y-4", className)} {...restProps}>
	{@render children?.()}
</div>
