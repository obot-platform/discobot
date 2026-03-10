import { getContext, setContext } from "svelte";

const TERMINAL_CONTEXT_KEY = Symbol.for("discobot-ui-ai-terminal-context");

export type TerminalContextValue = {
	output: string;
	isStreaming: boolean;
	autoScroll: boolean;
	onClear?: () => void;
};

export function setTerminalContext(
	value: TerminalContextValue,
): TerminalContextValue {
	return setContext(TERMINAL_CONTEXT_KEY, value);
}

export function useTerminalContext(): TerminalContextValue {
	const context = getContext<TerminalContextValue | undefined>(
		TERMINAL_CONTEXT_KEY,
	);
	if (!context) {
		throw new Error("Terminal components must be used within Terminal");
	}
	return context;
}
