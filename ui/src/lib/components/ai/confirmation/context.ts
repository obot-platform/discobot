import { getContext, setContext } from "svelte";
import type { ToolApproval, ToolState } from "$lib/components/ai/types";

const CONFIRMATION_CONTEXT_KEY = Symbol.for(
	"discobot-ui-ai-confirmation-context",
);

export type ConfirmationContextValue = {
	approval: ToolApproval;
	state: ToolState;
};

export function setConfirmationContext(
	value: ConfirmationContextValue,
): ConfirmationContextValue {
	return setContext(CONFIRMATION_CONTEXT_KEY, value);
}

export function useConfirmationContext(): ConfirmationContextValue {
	const context = getContext<ConfirmationContextValue | undefined>(
		CONFIRMATION_CONTEXT_KEY,
	);
	if (!context) {
		throw new Error("Confirmation components must be used within Confirmation");
	}
	return context;
}
