<script lang="ts">
	import { Command } from "$lib/components/ui/command";
	import { PopoverContent } from "$lib/components/ui/popover";
	import { cn } from "$lib/utils";
	import { useMicSelectorContext } from "./context";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
	const micSelector = useMicSelectorContext();
</script>

<PopoverContent
	class={cn("p-0", className)}
	style={`width: ${micSelector.width}px;`}
	{...restProps}
>
	<Command>
		{@render children?.()}
	</Command>
</PopoverContent>
