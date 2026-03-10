<script lang="ts">
	import CheckIcon from "@lucide/svelte/icons/check";
	import CopyIcon from "@lucide/svelte/icons/copy";
	import { Button } from "$lib/components/ui/button";
	import { cn } from "$lib/utils";

	type Props = {
		hash: string;
		onCopy?: () => void;
		onError?: (error: Error) => void;
		timeout?: number;
		class?: string;
		children?: () => any;
	};

	let {
		hash,
		onCopy,
		onError,
		timeout = 2000,
		class: className,
		children,
		...restProps
	}: Props = $props();

	let isCopied = $state(false);
	let timer: ReturnType<typeof setTimeout> | undefined;
	const Icon = $derived.by(() => (isCopied ? CheckIcon : CopyIcon));

	async function copyToClipboard() {
		if (typeof window === "undefined" || !navigator?.clipboard?.writeText) {
			onError?.(new Error("Clipboard API not available"));
			return;
		}

		try {
			if (!isCopied) {
				await navigator.clipboard.writeText(hash);
				isCopied = true;
				onCopy?.();
				if (timer) {
					clearTimeout(timer);
				}
				timer = setTimeout(() => {
					isCopied = false;
				}, timeout);
			}
		} catch (error) {
			onError?.(error as Error);
		}
	}

	$effect(() => {
		return () => {
			if (timer) {
				clearTimeout(timer);
			}
		};
	});
</script>

<Button
	class={cn("size-7 shrink-0", className)}
	onclick={copyToClipboard}
	size="icon"
	variant="ghost"
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else}
		<Icon class="size-3.5" />
	{/if}
</Button>
