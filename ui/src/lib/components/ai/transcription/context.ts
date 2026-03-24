import { getContext, setContext } from "svelte";
import type { Experimental_TranscriptionResult as TranscriptionResult } from "ai";

export type TranscriptionSegmentData = TranscriptionResult["segments"][number];

const TRANSCRIPTION_CONTEXT_KEY = Symbol.for(
	"discobot-ui-ai-transcription-context",
);

export type TranscriptionContextValue = {
	segments: TranscriptionSegmentData[];
	currentTime: number;
	seekTo: (time: number) => void;
	onSeek?: (time: number) => void;
};

export function setTranscriptionContext(
	value: TranscriptionContextValue,
): TranscriptionContextValue {
	return setContext(TRANSCRIPTION_CONTEXT_KEY, value);
}

export function useTranscriptionContext(): TranscriptionContextValue {
	const context = getContext<TranscriptionContextValue | undefined>(
		TRANSCRIPTION_CONTEXT_KEY,
	);
	if (!context) {
		throw new Error(
			"Transcription components must be used within Transcription",
		);
	}
	return context;
}
