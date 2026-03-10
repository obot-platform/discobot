<script lang="ts">
	import XIcon from "@lucide/svelte/icons/x";
	import { Button } from "$lib/components/ui/button";
	import { cn } from "$lib/utils";
	import { useAttachmentContext } from "./context";

	type Props = {
		label?: string;
		class?: string;
		children?: () => any;
	};

	let {
		label = "Remove",
		class: className,
		children,
		...restProps
	}: Props = $props();

	const attachment = useAttachmentContext();

	function handleClick(event: MouseEvent) {
		event.stopPropagation();
		attachment.onRemove?.();
	}
</script>

{#if attachment.onRemove}
	<Button
		aria-label={label}
		class={cn(
			attachment.variant === "grid"
				? [
						"absolute top-2 right-2 size-6 rounded-full p-0",
						"bg-background/80 backdrop-blur-sm",
						"opacity-0 transition-opacity group-hover:opacity-100",
						"hover:bg-background",
						"[&>svg]:size-3",
					]
				: "",
			attachment.variant === "inline"
				? [
						"size-5 rounded p-0",
						"opacity-0 transition-opacity group-hover:opacity-100",
						"[&>svg]:size-2.5",
					]
				: "",
			attachment.variant === "list"
				? ["size-8 shrink-0 rounded p-0", "[&>svg]:size-4"]
				: "",
			className,
		)}
		onclick={handleClick}
		type="button"
		variant="ghost"
		{...restProps}
	>
		{#if children}
			{@render children()}
		{:else}
			<XIcon />
		{/if}
		<span class="sr-only">{label}</span>
	</Button>
{/if}
