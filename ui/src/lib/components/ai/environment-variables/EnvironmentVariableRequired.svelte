<script lang="ts">
	import { Badge } from "$lib/components/ui/badge";
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<Badge class={cn("text-xs", className)} variant="secondary" {...restProps}>
	{#if children}
		{@render children()}
	{:else}
		Required
	{/if}
</Badge>
