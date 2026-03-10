<script lang="ts">
	import { CommandItem } from "$lib/components/ui/command";
	import { cn } from "$lib/utils";
	import { useVoiceSelectorContext } from "./context";

	type Props = {
		value: string;
		onselect?: (value: string) => void;
		class?: string;
		children?: () => any;
	};

	let {
		value,
		onselect,
		class: className,
		children,
		...restProps
	}: Props = $props();

	const voiceSelector = useVoiceSelectorContext();

	function handleSelect() {
		voiceSelector.setValue(value);
		voiceSelector.setOpen(false);
		onselect?.(value);
	}
</script>

<CommandItem
	class={cn("px-4 py-2", className)}
	value={value}
	onSelect={handleSelect}
	{...restProps}
>
	{@render children?.()}
</CommandItem>
