import { getContext, setContext } from "svelte";
import type { ParsedStackTrace } from "./parse";

const STACK_TRACE_CONTEXT_KEY = Symbol.for(
	"discobot-ui-ai-stack-trace-context",
);

export type StackTraceContextValue = {
	trace: ParsedStackTrace;
	raw: string;
	isOpen: boolean;
	setIsOpen: (open: boolean) => void;
	onFilePathClick?: (filePath: string, line?: number, column?: number) => void;
};

export function setStackTraceContext(
	value: StackTraceContextValue,
): StackTraceContextValue {
	return setContext(STACK_TRACE_CONTEXT_KEY, value);
}

export function useStackTraceContext(): StackTraceContextValue {
	const context = getContext<StackTraceContextValue | undefined>(
		STACK_TRACE_CONTEXT_KEY,
	);
	if (!context) {
		throw new Error("StackTrace components must be used within StackTrace");
	}
	return context;
}
