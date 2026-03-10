<script lang="ts">
	import PauseIcon from "@lucide/svelte/icons/pause";
	import PlayIcon from "@lucide/svelte/icons/play";
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
		player.togglePlay();
	}
</script>

<Button
	class={cn("bg-transparent", className)}
	data-slot="audio-player-play-button"
	onclick={handleClick}
	size="icon-sm"
	variant="outline"
	{...restProps}
>
	{#if children}
		{@render children()}
	{:else if player.isPlaying}
		<PauseIcon class="size-4" />
	{:else}
		<PlayIcon class="size-4" />
	{/if}
</Button>
