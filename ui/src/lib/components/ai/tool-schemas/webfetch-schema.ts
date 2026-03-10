import { z } from "zod";
import { createValidator, type ToolSchema } from "./index";

/**
 * WebFetch tool input schema (Zod)
 *
 * Fetches content from a URL and processes it with an AI model
 */
export const WebFetchToolInputSchema = z.object({
	/** The URL to fetch content from */
	url: z.string().optional(),
	/** The prompt describing what information to extract */
	prompt: z.string().optional(),
});

/**
 * WebFetch tool input type (inferred from Zod schema)
 */
export type WebFetchToolInput = z.infer<typeof WebFetchToolInputSchema>;

/**
 * WebFetch tool output schema (Zod)
 */
export const WebFetchToolOutputSchema = z.object({
	/** The processed content/response */
	content: z.string().optional(),
	/** Error message if fetch failed */
	error: z.string().optional(),
});

/**
 * WebFetch tool output type (inferred from Zod schema)
 */
export type WebFetchToolOutput = z.infer<typeof WebFetchToolOutputSchema>;

/**
 * Validates WebFetch tool input parameters using Zod
 */
export const validateWebFetchInput = createValidator(WebFetchToolInputSchema);

/**
 * Validates WebFetch tool output using Zod
 */
export const validateWebFetchOutput = createValidator(
	z.union([
		z.string().transform((str) => ({ content: str })),
		WebFetchToolOutputSchema,
		z.object({}).transform(() => ({})),
	]),
);

/**
 * WebFetch tool schema export
 */
export const WebFetchToolSchema: ToolSchema<
	WebFetchToolInput,
	WebFetchToolOutput
> = {
	toolName: "WebFetch",
	inputSchema: WebFetchToolInputSchema,
	outputSchema: WebFetchToolOutputSchema,
	validateInput: validateWebFetchInput,
	validateOutput: validateWebFetchOutput,
};
