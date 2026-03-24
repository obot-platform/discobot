<script lang="ts">
	import type { AudioPlayerContextValue } from "./context";
	import { useAudioPlayerContext } from "./context";

	type AudioData = {
		mediaType: string;
		base64: string;
	};

	type Props = Omit<
		import("svelte/elements").HTMLAttributes<HTMLAudioElement>,
		"src"
	> &
		(
			| {
					data: AudioData;
					src?: never;
			  }
			| {
					src: string;
					data?: never;
			  }
		);

	let { data, src, ...restProps }: Props = $props();
	let ref = $state<HTMLAudioElement | null>(null);
	const player: AudioPlayerContextValue = useAudioPlayerContext();

	const resolvedSrc = $derived.by(
		() =>
			src ??
			(data ? `data:${data.mediaType};base64,${data.base64}` : undefined),
	);

	$effect(() => {
		player.setAudio(ref);
		return () => {
			if (player.audio === ref) {
				player.setAudio(null);
			}
		};
	});
</script>

<audio
	bind:this={ref}
	data-slot="audio-player-element"
	src={resolvedSrc}
	{...restProps}
></audio>
