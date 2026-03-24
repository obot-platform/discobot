<script lang="ts">
	import { cn } from "$lib/utils";
	import { useSchemaDisplayContext } from "./context";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();
	const schemaDisplay = useSchemaDisplayContext();
</script>

<p
	class={cn("border-b px-4 py-3 text-muted-foreground text-sm", className)}
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else}
		{schemaDisplay.description}
	{/if}
</p>
