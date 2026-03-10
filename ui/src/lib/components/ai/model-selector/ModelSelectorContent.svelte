<script lang="ts">
	import { Command } from "$lib/components/ui/command";
	import { DialogContent, DialogTitle } from "$lib/components/ui/dialog";
	import { cn } from "$lib/utils";

	type Props = {
		title?: string;
		class?: string;
		children?: () => any;
	};

	let {
		title = "Model Selector",
		class: className,
		children,
		...restProps
	}: Props = $props();
</script>

<DialogContent
	class={cn("outline! border-none! p-0 outline-border! outline-solid!", className)}
	{...restProps}
>
	<DialogTitle class="sr-only">{title}</DialogTitle>
	<Command class="**:data-[slot=command-input-wrapper]:h-auto">
		{@render children?.()}
	</Command>
</DialogContent>
