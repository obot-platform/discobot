<script lang="ts">
	import type { HTMLAttributes } from "svelte/elements";
	import { ButtonGroupText } from "$lib/components/ui/button-group";
	import { cn } from "$lib/utils";
	import { useAudioPlayerContext } from "./context";
	import { formatTime } from "./format-time";

	type Props = HTMLAttributes<HTMLDivElement> & {
		class?: string;
	};

	let { class: className, ...restProps }: Props = $props();
	const player = useAudioPlayerContext();

	const label = $derived.by(() => formatTime(player.currentTime));
</script>

<ButtonGroupText
	class={cn("bg-transparent font-mono tabular-nums", className)}
	data-slot="audio-player-time-display"
	{...restProps}
>
	{label}
</ButtonGroupText>
