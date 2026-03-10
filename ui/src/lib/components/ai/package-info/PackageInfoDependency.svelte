<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = {
		name: string;
		version?: string;
		class?: string;
		children?: () => any;
	};

	let { name, version, class: className, children, ...restProps }: Props = $props();
</script>

<div class={cn("flex items-center justify-between text-sm", className)} {...restProps}>
	{#if children}
		{@render children()}
	{:else}
		<span class="font-mono text-muted-foreground">{name}</span>
		{#if version}
			<span class="font-mono text-xs">{version}</span>
		{/if}
	{/if}
</div>
