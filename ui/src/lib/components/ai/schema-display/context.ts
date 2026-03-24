import { getContext, setContext } from "svelte";

const SCHEMA_DISPLAY_CONTEXT_KEY = Symbol.for(
	"discobot-ui-ai-schema-display-context",
);

export type HttpMethod = "GET" | "POST" | "PUT" | "PATCH" | "DELETE";

export type SchemaParameter = {
	name: string;
	type: string;
	required?: boolean;
	description?: string;
	location?: "path" | "query" | "header";
};

export type SchemaProperty = {
	name: string;
	type: string;
	required?: boolean;
	description?: string;
	properties?: SchemaProperty[];
	items?: SchemaProperty;
};

export type SchemaDisplayContextValue = {
	method: HttpMethod;
	path: string;
	description?: string;
	parameters?: SchemaParameter[];
	requestBody?: SchemaProperty[];
	responseBody?: SchemaProperty[];
};

export function setSchemaDisplayContext(
	value: SchemaDisplayContextValue,
): SchemaDisplayContextValue {
	return setContext(SCHEMA_DISPLAY_CONTEXT_KEY, value);
}

export function useSchemaDisplayContext(): SchemaDisplayContextValue {
	const context = getContext<SchemaDisplayContextValue | undefined>(
		SCHEMA_DISPLAY_CONTEXT_KEY,
	);
	if (!context) {
		throw new Error(
			"SchemaDisplay components must be used within SchemaDisplay",
		);
	}
	return context;
}
