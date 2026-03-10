<script lang="ts">
	import CheckIcon from "@lucide/svelte/icons/check";
	import CopyIcon from "@lucide/svelte/icons/copy";
	import { Button } from "$lib/components/ui/button";
	import { cn } from "$lib/utils";
	import { onDestroy } from "svelte";
	import { useTerminalContext } from "./context";

	type Props = {
		onCopy?: () => void;
		onError?: (error: Error) => void;
		timeout?: number;
		class?: string;
		children?: () => any;
	};

	let {
		onCopy,
		onError,
		timeout = 2000,
		class: className,
		children,
		...restProps
	}: Props = $props();

	const terminal = useTerminalContext();
	let isCopied = $state(false);
	let timeoutHandle = $state<ReturnType<typeof setTimeout> | null>(null);

	async function copyToClipboard() {
		if (typeof navigator === "undefined" || !navigator.clipboard?.writeText) {
			onError?.(new Error("Clipboard API not available"));
			return;
		}

		try {
			await navigator.clipboard.writeText(terminal.output);
			isCopied = true;
			onCopy?.();
			if (timeoutHandle) {
				clearTimeout(timeoutHandle);
			}
			timeoutHandle = setTimeout(() => {
				isCopied = false;
			}, timeout);
		} catch (error) {
			onError?.(error as Error);
		}
	}

	onDestroy(() => {
		if (timeoutHandle) {
			clearTimeout(timeoutHandle);
		}
	});
</script>

<Button
	class={cn(
		"size-7 shrink-0 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100",
		className,
	)}
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
