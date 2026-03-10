<script lang="ts">
	import ChevronDownIcon from "@lucide/svelte/icons/chevron-down";
	import { cn } from "$lib/utils";
	import { useStackTraceContext } from "./context";

	type Props = {
		class?: string;
	};

	let { class: className, ...restProps }: Props = $props();
	const stackTrace = useStackTraceContext();
</script>

<div class={cn("flex size-7 items-center justify-center", className)} {...restProps}>
	<ChevronDownIcon
		class={cn(
			"size-4 text-muted-foreground transition-transform",
			stackTrace.isOpen ? "rotate-180" : "rotate-0",
		)}
	/>
</div>
