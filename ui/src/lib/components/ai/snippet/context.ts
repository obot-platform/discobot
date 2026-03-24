import { getContext, setContext } from "svelte";

const SNIPPET_CONTEXT_KEY = Symbol.for("discobot-ui-ai-snippet-context");

export type SnippetContextValue = {
	code: string;
};

export function setSnippetContext(
	value: SnippetContextValue,
): SnippetContextValue {
	return setContext(SNIPPET_CONTEXT_KEY, value);
}

export function useSnippetContext(): SnippetContextValue {
	const context = getContext<SnippetContextValue | undefined>(
		SNIPPET_CONTEXT_KEY,
	);
	if (!context) {
		throw new Error("Snippet components must be used within Snippet");
	}
	return context;
}
