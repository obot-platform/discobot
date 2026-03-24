<script lang="ts">
	import { cn } from "$lib/utils";
	import { useStackTraceContext } from "./context";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
	const stackTrace = useStackTraceContext();
</script>

<span
	class={cn("shrink-0 font-semibold text-destructive", className)}
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else}
		{stackTrace.trace.errorType}
	{/if}
</span>
