<script lang="ts">
	import { Dialog } from "$lib/components/ui/dialog";
	import { setVoiceSelectorContext } from "./context";

	type Props = {
		value?: string;
		open?: boolean;
		onValueChange?: (value: string | undefined) => void;
		onOpenChange?: (open: boolean) => void;
		children?: () => any;
	};

	let {
		value = $bindable(undefined),
		open = $bindable(false),
		onValueChange,
		onOpenChange,
		children,
		...restProps
	}: Props = $props();

	const voiceSelector = $state({
		value: undefined as string | undefined,
		setValue: (nextValue: string | undefined) => {
			value = nextValue;
			onValueChange?.(nextValue);
		},
		open: false,
		setOpen: (nextOpen: boolean) => {
			open = nextOpen;
			onOpenChange?.(nextOpen);
		},
	});

	$effect(() => {
		voiceSelector.value = value;
		voiceSelector.open = open;
	});

	setVoiceSelectorContext(voiceSelector);
</script>

<Dialog bind:open {...restProps}>
	{@render children?.()}
</Dialog>
