import { getContext, setContext } from "svelte";

const TOOL_CONTEXT_KEY = Symbol.for("discobot-ui-ai-tool-context");

export type ToolContextValue = {
	isOpen: boolean;
	setIsOpen: (open: boolean) => void;
};

export function setToolContext(value: ToolContextValue): ToolContextValue {
	return setContext(TOOL_CONTEXT_KEY, value);
}

export function useToolContext(): ToolContextValue {
	const context = getContext<ToolContextValue | undefined>(TOOL_CONTEXT_KEY);
	if (!context) {
		throw new Error("Tool components must be used within Tool");
	}
	return context;
}
