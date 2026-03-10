<script lang="ts">
	import { Badge } from "$lib/components/ui/badge";
	import { cn } from "$lib/utils";
	import { useSchemaDisplayContext, type HttpMethod } from "./context";

	type Props = { class?: string; children?: () => any };
	let { class: className, children, ...restProps }: Props = $props();

	const schemaDisplay = useSchemaDisplayContext();

	const methodStyles: Record<HttpMethod, string> = {
		GET: "bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400",
		POST: "bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400",
		PUT: "bg-orange-100 text-orange-700 dark:bg-orange-900/30 dark:text-orange-400",
		PATCH: "bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-400",
		DELETE: "bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400",
	};
</script>

<Badge
	class={cn("font-mono text-xs", methodStyles[schemaDisplay.method], className)}
	variant="secondary"
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else}
		{schemaDisplay.method}
	{/if}
</Badge>
