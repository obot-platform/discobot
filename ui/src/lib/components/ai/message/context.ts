import { getContext, setContext } from "svelte";

const MESSAGE_BRANCH_CONTEXT_KEY = Symbol.for(
	"discobot-ui-ai-message-branch-context",
);

export type MessageBranchContextValue = {
	currentBranch: number;
	totalBranches: number;
	goToPrevious: () => void;
	goToNext: () => void;
};

export function setMessageBranchContext(
	value: MessageBranchContextValue,
): MessageBranchContextValue {
	return setContext(MESSAGE_BRANCH_CONTEXT_KEY, value);
}

export function useMessageBranchContext(): MessageBranchContextValue {
	const context = getContext<MessageBranchContextValue | undefined>(
		MESSAGE_BRANCH_CONTEXT_KEY,
	);
	if (!context) {
		throw new Error(
			"MessageBranch components must be used within MessageBranch",
		);
	}
	return context;
}
