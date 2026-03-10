<script lang="ts">
	import CheckIcon from "@lucide/svelte/icons/check";
	import CopyIcon from "@lucide/svelte/icons/copy";
	import { Button } from "$lib/components/ui/button";
	import { cn } from "$lib/utils";
	import { useStackTraceContext } from "./context";

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

	let isCopied = $state(false);
	let timer: ReturnType<typeof setTimeout> | undefined;
	const stackTrace = useStackTraceContext();
	const Icon = $derived.by(() => (isCopied ? CheckIcon : CopyIcon));

	async function copyToClipboard() {
		if (typeof window === "undefined" || !navigator?.clipboard?.writeText) {
			onError?.(new Error("Clipboard API not available"));
			return;
		}

		try {
			await navigator.clipboard.writeText(stackTrace.raw);
			isCopied = true;
			onCopy?.();

			if (timer) {
				clearTimeout(timer);
			}
			timer = setTimeout(() => {
				isCopied = false;
			}, timeout);
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
	class={cn("size-7", className)}
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
