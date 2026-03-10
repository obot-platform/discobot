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

	const plugins = $derived.by(() =>
		mermaidPlugin ? { code, math, cjk, mermaid: mermaidPlugin } : { code, math, cjk },
	);
</script>

<CollapsibleContent
	class={cn(
		"mt-4 text-sm",
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
