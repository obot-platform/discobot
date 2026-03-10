import { getContext, setContext } from "svelte";

const CHAIN_OF_THOUGHT_CONTEXT_KEY = Symbol.for(
	"discobot-ui-ai-chain-of-thought-context",
);

export type ChainOfThoughtContextValue = {
	isOpen: boolean;
	setIsOpen: (open: boolean) => void;
};

export function setChainOfThoughtContext(
	value: ChainOfThoughtContextValue,
): ChainOfThoughtContextValue {
	return setContext(CHAIN_OF_THOUGHT_CONTEXT_KEY, value);
}

export function useChainOfThoughtContext(): ChainOfThoughtContextValue {
	const context = getContext<ChainOfThoughtContextValue | undefined>(
		CHAIN_OF_THOUGHT_CONTEXT_KEY,
	);
	if (!context) {
		throw new Error(
			"ChainOfThought components must be used within ChainOfThought",
		);
	}
	return context;
}
