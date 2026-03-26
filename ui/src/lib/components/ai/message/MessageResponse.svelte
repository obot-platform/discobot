<script lang="ts">
	import { SvelteStreamdown } from "$lib/components/ai/streamdown";
	import { cn } from "$lib/utils";

	type Props = {
		text?: string;
		class?: string;
		children?: () => any;
	};

	let { text, class: className, children, ...restProps }: Props = $props();
</script>

<div class={cn("size-full break-words", className)} {...restProps}>
	{#if text !== undefined}
		<SvelteStreamdown
			{text}
			class="size-full [&>*:first-child]:mt-0 [&>*:last-child]:mb-0"
		/>
	{:else}
		{@render children?.()}
	{/if}
</div>
