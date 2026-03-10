import { z } from "zod";
import { createValidator, type ToolSchema } from "./index";

/**
 * WebSearch tool input schema (Zod)
 */
export const WebSearchToolInputSchema = z.object({
	/** Search query string */
	query: z.string().optional(),
	/** Only include results from these domains */
	allowed_domains: z.array(z.string()).optional(),
	/** Exclude results from these domains */
	blocked_domains: z.array(z.string()).optional(),
});

/**
 * WebSearch tool input type (inferred from Zod schema)
 */
export type WebSearchToolInput = z.infer<typeof WebSearchToolInputSchema>;

/**
 * WebSearch tool output schema (Zod)
 */
export const WebSearchToolOutputSchema = z.object({
	/** Search results */
	results: z
		.array(
			z.object({
				title: z.string(),
				url: z.string(),
				snippet: z.string().optional(),
				favicon: z.string().optional(),
			}),
		)
		.optional(),
	/** Raw string output */
	content: z.string().optional(),
});

/**
 * WebSearch tool output type (inferred from Zod schema)
 */
export type WebSearchToolOutput = z.infer<typeof WebSearchToolOutputSchema>;

/**
 * Validates WebSearch tool input parameters using Zod
 */
export const validateWebSearchInput = createValidator(WebSearchToolInputSchema);

/**
 * Validates WebSearch tool output using Zod
 *
 * Handles string output (direct content) and object output
 */
export const validateWebSearchOutput = createValidator(
	z.union([
		z.string().transform((str) => ({ content: str })),
		WebSearchToolOutputSchema,
		z.object({}).transform(() => ({})),
	]),
);

/**
 * WebSearch tool schema export
 */
export const WebSearchToolSchema: ToolSchema<
	WebSearchToolInput,
	WebSearchToolOutput
> = {
	toolName: "WebSearch",
	inputSchema: WebSearchToolInputSchema,
	outputSchema: WebSearchToolOutputSchema,
	validateInput: validateWebSearchInput,
	validateOutput: validateWebSearchOutput,
};
