<script lang="ts">
	import { cn } from "$lib/utils";

	type Props = {
		passed?: number;
		failed?: number;
		skipped?: number;
		class?: string;
		children?: () => any;
	};

	let {
		passed = 0,
		failed = 0,
		skipped = 0,
		class: className,
		children,
		...restProps
	}: Props = $props();
</script>

<div
	class={cn("ml-auto flex items-center gap-2 text-xs", className)}
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else}
		{#if passed > 0}
			<span class="text-green-600 dark:text-green-400">{passed} passed</span>
		{/if}
		{#if failed > 0}
			<span class="text-red-600 dark:text-red-400">{failed} failed</span>
		{/if}
		{#if skipped > 0}
			<span class="text-yellow-600 dark:text-yellow-400">{skipped} skipped</span
			>
		{/if}
	{/if}
</div>
