<script lang="ts">
	import CheckIcon from "@lucide/svelte/icons/check";
	import CopyIcon from "@lucide/svelte/icons/copy";
	import type { ComponentProps } from "svelte";
	import { Button } from "$lib/components/ui/button";
	import { cn } from "$lib/utils";
	import { useCodeBlockContext } from "./context";

	type Props = ComponentProps<typeof Button> & {
		onCopy?: () => void;
		onError?: (error: Error) => void;
		timeout?: number;
		children?: () => any;
	};

	let {
		onCopy,
		onError,
		timeout = 2000,
		children,
		class: className,
		...restProps
	}: Props = $props();

	let isCopied = $state(false);
	let timeoutRef = $state<ReturnType<typeof setTimeout> | null>(null);

	const codeBlock = useCodeBlockContext();

	async function copyToClipboard() {
		if (typeof window === "undefined" || !navigator?.clipboard?.writeText) {
			onError?.(new Error("Clipboard API not available"));
			return;
		}

		try {
			if (!isCopied) {
				await navigator.clipboard.writeText(codeBlock.code);
				isCopied = true;
				onCopy?.();
				timeoutRef = setTimeout(() => {
					isCopied = false;
				}, timeout);
			}
		} catch (error) {
			onError?.(error as Error);
		}
	}

	$effect(() => {
		return () => {
			if (timeoutRef) {
				clearTimeout(timeoutRef);
			}
		};
	});
</script>

<Button
	class={cn("shrink-0", className)}
	onclick={copyToClipboard}
	size="icon"
	variant="ghost"
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else if isCopied}
		<CheckIcon size={14} />
	{:else}
		<CopyIcon size={14} />
	{/if}
</Button>
