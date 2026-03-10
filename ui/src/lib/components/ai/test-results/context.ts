import { getContext, setContext } from "svelte";

const TEST_RESULTS_CONTEXT_KEY = Symbol.for("discobot-ui-ai-test-results-context");
const TEST_SUITE_CONTEXT_KEY = Symbol.for("discobot-ui-ai-test-suite-context");
const TEST_CONTEXT_KEY = Symbol.for("discobot-ui-ai-test-context");

export type TestStatus = "passed" | "failed" | "skipped" | "running";

export type TestResultsSummary = {
	passed: number;
	failed: number;
	skipped: number;
	total: number;
	duration?: number;
};

export type TestResultsContextValue = {
	summary?: TestResultsSummary;
};

export type TestSuiteContextValue = {
	name: string;
	status: TestStatus;
};

export type TestContextValue = {
	name: string;
	status: TestStatus;
	duration?: number;
};

export function setTestResultsContext(
	value: TestResultsContextValue,
): TestResultsContextValue {
	return setContext(TEST_RESULTS_CONTEXT_KEY, value);
}

export function useTestResultsContext(): TestResultsContextValue {
	return getContext<TestResultsContextValue | undefined>(TEST_RESULTS_CONTEXT_KEY) ?? {};
}

export function setTestSuiteContext(value: TestSuiteContextValue): TestSuiteContextValue {
	return setContext(TEST_SUITE_CONTEXT_KEY, value);
}

export function useTestSuiteContext(): TestSuiteContextValue {
	const context = getContext<TestSuiteContextValue | undefined>(TEST_SUITE_CONTEXT_KEY);
	if (!context) {
		throw new Error("TestSuite components must be used within TestSuite");
	}
	return context;
}

export function setTestContext(value: TestContextValue): TestContextValue {
	return setContext(TEST_CONTEXT_KEY, value);
}

export function useTestContext(): TestContextValue {
	const context = getContext<TestContextValue | undefined>(TEST_CONTEXT_KEY);
	if (!context) {
		throw new Error("Test components must be used within Test");
	}
	return context;
}
