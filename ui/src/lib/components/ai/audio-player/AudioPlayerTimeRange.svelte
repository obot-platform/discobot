<script lang="ts">
	import type { HTMLInputAttributes } from "svelte/elements";
	import { ButtonGroupText } from "$lib/components/ui/button-group";
	import { cn } from "$lib/utils";
	import { useAudioPlayerContext } from "./context";

	type Props = Omit<HTMLInputAttributes, "type" | "value" | "max"> & {
		class?: string;
	};

	let { class: className, oninput, ...restProps }: Props = $props();
	const player = useAudioPlayerContext();

	const max = $derived.by(() =>
		player.duration > 0 && Number.isFinite(player.duration)
			? player.duration
			: 0,
	);

	function handleInput(event: Event & { currentTarget: EventTarget & HTMLInputElement }) {
		oninput?.(event);
		if (event.defaultPrevented) {
			return;
		}
		player.setCurrentTime(Number(event.currentTarget.value));
	}
</script>

<ButtonGroupText class="bg-transparent" data-slot="audio-player-time-range">
	<input
		class={cn("h-1 w-28 cursor-pointer accent-primary", className)}
		max={max}
		min={0}
		oninput={handleInput}
		step="any"
		type="range"
		value={player.currentTime}
		{...restProps}
	/>
</ButtonGroupText>
