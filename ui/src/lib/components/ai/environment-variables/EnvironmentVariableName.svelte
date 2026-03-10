<script lang="ts">
	import { cn } from "$lib/utils";
	import { useEnvironmentVariableContext } from "./context";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
	const environmentVariable = useEnvironmentVariableContext();
</script>

<span class={cn("font-mono text-sm", className)} {...restProps}>
	{#if children}
		{@render children()}
	{:else}
		{environmentVariable.name}
	{/if}
</span>
