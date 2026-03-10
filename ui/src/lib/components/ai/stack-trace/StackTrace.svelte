<script lang="ts">
	import { Collapsible } from "$lib/components/ui/collapsible";
	import { cn } from "$lib/utils";
	import { parseStackTrace } from "./parse";
	import { setStackTraceContext } from "./context";

	type Props = {
		trace: string;
		defaultOpen?: boolean;
		open?: boolean;
		onOpenChange?: (open: boolean) => void;
		onFilePathClick?: (filePath: string, line?: number, column?: number) => void;
		class?: string;
		children?: () => any;
	};

	let {
		trace,
		defaultOpen = false,
		open = $bindable(defaultOpen),
		onOpenChange,
		onFilePathClick,
		class: className,
		children,
		...restProps
	}: Props = $props();

	const parsed = $derived.by(() => parseStackTrace(trace));
	const stackTrace = $state({
		trace: parseStackTrace(""),
		raw: "",
		isOpen: false,
		setIsOpen: (next: boolean) => {
			open = next;
		},
		onFilePathClick: undefined as
			| ((filePath: string, line?: number, column?: number) => void)
			| undefined,
	});

	$effect(() => {
		stackTrace.trace = parsed;
		stackTrace.raw = trace;
		stackTrace.isOpen = open;
		stackTrace.onFilePathClick = onFilePathClick;
	});

	$effect(() => {
		onOpenChange?.(open);
	});

	setStackTraceContext(stackTrace);
</script>

<Collapsible bind:open>
	<div
		class={cn(
			"not-prose w-full overflow-hidden rounded-lg border bg-background font-mono text-sm",
			className,
		)}
		{...restProps}
	>
		{@render children?.()}
	</div>
</Collapsible>
