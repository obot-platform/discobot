<script lang="ts">
	import { cn } from "$lib/utils";
	import TerminalActions from "./TerminalActions.svelte";
	import TerminalClearButton from "./TerminalClearButton.svelte";
	import TerminalContent from "./TerminalContent.svelte";
	import TerminalCopyButton from "./TerminalCopyButton.svelte";
	import TerminalHeader from "./TerminalHeader.svelte";
	import TerminalStatus from "./TerminalStatus.svelte";
	import TerminalTitle from "./TerminalTitle.svelte";
	import { setTerminalContext } from "./context";

	type Props = {
		output: string;
		isStreaming?: boolean;
		autoScroll?: boolean;
		onClear?: () => void;
		class?: string;
		children?: () => any;
	};

	let {
		output,
		isStreaming = false,
		autoScroll = true,
		onClear,
		class: className,
		children,
		...restProps
	}: Props = $props();

	const terminal = $state({
		output: "",
		isStreaming: false,
		autoScroll: true,
		onClear: undefined as (() => void) | undefined,
	});

	$effect(() => {
		terminal.output = output;
		terminal.isStreaming = isStreaming;
		terminal.autoScroll = autoScroll;
		terminal.onClear = onClear;
	});

	setTerminalContext(terminal);
</script>

<div
	class={cn(
		"flex flex-col overflow-hidden rounded-lg border bg-zinc-950 text-zinc-100",
		className,
	)}
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else}
		<TerminalHeader>
			<TerminalTitle />
			<div class="flex items-center gap-1">
				<TerminalStatus />
				<TerminalActions>
					<TerminalCopyButton />
					{#if onClear}
						<TerminalClearButton />
					{/if}
				</TerminalActions>
			</div>
		</TerminalHeader>
		<TerminalContent />
	{/if}
</div>
