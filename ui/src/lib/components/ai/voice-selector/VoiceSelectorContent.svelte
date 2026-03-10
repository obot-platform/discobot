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
		title = "Voice Selector",
		class: className,
		children,
		...restProps
	}: Props = $props();
</script>

<DialogContent class={cn("p-0", className)} {...restProps}>
	<DialogTitle class="sr-only">{title}</DialogTitle>
	<Command class="**:data-[slot=command-input-wrapper]:h-auto">
		{@render children?.()}
	</Command>
</DialogContent>
