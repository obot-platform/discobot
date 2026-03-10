import { getContext, setContext } from "svelte";

const VOICE_SELECTOR_CONTEXT_KEY = Symbol.for(
	"discobot-ui-ai-voice-selector-context",
);

export type VoiceSelectorContextValue = {
	value?: string;
	setValue: (value: string | undefined) => void;
	open: boolean;
	setOpen: (open: boolean) => void;
};

export function setVoiceSelectorContext(
	value: VoiceSelectorContextValue,
): VoiceSelectorContextValue {
	return setContext(VOICE_SELECTOR_CONTEXT_KEY, value);
}

export function useVoiceSelectorContext(): VoiceSelectorContextValue {
	const context = getContext<VoiceSelectorContextValue | undefined>(
		VOICE_SELECTOR_CONTEXT_KEY,
	);
	if (!context) {
		throw new Error("VoiceSelector components must be used within VoiceSelector");
	}
	return context;
}
