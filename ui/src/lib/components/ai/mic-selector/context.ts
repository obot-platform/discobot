import { getContext, setContext } from "svelte";

const MIC_SELECTOR_CONTEXT_KEY = Symbol.for(
	"discobot-ui-ai-mic-selector-context",
);

export type MicSelectorContextValue = {
	data: MediaDeviceInfo[];
	loading: boolean;
	error: string | null;
	hasPermission: boolean;
	loadDevices: () => Promise<void>;
	value?: string;
	setValue: (value: string | undefined) => void;
	open: boolean;
	setOpen: (open: boolean) => void;
	width: number;
	setWidth: (width: number) => void;
};

export function setMicSelectorContext(
	value: MicSelectorContextValue,
): MicSelectorContextValue {
	return setContext(MIC_SELECTOR_CONTEXT_KEY, value);
}

export function useMicSelectorContext(): MicSelectorContextValue {
	const context = getContext<MicSelectorContextValue | undefined>(
		MIC_SELECTOR_CONTEXT_KEY,
	);
	if (!context) {
		throw new Error("MicSelector components must be used within MicSelector");
	}
	return context;
}
