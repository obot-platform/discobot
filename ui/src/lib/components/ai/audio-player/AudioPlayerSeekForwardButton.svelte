<script lang="ts">
	import RotateCwIcon from "@lucide/svelte/icons/rotate-cw";
	import type { ComponentProps } from "svelte";
	import { Button } from "$lib/components/ui/button";
	import { useAudioPlayerContext } from "./context";

	type Props = ComponentProps<typeof Button> & {
		seekOffset?: number;
		children?: () => any;
	};

	let {
		seekOffset = 10,
		onclick,
		children,
		...restProps
	}: Props = $props();

	const player = useAudioPlayerContext();

	function handleClick(event: MouseEvent) {
		onclick?.(event as never);
		if (event.defaultPrevented) {
			return;
		}
		player.seekBy(Math.abs(seekOffset));
	}
</script>

<Button
	data-slot="audio-player-seek-forward-button"
	onclick={handleClick}
	size="icon-sm"
	variant="outline"
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else}
		<RotateCwIcon class="size-4" />
	{/if}
</Button>
