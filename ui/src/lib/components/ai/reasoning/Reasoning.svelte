<script lang="ts">
	import { Collapsible } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";
	import { setReasoningContext } from "./context";

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
	let startTime = $state<number | null>(null);
	let previousIsStreaming = $state(false);
	let previousOpen = $state(false);
	let hasTrackedStreamingState = $state(false);
	let hasTrackedOpenState = $state(false);
	let hasUserModifiedOpen = $state(false);
	let isAutomaticOpenChange = $state(false);

	function setOpenAutomatically(nextOpen: boolean): void {
		if (open === nextOpen) {
			return;
		}

		isAutomaticOpenChange = true;
		open = nextOpen;
	}

	const reasoning = $state({
		isStreaming: false,
		isOpen: false,
		setIsOpen: (next: boolean) => {
			open = next;
		},
		setPreviewText: (next?: string) => {
			reasoning.previewText = next;
		},
		previewText: undefined as string | undefined,
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
		if (!hasTrackedOpenState) {
			previousOpen = open;
			hasTrackedOpenState = true;
			return;
		}

		if (open === previousOpen) {
			return;
		}

		if (isAutomaticOpenChange) {
			isAutomaticOpenChange = false;
		} else {
			hasUserModifiedOpen = true;
		}

		previousOpen = open;
	});

	$effect(() => {
		if (!hasTrackedStreamingState) {
			previousIsStreaming = isStreaming;
			hasTrackedStreamingState = true;
			return;
		}

		if (isStreaming && !previousIsStreaming) {
			setOpenAutomatically(true);
		}

		if (!isStreaming && previousIsStreaming && !hasUserModifiedOpen) {
			setOpenAutomatically(false);
		}

		previousIsStreaming = isStreaming;
	});

	$effect(() => {
		onOpenChange?.(open);
	});

	setReasoningContext(reasoning);
</script>

<Collapsible
	data-ai-reasoning
	data-ai-stack
	class={cn(
		"group group/reasoning not-prose mb-4 w-full rounded-md",
		className,
	)}
	bind:open
	{...restProps}
>
	{@render children?.()}
</Collapsible>
