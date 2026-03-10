<script lang="ts">
	import { cn } from "$lib/utils";
	import { type TranscriptionSegmentData, useTranscriptionContext } from "./context";

	type Props = {
		segment: TranscriptionSegmentData;
		index: number;
		class?: string;
		onclick?: (event: MouseEvent) => void;
	};

	let { segment, index, class: className, onclick, ...restProps }: Props =
		$props();

	const transcription = useTranscriptionContext();

	const isActive = $derived.by(
		() =>
			transcription.currentTime >= segment.startSecond &&
			transcription.currentTime < segment.endSecond,
	);
	const isPast = $derived.by(
		() => transcription.currentTime >= segment.endSecond,
	);

	function handleClick(event: MouseEvent) {
		if (transcription.onSeek) {
			transcription.seekTo(segment.startSecond);
		}
		onclick?.(event);
	}
</script>

<button
	type="button"
	class={cn(
		"inline text-left",
		isActive && "text-primary",
		isPast && "text-muted-foreground",
		!isActive && !isPast && "text-muted-foreground/60",
		transcription.onSeek && "cursor-pointer hover:text-foreground",
		!transcription.onSeek && "cursor-default",
		className,
	)}
	data-active={isActive}
	data-index={index}
	data-slot="transcription-segment"
	onclick={handleClick}
	{...restProps}
>
	{segment.text}
</button>
