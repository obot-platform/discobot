<script lang="ts">
	import { Collapsible } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";
	import { setReasoningContext } from "./context";

	const AUTO_CLOSE_DELAY = 1000;
	const MS_IN_S = 1000;

	type Props = {
		isStreaming?: boolean;
		open?: boolean;
		defaultOpen?: boolean;
		onOpenChange?: (open: boolean) => void;
		duration?: number;
		class?: string;
		children?: () => any;
	};

	let {
		isStreaming = false,
		defaultOpen = true,
		open = $bindable(defaultOpen),
		onOpenChange,
		duration: durationProp,
		class: className,
		children,
		...restProps
	}: Props = $props();

	let duration = $state<number | undefined>(undefined);
	let hasAutoClosed = $state(false);
	let startTime = $state<number | null>(null);

	const reasoning = $state({
		isStreaming: false,
		isOpen: false,
		setIsOpen: (next: boolean) => {
			open = next;
		},
		duration: undefined as number | undefined,
	});

	$effect(() => {
		reasoning.isStreaming = isStreaming;
		reasoning.isOpen = open;
		reasoning.duration = duration;
	});

	$effect(() => {
		if (durationProp !== undefined) {
			duration = durationProp;
		}
	});

	$effect(() => {
		if (isStreaming) {
			if (startTime === null) {
				startTime = Date.now();
			}
		} else if (startTime !== null && durationProp === undefined) {
			duration = Math.ceil((Date.now() - startTime) / MS_IN_S);
			startTime = null;
		}
	});

	$effect(() => {
		if (defaultOpen && !isStreaming && open && !hasAutoClosed) {
			const timer = setTimeout(() => {
				open = false;
				hasAutoClosed = true;
			}, AUTO_CLOSE_DELAY);

			return () => clearTimeout(timer);
		}
	});

	$effect(() => {
		onOpenChange?.(open);
	});

	setReasoningContext(reasoning);
</script>

<Collapsible class={cn("not-prose mb-4", className)} bind:open {...restProps}>
	{@render children?.()}
</Collapsible>
