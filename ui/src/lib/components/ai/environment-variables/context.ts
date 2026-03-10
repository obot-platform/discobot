import { getContext, setContext } from "svelte";

const ENVIRONMENT_VARIABLES_CONTEXT_KEY = Symbol.for(
	"discobot-ui-ai-environment-variables-context",
);
const ENVIRONMENT_VARIABLE_CONTEXT_KEY = Symbol.for(
	"discobot-ui-ai-environment-variable-context",
);

export type EnvironmentVariablesContextValue = {
	showValues: boolean;
	setShowValues: (show: boolean) => void;
};

export type EnvironmentVariableContextValue = {
	name: string;
	value: string;
};

export function setEnvironmentVariablesContext(
	value: EnvironmentVariablesContextValue,
): EnvironmentVariablesContextValue {
	return setContext(ENVIRONMENT_VARIABLES_CONTEXT_KEY, value);
}

export function useEnvironmentVariablesContext(): EnvironmentVariablesContextValue {
	const context = getContext<EnvironmentVariablesContextValue | undefined>(
		ENVIRONMENT_VARIABLES_CONTEXT_KEY,
	);
	if (!context) {
		throw new Error(
			"EnvironmentVariable components must be used within EnvironmentVariables",
		);
	}
	return context;
}

export function setEnvironmentVariableContext(
	value: EnvironmentVariableContextValue,
): EnvironmentVariableContextValue {
	return setContext(ENVIRONMENT_VARIABLE_CONTEXT_KEY, value);
}

export function useEnvironmentVariableContext(): EnvironmentVariableContextValue {
	const context = getContext<EnvironmentVariableContextValue | undefined>(
		ENVIRONMENT_VARIABLE_CONTEXT_KEY,
	);
	if (!context) {
		throw new Error(
			"EnvironmentVariable child components must be used within EnvironmentVariable",
		);
	}
	return context;
}
