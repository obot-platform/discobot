import { z } from "zod";
import { createValidator, type ToolSchema } from "./index";

/**
 * Glob tool input schema (Zod)
 */
export const GlobToolInputSchema = z.object({
	/** Glob pattern to match files (e.g., "\*\*\/\*.ts", "src/\*\*\/\*.tsx") */
	pattern: z.string().optional(),
	/** Directory to search in (optional) */
	path: z.string().optional(),
});

/**
 * Glob tool input type (inferred from Zod schema)
 */
export type GlobToolInput = z.infer<typeof GlobToolInputSchema>;

/**
 * Glob tool output schema (Zod)
 */
export const GlobToolOutputSchema = z.object({
	/** Array of file paths that matched the pattern */
	files: z.array(z.string()).optional(),
	/** String output (alternative format) */
	content: z.string().optional(),
});

/**
 * Glob tool output type (inferred from Zod schema)
 */
export type GlobToolOutput = z.infer<typeof GlobToolOutputSchema>;

/**
 * Validates Glob tool input parameters using Zod
 */
export const validateGlobInput = createValidator(GlobToolInputSchema);

/**
 * Validates Glob tool output using Zod
 *
 * Handles string output, array output, and object output
 */
export const validateGlobOutput = createValidator(
	z.union([
		z.string().transform((str) => ({ content: str })),
		z.array(z.string()).transform((arr) => ({ files: arr })),
		GlobToolOutputSchema,
		z.object({}).transform(() => ({})),
	]),
);

/**
 * Glob tool schema export
 */
export const GlobToolSchema: ToolSchema<GlobToolInput, GlobToolOutput> = {
	toolName: "Glob",
	inputSchema: GlobToolInputSchema,
	outputSchema: GlobToolOutputSchema,
	validateInput: validateGlobInput,
	validateOutput: validateGlobOutput,
};
