<script lang="ts">
	import { cjk } from "@streamdown/cjk";
	import { code } from "@streamdown/code";
	import { math } from "@streamdown/math";
	import { LinkSafetyModal } from "$lib/components/ai/link-safety-modal";
	import { hasIncompleteCodeFence } from "$lib/markdown/incomplete-code-utils";
	import { parseMarkdownIntoBlocks } from "$lib/markdown/parse-blocks";
	import { parseMarkdownToHast } from "$lib/markdown/pipeline";
	import { preprocessMarkdown } from "$lib/markdown/preprocess";
	import { renderMarkdownTree } from "$lib/markdown/render-dom";
	import type { MarkdownMode, MarkdownPluginConfig } from "$lib/markdown/types";
	import { cn } from "$lib/utils";

	type Props = {
		text: string;
		class?: string;
		plugins?: MarkdownPluginConfig;
		mode?: MarkdownMode;
		isAnimating?: boolean;
		animated?: boolean;
	};

	let {
		text,
		class: className,
		plugins = { code, math, cjk },
		mode = "streaming",
		isAnimating = false,
		animated = false,
	}: Props = $props();

	let host = $state<HTMLDivElement | null>(null);
	let pendingUrl = $state<string | null>(null);
	let renderedBlocks: string[] = [];

	function closeModal() {
		pendingUrl = null;
	}

	function createBlockElement(
		block: string,
		index: number,
		isIncompleteCodeFence: boolean,
	): HTMLDivElement {
		const wrapper = document.createElement("div");
		wrapper.className = cn("min-w-0", index > 0 && "mt-4");
		wrapper.dataset.markdownBlock = String(index);
		wrapper.dataset.incompleteCodeFence = String(isIncompleteCodeFence);

		try {
			const tree = parseMarkdownToHast(block, plugins);
			wrapper.append(
				renderMarkdownTree(tree, {
					isIncompleteCodeFence,
					onLinkClick: (url) => {
						pendingUrl = url;
					},
					plugins,
				}),
			);
		} catch {
			const fallback = document.createElement("div");
			fallback.className = "whitespace-pre-wrap break-words";
			fallback.textContent = block;
			wrapper.append(fallback);
		}

		return wrapper;
	}

	function syncBlocks(nextBlocks: string[]) {
		if (!host) {
			return;
		}

		while (host.children.length > nextBlocks.length) {
			host.lastChild?.remove();
		}

		for (let index = 0; index < nextBlocks.length; index += 1) {
			const block = nextBlocks[index];
			const isLastBlock = index === nextBlocks.length - 1;
			const isIncompleteCodeFence =
				isAnimating && isLastBlock && hasIncompleteCodeFence(block);
			const existingBlock = host.children[index] as HTMLDivElement | undefined;
			if (
				renderedBlocks[index] === block &&
				existingBlock &&
				existingBlock.dataset.incompleteCodeFence ===
					String(isIncompleteCodeFence)
			) {
				continue;
			}

			const blockElement = createBlockElement(
				block,
				index,
				isIncompleteCodeFence,
			);
			if (host.children[index]) {
				host.children[index].replaceWith(blockElement);
			} else {
				// eslint-disable-next-line svelte/no-dom-manipulating
				host.append(blockElement);
			}
		}

		Array.from(host.children).forEach((child, index) => {
			(child as HTMLDivElement).className = cn("min-w-0", index > 0 && "mt-4");
			(child as HTMLDivElement).dataset.markdownBlock = String(index);
		});
	}

	$effect(() => {
		if (!host) {
			return;
		}

		const processed = preprocessMarkdown(text, { isAnimating, mode });
		const nextBlocks =
			mode === "streaming" ? parseMarkdownIntoBlocks(processed) : [processed];

		syncBlocks(nextBlocks);
		renderedBlocks = [...nextBlocks];
		void animated;
	});
</script>

<div
	bind:this={host}
	class={cn("size-full whitespace-normal", className)}
></div>

<LinkSafetyModal
	isOpen={pendingUrl !== null}
	onClose={closeModal}
	url={pendingUrl ?? ""}
/>
