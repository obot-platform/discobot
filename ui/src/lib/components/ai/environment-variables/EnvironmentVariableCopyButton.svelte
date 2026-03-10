<script lang="ts">
	import CheckIcon from "@lucide/svelte/icons/check";
	import CopyIcon from "@lucide/svelte/icons/copy";
	import type { ButtonProps } from "$lib/components/ui/button";
	import { Button } from "$lib/components/ui/button";
	import { cn } from "$lib/utils";
	import { useEnvironmentVariableContext } from "./context";

	type Props = ButtonProps & {
		onCopy?: () => void;
		onError?: (error: Error) => void;
		timeout?: number;
		copyFormat?: "name" | "value" | "export";
		class?: string;
		children?: () => any;
	};

	let {
		onCopy,
		onError,
		timeout = 2000,
		copyFormat = "value",
		children,
		class: className,
		...restProps
	}: Props = $props();

	let isCopied = $state(false);
	const environmentVariable = useEnvironmentVariableContext();

	async function copyToClipboard() {
		if (typeof window === "undefined" || !navigator?.clipboard?.writeText) {
			onError?.(new Error("Clipboard API not available"));
			return;
		}

		let textToCopy = environmentVariable.value;
		if (copyFormat === "name") {
			textToCopy = environmentVariable.name;
		} else if (copyFormat === "export") {
			textToCopy = `export ${environmentVariable.name}="${environmentVariable.value}"`;
		}

		try {
			await navigator.clipboard.writeText(textToCopy);
			isCopied = true;
			onCopy?.();
			setTimeout(() => {
				isCopied = false;
			}, timeout);
		} catch (error) {
			onError?.(error as Error);
		}
	}
</script>

<Button
	class={cn("size-6 shrink-0", className)}
	onclick={copyToClipboard}
	size="icon"
	variant="ghost"
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else if isCopied}
		<CheckIcon size={12} />
	{:else}
		<CopyIcon size={12} />
	{/if}
</Button>
