import { getContext, setContext } from "svelte";

const CODE_BLOCK_CONTEXT_KEY = Symbol.for("discobot-ui-ai-code-block-context");

export type CodeBlockContextValue = {
	code: string;
};

export function setCodeBlockContext(
	value: CodeBlockContextValue,
): CodeBlockContextValue {
	return setContext(CODE_BLOCK_CONTEXT_KEY, value);
}

export function useCodeBlockContext(): CodeBlockContextValue {
	const context = getContext<CodeBlockContextValue | undefined>(
		CODE_BLOCK_CONTEXT_KEY,
	);
	if (!context) {
		throw new Error("CodeBlock components must be used within CodeBlock");
	}
	return context;
}
