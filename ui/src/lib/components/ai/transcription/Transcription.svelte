<script lang="ts">
	import { cn } from "$lib/utils";
	import {
		type TranscriptionSegmentData,
		setTranscriptionContext,
	} from "./context";

	type Props = {
		segments: TranscriptionSegmentData[];
		currentTime?: number;
		onSeek?: (time: number) => void;
		class?: string;
		children?: (segment: TranscriptionSegmentData, index: number) => any;
	};

	let {
		segments,
		currentTime = $bindable(0),
		onSeek,
		class: className,
		children,
		...restProps
	}: Props = $props();

	const transcription = $state({
		segments: [] as TranscriptionSegmentData[],
		currentTime: 0,
		seekTo: (time: number) => {
			currentTime = time;
			onSeek?.(time);
		},
		onSeek: undefined as ((time: number) => void) | undefined,
	});

	$effect(() => {
		transcription.segments = segments;
		transcription.currentTime = currentTime;
		transcription.onSeek = onSeek;
	});

	const visibleSegments = $derived.by(() =>
		segments.filter((segment) => segment.text.trim()),
	);

	setTranscriptionContext(transcription);
</script>

<div
	class={cn("flex flex-wrap gap-1 text-sm leading-relaxed", className)}
	data-slot="transcription"
	{...restProps}
>
	{#if children}
		{#each visibleSegments as segment, index (`${segment.startSecond}-${segment.endSecond}-${index}`)}
			{@render children(segment, index)}
		{/each}
	{:else}
		{#each visibleSegments as segment, index (`${segment.startSecond}-${segment.endSecond}-${index}`)}
			<span>{segment.text}</span>
		{/each}
	{/if}
</div>
