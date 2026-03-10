import { z } from "zod";
import { createValidator, type ToolSchema } from "./index";

/**
 * Read tool input schema (Zod)
 */
export const ReadToolInputSchema = z.object({
	/** Path to the file to read */
	file_path: z.string().optional(),
	/** Number of lines to read (optional, for partial reads) */
	limit: z.number().optional(),
	/** Line offset to start reading from (optional) */
	offset: z.number().optional(),
	/** Page range for PDF files (optional, e.g., "1-5", "3") */
	pages: z.string().optional(),
});

/**
 * Read tool input type (inferred from Zod schema)
 */
export type ReadToolInput = z.infer<typeof ReadToolInputSchema>;

/**
 * Read tool output schema (Zod)
 */
export const ReadToolOutputSchema = z.object({
	/** File content as string */
	content: z.string().optional(),
	/** File content as array of lines */
	lines: z.array(z.string()).optional(),
	/** Error message if read failed */
	error: z.string().optional(),
});

/**
 * Read tool output type (inferred from Zod schema)
 */
export type ReadToolOutput = z.infer<typeof ReadToolOutputSchema>;

/**
 * Validates Read tool input parameters using Zod
 */
export const validateReadInput = createValidator(ReadToolInputSchema);

/**
 * Validates Read tool output using Zod
 *
 * Handles string output (direct content) and object output
 */
export const validateReadOutput = createValidator(
	z.union([
		z.string().transform((str) => ({ content: str })),
		ReadToolOutputSchema,
		z.object({}).transform(() => ({})),
	]),
);

/**
 * Read tool schema export
 */
export const ReadToolSchema: ToolSchema<ReadToolInput, ReadToolOutput> = {
	toolName: "Read",
	inputSchema: ReadToolInputSchema,
	outputSchema: ReadToolOutputSchema,
	validateInput: validateReadInput,
	validateOutput: validateReadOutput,
};
