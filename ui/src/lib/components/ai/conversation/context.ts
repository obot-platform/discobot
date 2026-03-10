import { getContext, setContext } from "svelte";

const CONVERSATION_CONTEXT_KEY = Symbol.for("discobot-ui-ai-conversation-context");

export type ConversationContextValue = {
	isAtBottom: boolean;
	scrollToBottom: () => void;
};

export function setConversationContext(
	value: ConversationContextValue,
): ConversationContextValue {
	return setContext(CONVERSATION_CONTEXT_KEY, value);
}

export function useConversationContext(): ConversationContextValue {
	const context = getContext<ConversationContextValue | undefined>(
		CONVERSATION_CONTEXT_KEY,
	);
	if (!context) {
		throw new Error(
			"Conversation components must be used within a Conversation",
		);
	}
	return context;
}
