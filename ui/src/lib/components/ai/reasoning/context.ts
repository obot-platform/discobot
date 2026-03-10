import { getContext, setContext } from "svelte";

const REASONING_CONTEXT_KEY = Symbol.for("discobot-ui-ai-reasoning-context");

export type ReasoningContextValue = {
	isStreaming: boolean;
	isOpen: boolean;
	setIsOpen: (open: boolean) => void;
	duration?: number;
};

export function setReasoningContext(
	value: ReasoningContextValue,
): ReasoningContextValue {
	return setContext(REASONING_CONTEXT_KEY, value);
}

export function useReasoningContext(): ReasoningContextValue {
	const context = getContext<ReasoningContextValue | undefined>(
		REASONING_CONTEXT_KEY,
	);
	if (!context) {
		throw new Error("Reasoning components must be used within Reasoning");
	}
	return context;
}
