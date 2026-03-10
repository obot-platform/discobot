import { getContext, setContext } from "svelte";

const AUDIO_PLAYER_CONTEXT_KEY = Symbol.for(
	"discobot-ui-ai-audio-player-context",
);

export type AudioPlayerContextValue = {
	audio: HTMLAudioElement | null;
	setAudio: (audio: HTMLAudioElement | null) => void;
	isPlaying: boolean;
	currentTime: number;
	duration: number;
	muted: boolean;
	volume: number;
	togglePlay: () => void;
	seekBy: (seconds: number) => void;
	setCurrentTime: (time: number) => void;
	toggleMute: () => void;
	setMuted: (muted: boolean) => void;
	setVolume: (volume: number) => void;
};

export function setAudioPlayerContext(
	value: AudioPlayerContextValue,
): AudioPlayerContextValue {
	return setContext(AUDIO_PLAYER_CONTEXT_KEY, value);
}

export function useAudioPlayerContext(): AudioPlayerContextValue {
	const context = getContext<AudioPlayerContextValue | undefined>(
		AUDIO_PLAYER_CONTEXT_KEY,
	);
	if (!context) {
		throw new Error("AudioPlayer components must be used within AudioPlayer");
	}
	return context;
}
