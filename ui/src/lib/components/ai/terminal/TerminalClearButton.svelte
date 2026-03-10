<script lang="ts">
	import Trash2Icon from "@lucide/svelte/icons/trash-2";
	import { Button } from "$lib/components/ui/button";
	import { cn } from "$lib/utils";
	import { useTerminalContext } from "./context";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();

	const terminal = useTerminalContext();
</script>

{#if terminal.onClear}
	<Button
		class={cn(
			"size-7 shrink-0 text-zinc-400 hover:bg-zinc-800 hover:text-zinc-100",
			className,
		)}
		onclick={terminal.onClear}
		size="icon"
		variant="ghost"
		{...restProps}
	>
		{#if children}
			{@render children()}
		{:else}
			<Trash2Icon size={14} />
		{/if}
	</Button>
{/if}
