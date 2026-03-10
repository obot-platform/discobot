<script lang="ts">
	import Volume2Icon from "@lucide/svelte/icons/volume-2";
	import VolumeXIcon from "@lucide/svelte/icons/volume-x";
	import type { ComponentProps } from "svelte";
	import { Button } from "$lib/components/ui/button";
	import { cn } from "$lib/utils";
	import { useAudioPlayerContext } from "./context";

	type Props = ComponentProps<typeof Button> & {
		class?: string;
		children?: () => any;
	};

	let {
		class: className,
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
		player.toggleMute();
	}
</script>

<Button
	class={cn("bg-transparent", className)}
	data-slot="audio-player-mute-button"
	onclick={handleClick}
	size="icon-sm"
	variant="outline"
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else if player.muted || player.volume === 0}
		<VolumeXIcon class="size-4" />
	{:else}
		<Volume2Icon class="size-4" />
	{/if}
</Button>
