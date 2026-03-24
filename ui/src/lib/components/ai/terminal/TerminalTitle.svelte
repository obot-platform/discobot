<script lang="ts">
	import TerminalIcon from "@lucide/svelte/icons/terminal";
	import { cn } from "$lib/utils";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
</script>

<div
	class={cn("flex items-center gap-2 text-sm text-zinc-400", className)}
	{...restProps}
>
	<TerminalIcon class="size-4" />
	{#if children}
		{@render children()}
	{:else}
		Terminal
	{/if}
</div>
