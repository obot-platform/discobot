<script lang="ts">
	import type { ButtonProps } from "$lib/components/ui/button";
	import { Button } from "$lib/components/ui/button";
	import { cn } from "$lib/utils";

	type Props = Omit<ButtonProps, "onclick"> & {
		suggestion: string;
		onclick?: (suggestion: string) => void;
		children?: () => any;
		class?: string;
	};

	let {
		suggestion,
		onclick,
		class: className,
		variant = "outline",
		size = "sm",
		children,
		...restProps
	}: Props = $props();

	function handleClick() {
		onclick?.(suggestion);
	}
</script>

<Button
	class={cn("cursor-pointer rounded-full px-4", className)}
	onclick={handleClick}
	{size}
	type="button"
	{variant}
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else}
		{suggestion}
	{/if}
</Button>
