<script lang="ts">
	import { cn } from "$lib/utils";
	import {
		useEnvironmentVariableContext,
		useEnvironmentVariablesContext,
	} from "./context";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
	const environmentVariable = useEnvironmentVariableContext();
	const environment = useEnvironmentVariablesContext();

	const displayValue = $derived.by(() =>
		environment.showValues
			? environmentVariable.value
			: "•".repeat(Math.min(environmentVariable.value.length, 20)),
	);
</script>

<span
	class={cn(
		"font-mono text-muted-foreground text-sm",
		!environment.showValues && "select-none",
		className,
	)}
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else}
		{displayValue}
	{/if}
</span>
