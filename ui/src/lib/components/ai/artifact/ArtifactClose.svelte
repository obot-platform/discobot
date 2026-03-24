<script lang="ts">
	import XIcon from "@lucide/svelte/icons/x";
	import type { ButtonProps } from "$lib/components/ui/button";
	import { Button } from "$lib/components/ui/button";
	import { cn } from "$lib/utils";

	type Props = ButtonProps & {
		class?: string;
		children?: () => any;
	};

	let {
		class: className,
		children,
		size = "sm",
		variant = "ghost",
		...restProps
	}: Props = $props();
</script>

<Button
	class={cn(
		"size-8 p-0 text-muted-foreground hover:text-foreground",
		className,
	)}
	{size}
	type="button"
	{variant}
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else}
		<XIcon class="size-4" />
	{/if}
	<span class="sr-only">Close</span>
</Button>
