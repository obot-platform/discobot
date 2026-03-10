<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<div
	class={cn(
		"flex items-center justify-center p-4 text-muted-foreground text-sm",
		className,
	)}
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else}
		No attachments
	{/if}
</div>
