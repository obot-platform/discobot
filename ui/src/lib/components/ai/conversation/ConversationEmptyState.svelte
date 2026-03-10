<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = {
		title?: string;
		description?: string;
		class?: string;
		icon?: () => any;
		children?: () => any;
	};

	let {
		title = "No messages yet",
		description = "Start a conversation to see messages here",
		class: className,
		icon,
		children,
		...restProps
	}: Props = $props();
</script>

<div
	class={cn(
		"flex size-full flex-col items-center justify-center gap-3 p-8 text-center",
		className,
	)}
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else}
		{#if icon}
			<div class="text-muted-foreground">{@render icon()}</div>
		{/if}
		<div class="space-y-1">
			<h3 class="font-medium text-sm">{title}</h3>
			{#if description}
				<p class="text-muted-foreground text-sm">{description}</p>
			{/if}
		</div>
	{/if}
</div>
