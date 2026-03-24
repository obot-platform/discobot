<script lang="ts">
	import type { HTMLInputAttributes } from "svelte/elements";
	import { ButtonGroupText } from "$lib/components/ui/button-group";
	import { cn } from "$lib/utils";
	import { useAudioPlayerContext } from "./context";

	type Props = Omit<HTMLInputAttributes, "type" | "value" | "max" | "min"> & {
		class?: string;
	};

	let { class: className, oninput, ...restProps }: Props = $props();
	const player = useAudioPlayerContext();

	function handleInput(
		event: Event & { currentTarget: EventTarget & HTMLInputElement },
	) {
		oninput?.(event);
		if (event.defaultPrevented) {
			return;
		}
		player.setVolume(Number(event.currentTarget.value));
	}
</script>

<ButtonGroupText class="bg-transparent" data-slot="audio-player-volume-range">
	<input
		class={cn("h-1 w-20 cursor-pointer accent-primary", className)}
		max={1}
		min={0}
		oninput={handleInput}
		step={0.01}
		type="range"
		value={player.volume}
		{...restProps}
	/>
</ButtonGroupText>
