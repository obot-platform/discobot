<script lang="ts">
	import CheckIcon from "@lucide/svelte/icons/check";
	import CopyIcon from "@lucide/svelte/icons/copy";
	import type { ComponentProps } from "svelte";
	import { InputGroupButton } from "$lib/components/ui/input-group";
	import { useSnippetContext } from "./context";

	type Props = ComponentProps<typeof InputGroupButton> & {
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
		...restProps
	}: Props = $props();
	let isCopied = $state(false);
	const snippet = useSnippetContext();
	let timeoutRef = $state<ReturnType<typeof setTimeout> | null>(null);

	async function copyToClipboard() {
		if (typeof window === "undefined" || !navigator?.clipboard?.writeText) {
			onError?.(new Error("Clipboard API not available"));
			return;
		}

		try {
			if (!isCopied) {
				await navigator.clipboard.writeText(snippet.code);
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

<InputGroupButton
	aria-label="Copy"
	onclick={copyToClipboard}
	size="icon-sm"
	title="Copy"
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else if isCopied}
		<CheckIcon class="size-3.5" size={14} />
	{:else}
		<CopyIcon class="size-3.5" size={14} />
	{/if}
</InputGroupButton>
