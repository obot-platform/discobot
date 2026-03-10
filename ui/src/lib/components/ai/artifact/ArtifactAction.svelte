<script lang="ts">
	import type { Component } from "svelte";
	import type { ButtonProps } from "$lib/components/ui/button";
	import { Button } from "$lib/components/ui/button";
	import { cn } from "$lib/utils";

	type Props = ButtonProps & {
		tooltip?: string;
		label?: string;
		icon?: Component<{ class?: string }>;
		children?: () => any;
		class?: string;
	};

	let {
		tooltip,
		label,
		icon: Icon,
		children,
		class: className,
		size = "sm",
		variant = "ghost",
		...restProps
	}: Props = $props();
</script>

<Button
	class={cn("size-8 p-0 text-muted-foreground hover:text-foreground", className)}
	{size}
	type="button"
	{variant}
	title={tooltip}
	{...restProps}
>
	{#if Icon}
		<Icon class="size-4" />
	{:else if children}
		{@render children()}
	{/if}
	<span class="sr-only">{label || tooltip}</span>
</Button>
