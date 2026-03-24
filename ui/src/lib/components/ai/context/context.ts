import { getContext, setContext } from "svelte";
import type { LanguageModelUsage } from "$lib/components/ai/types";

const CONTEXT_USAGE_CONTEXT_KEY = Symbol.for(
	"discobot-ui-ai-context-usage-context",
);

export type ContextUsageValue = {
	usedTokens: number;
	maxTokens: number;
	usage?: LanguageModelUsage;
	modelId?: string;
};

export function setContextUsageContext(
	value: ContextUsageValue,
): ContextUsageValue {
	return setContext(CONTEXT_USAGE_CONTEXT_KEY, value);
}

export function useContextUsageContext(): ContextUsageValue {
	const context = getContext<ContextUsageValue | undefined>(
		CONTEXT_USAGE_CONTEXT_KEY,
	);
	if (!context) {
		throw new Error("Context components must be used within Context");
	}
	return context;
}

export function estimateCostUSD(tokens: number): number {
	// Lightweight Svelte-side fallback for gallery/demo usage.
	return tokens * 0.000001;
}
