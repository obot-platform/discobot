<script lang="ts">
	import type { HTMLAttributes } from "svelte/elements";
	import { cn } from "$lib/utils";
	import { setAudioPlayerContext } from "./context";

	type Props = HTMLAttributes<HTMLDivElement> & {
		class?: string;
		children?: () => any;
	};

	let { class: className, children, style, ...restProps }: Props = $props();

	const player = $state({
		audio: null as HTMLAudioElement | null,
		isPlaying: false,
		currentTime: 0,
		duration: 0,
		muted: false,
		volume: 1,
	});

	function setAudio(audio: HTMLAudioElement | null) {
		player.audio = audio;
	}

	function togglePlay() {
		if (!player.audio) {
			return;
		}

		if (player.audio.paused) {
			void player.audio.play();
		} else {
			player.audio.pause();
		}
	}

	function setCurrentTime(time: number) {
		if (!player.audio) {
			return;
		}
		const duration = Number.isFinite(player.audio.duration)
			? player.audio.duration
			: 0;
		const next = Math.min(Math.max(time, 0), duration || Number.POSITIVE_INFINITY);
		player.audio.currentTime = Number.isFinite(next) ? next : 0;
	}

	function seekBy(seconds: number) {
		if (!player.audio) {
			return;
		}
		setCurrentTime(player.audio.currentTime + seconds);
	}

	function setMuted(muted: boolean) {
		if (!player.audio) {
			return;
		}
		player.audio.muted = muted;
	}

	function toggleMute() {
		if (!player.audio) {
			return;
		}
		player.audio.muted = !player.audio.muted;
	}

	function setVolume(volume: number) {
		if (!player.audio) {
			return;
		}
		const next = Math.min(Math.max(volume, 0), 1);
		player.audio.volume = next;
		if (next > 0 && player.audio.muted) {
			player.audio.muted = false;
		}
	}

	setAudioPlayerContext({
		get audio() {
			return player.audio;
		},
		setAudio,
		get isPlaying() {
			return player.isPlaying;
		},
		get currentTime() {
			return player.currentTime;
		},
		get duration() {
			return player.duration;
		},
		get muted() {
			return player.muted;
		},
		get volume() {
			return player.volume;
		},
		togglePlay,
		seekBy,
		setCurrentTime,
		toggleMute,
		setMuted,
		setVolume,
	});

	$effect(() => {
		const audio = player.audio;
		if (!audio) {
			player.isPlaying = false;
			player.currentTime = 0;
			player.duration = 0;
			player.muted = false;
			player.volume = 1;
			return;
		}

		const update = () => {
			player.isPlaying = !audio.paused;
			player.currentTime = Number.isFinite(audio.currentTime)
				? audio.currentTime
				: 0;
			player.duration = Number.isFinite(audio.duration) ? audio.duration : 0;
			player.muted = audio.muted;
			player.volume = audio.volume;
		};

		update();

		audio.addEventListener("play", update);
		audio.addEventListener("pause", update);
		audio.addEventListener("timeupdate", update);
		audio.addEventListener("durationchange", update);
		audio.addEventListener("loadedmetadata", update);
		audio.addEventListener("volumechange", update);
		audio.addEventListener("ended", update);

		return () => {
			audio.removeEventListener("play", update);
			audio.removeEventListener("pause", update);
			audio.removeEventListener("timeupdate", update);
			audio.removeEventListener("durationchange", update);
			audio.removeEventListener("loadedmetadata", update);
			audio.removeEventListener("volumechange", update);
			audio.removeEventListener("ended", update);
		};
	});
</script>

<div
	class={cn("not-prose w-full", className)}
	data-slot="audio-player"
	style={`--media-button-icon-width: 1rem; --media-button-icon-height: 1rem; --media-icon-color: currentColor; --media-font: var(--font-sans); --media-font-size: 10px; --media-control-background: transparent; --media-control-hover-background: var(--color-accent); --media-control-padding: 0; --media-background-color: transparent; --media-primary-color: var(--color-primary); --media-secondary-color: var(--color-secondary); --media-text-color: var(--color-foreground); --media-tooltip-background: var(--color-background); --media-range-bar-color: var(--color-primary); --media-tooltip-arrow-display: none; --media-tooltip-border-radius: var(--radius-md); --media-preview-time-text-shadow: none; --media-preview-time-background: var(--color-background); --media-preview-time-border-radius: var(--radius-md); --media-range-track-background: var(--color-secondary); ${style ?? ""}`}
	{...restProps}
>
	{@render children?.()}
</div>
