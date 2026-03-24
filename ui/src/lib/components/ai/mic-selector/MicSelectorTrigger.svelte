<script lang="ts">
	import ChevronsUpDownIcon from "@lucide/svelte/icons/chevrons-up-down";
	import { Button } from "$lib/components/ui/button";
	import { PopoverTrigger } from "$lib/components/ui/popover";
	import { onDestroy } from "svelte";
	import { useMicSelectorContext } from "./context";

	type Props = {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, ...restProps }: Props = $props();
	const micSelector = useMicSelectorContext();
	let buttonRef = $state<HTMLButtonElement | null>(null);
	let observer = $state<ResizeObserver | null>(null);

	$effect(() => {
		if (!buttonRef) {
			return;
		}

		observer?.disconnect();
		observer = new ResizeObserver((entries) => {
			for (const entry of entries) {
				const nextWidth = (entry.target as HTMLElement).offsetWidth;
				if (nextWidth > 0) {
					micSelector.setWidth(nextWidth);
				}
			}
		});
		observer.observe(buttonRef);
	});

	onDestroy(() => {
		observer?.disconnect();
	});
</script>

<PopoverTrigger>
	<Button
		bind:ref={buttonRef}
		variant="outline"
		class={className}
		{...restProps}
	>
		{@render children?.()}
		<ChevronsUpDownIcon class="shrink-0 text-muted-foreground" size={16} />
	</Button>
</PopoverTrigger>
