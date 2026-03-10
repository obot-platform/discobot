<script lang="ts">
	import { cjk } from "@streamdown/cjk";
	import { code } from "@streamdown/code";
	import { math } from "@streamdown/math";
	import { onMount } from "svelte";
	import type { PluginConfig } from "streamdown";
	import { ReactStreamdown } from "$lib/components/ai/streamdown";
	import { cn } from "$lib/utils";

	type Props = {
		text?: string;
		class?: string;
		children?: () => any;
	};

	let { text, class: className, children, ...restProps }: Props = $props();
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

<div class={cn("size-full break-words", className)} {...restProps}>
	{#if text !== undefined}
		<ReactStreamdown
			text={text}
			plugins={plugins}
			class="size-full [&>*:first-child]:mt-0 [&>*:last-child]:mb-0"
		/>
	{:else}
		{@render children?.()}
	{/if}
</div>
