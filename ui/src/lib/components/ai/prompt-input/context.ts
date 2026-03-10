import { getContext, setContext } from "svelte";

const PROMPT_INPUT_CONTEXT_KEY = Symbol.for("discobot-ui-ai-prompt-input-context");

export type PromptInputFile = {
	id: string;
	type: "file";
	url?: string;
	mediaType?: string;
	filename?: string;
};

export type PromptInputSubmitMessage = {
	text: string;
	files: Omit<PromptInputFile, "id">[];
};

export type PromptInputContextValue = {
	text: string;
	setText: (value: string) => void;
	files: PromptInputFile[];
	addFiles: (files: File[] | FileList) => void;
	removeFile: (id: string) => void;
	clearFiles: () => void;
	openFileDialog: () => void;
	requestSubmit: () => void;
};

export function setPromptInputContext(
	value: PromptInputContextValue,
): PromptInputContextValue {
	return setContext(PROMPT_INPUT_CONTEXT_KEY, value);
}

export function usePromptInputContext(): PromptInputContextValue {
	const context = getContext<PromptInputContextValue | undefined>(
		PROMPT_INPUT_CONTEXT_KEY,
	);
	if (!context) {
		throw new Error("PromptInput components must be used within PromptInput");
	}
	return context;
}
