import { getContext, setContext } from "svelte";

const PLAN_CONTEXT_KEY = Symbol.for("discobot-ui-ai-plan-context");

export type PlanContextValue = {
	isStreaming: boolean;
};

export function setPlanContext(value: PlanContextValue): PlanContextValue {
	return setContext(PLAN_CONTEXT_KEY, value);
}

export function usePlanContext(): PlanContextValue {
	const context = getContext<PlanContextValue | undefined>(PLAN_CONTEXT_KEY);
	if (!context) {
		throw new Error("Plan components must be used within Plan");
	}
	return context;
}
