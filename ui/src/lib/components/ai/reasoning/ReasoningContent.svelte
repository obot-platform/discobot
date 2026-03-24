<script lang="ts">
	import { cjk } from "@streamdown/cjk";
	import { code } from "@streamdown/code";
	import { math } from "@streamdown/math";
	import { CollapsibleContent } from "$lib/components/ui/collapsible";
	import { onMount } from "svelte";
	import type { PluginConfig } from "streamdown";
	import { ReactStreamdown } from "$lib/components/ai/streamdown";
	import { cn } from "$lib/utils";
	import { useReasoningContext } from "./context";

	type Props = {
		text?: string;
		class?: string;
		children?: () => any;
	};

	const MAX_PREVIEW_LENGTH = 80;
	const MAX_PREVIEW_WORDS = 12;
	const SENTENCE_ENDING = /[.!?:…]$/;

	function stripMarkdownHeader(line: string): string {
		return line
			.replace(/^#{1,6}\s+/, "")
			.replace(/^\*\*(.+)\*\*$/, "$1")
			.replace(/^__(.+)__$/, "$1")
			.replace(/^`(.+)`$/, "$1")
			.trim();
	}

	function extractPreviewText(text?: string): string | undefined {
		if (!text) {
			return undefined;
		}

		const lines = text.replace(/\r\n/g, "\n").split("\n");
		const firstNonEmptyLineIndex = lines.findIndex((line) => line.trim().length > 0);
		if (firstNonEmptyLineIndex === -1) {
			return undefined;
		}

		const firstLine = lines[firstNonEmptyLineIndex].trim();
		const nextLine = lines[firstNonEmptyLineIndex + 1]?.trim() ?? "";
		if (nextLine.length > 0) {
			return undefined;
		}

		const previewText = stripMarkdownHeader(firstLine);
		if (!previewText || previewText.length > MAX_PREVIEW_LENGTH) {
			return undefined;
		}

		const wordCount = previewText.split(/\s+/).filter(Boolean).length;
		if (wordCount === 0 || wordCount > MAX_PREVIEW_WORDS) {
			return undefined;
		}

		if (SENTENCE_ENDING.test(previewText)) {
			return undefined;
		}

		return previewText;
	}

	let { text, class: className, children, ...restProps }: Props = $props();
	const reasoning = useReasoningContext();
	let mermaidPlugin = $state<PluginConfig["mermaid"] | null>(null);

	onMount(() => {
		let cancelled = false;
		void import("@streamdown/mermaid")
			.then((module) => {
				if (!cancelled) {
					mermaidPlugin = module.mermaid;
				}
			})
			.catch(() => {
				// noop
			});

		return () => {
			cancelled = true;
		};
	});

	$effect(() => {
		reasoning.setPreviewText(extractPreviewText(text));
	});

	const plugins = $derived.by(() =>
		mermaidPlugin ? { code, math, cjk, mermaid: mermaidPlugin } : { code, math, cjk },
	);
</script>

<CollapsibleContent
	class={cn(
		"mt-4 px-4 text-sm",
		"data-[state=closed]:fade-out-0 data-[state=closed]:slide-out-to-top-2 data-[state=open]:slide-in-from-top-2 text-muted-foreground outline-none data-[state=closed]:animate-out data-[state=open]:animate-in",
		className,
	)}
	{...restProps}
>
	{#if text !== undefined}
		<ReactStreamdown text={text} plugins={plugins} isAnimating={reasoning.isStreaming} />
	{:else}
		<div class="whitespace-pre-wrap break-words">
			{@render children?.()}
		</div>
	{/if}
</CollapsibleContent>
