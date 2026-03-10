<script lang="ts">
	import EnvironmentVariableName from "./EnvironmentVariableName.svelte";
	import EnvironmentVariableValue from "./EnvironmentVariableValue.svelte";
	import { cn } from "$lib/utils";
	import { setEnvironmentVariableContext } from "./context";

	type Props = {
		name: string;
		value: string;
		class?: string;
		children?: () => any;
	};

	let { name, value, class: className, children, ...restProps }: Props = $props();
	const environmentVariable = $state({ name: "", value: "" });
	$effect(() => {
		environmentVariable.name = name;
		environmentVariable.value = value;
	});
	setEnvironmentVariableContext(environmentVariable);
</script>

<div
	class={cn("flex items-center justify-between gap-4 px-4 py-3", className)}
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else}
		<div class="flex items-center gap-2">
			<EnvironmentVariableName />
		</div>
		<EnvironmentVariableValue />
	{/if}
</div>
