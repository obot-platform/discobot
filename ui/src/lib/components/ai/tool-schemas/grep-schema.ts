import { z } from "zod";
import { createValidator, type ToolSchema } from "./index";

/**
 * Grep tool input schema (Zod)
 */
export const GrepToolInputSchema = z.object({
	/** Regular expression pattern to search for */
	pattern: z.string().optional(),
	/** File or directory to search in */
	path: z.string().optional(),
	/** Glob pattern to filter files (e.g. "*.js") */
	glob: z.string().optional(),
	/** File type to search (e.g. "js", "py") */
	type: z.string().optional(),
	/** Output mode: "content", "files_with_matches", or "count" */
	output_mode: z.string().optional(),
	/** Case insensitive search */
	"-i": z.boolean().optional(),
	/** Number of lines to show after each match */
	"-A": z.number().optional(),
	/** Number of lines to show before each match */
	"-B": z.number().optional(),
	/** Context lines (both before and after) */
	"-C": z.number().optional(),
	/** Show line numbers */
	"-n": z.boolean().optional(),
	/** Multiline mode */
	multiline: z.boolean().optional(),
	/** Limit output to first N results */
	head_limit: z.number().optional(),
	/** Skip first N results */
	offset: z.number().optional(),
});

/**
 * Grep tool input type (inferred from Zod schema)
 */
export type GrepToolInput = z.infer<typeof GrepToolInputSchema>;

/**
 * Grep tool output schema (Zod)
 */
export const GrepToolOutputSchema = z.object({
	/** String output (for content mode) */
	content: z.string().optional(),
	/** Array of file paths (for files_with_matches mode) */
	files: z.array(z.string()).optional(),
	/** Match count (for count mode) */
	count: z.number().optional(),
	/** Structured matches with file paths and line numbers */
	matches: z
		.array(
			z.object({
				file: z.string(),
				line: z.number(),
				content: z.string(),
			}),
		)
		.optional(),
});

/**
 * Grep tool output type (inferred from Zod schema)
 */
export type GrepToolOutput = z.infer<typeof GrepToolOutputSchema>;

/**
 * Validates Grep tool input parameters using Zod
 */
export const validateGrepInput = createValidator(GrepToolInputSchema);

/**
 * Validates Grep tool output using Zod
 *
 * Handles string output (direct content) and object output
 */
export const validateGrepOutput = createValidator(
	z.union([
		z.string().transform((str) => ({ content: str })),
		GrepToolOutputSchema,
		z.object({}).transform(() => ({})),
	]),
);

/**
 * Grep tool schema export
 */
export const GrepToolSchema: ToolSchema<GrepToolInput, GrepToolOutput> = {
	toolName: "Grep",
	inputSchema: GrepToolInputSchema,
	outputSchema: GrepToolOutputSchema,
	validateInput: validateGrepInput,
	validateOutput: validateGrepOutput,
};
