import { getContext, setContext } from "svelte";

const OPEN_IN_CHAT_CONTEXT_KEY = Symbol.for("discobot-ui-ai-open-in-chat-context");

export type OpenInChatContextValue = {
	query: string;
};

export function setOpenInChatContext(
	value: OpenInChatContextValue,
): OpenInChatContextValue {
	return setContext(OPEN_IN_CHAT_CONTEXT_KEY, value);
}

export function useOpenInChatContext(): OpenInChatContextValue {
	const context = getContext<OpenInChatContextValue | undefined>(
		OPEN_IN_CHAT_CONTEXT_KEY,
	);
	if (!context) {
		throw new Error("OpenIn components must be used within an OpenIn provider");
	}
	return context;
}
