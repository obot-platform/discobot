import { z } from "zod";
import { createValidator, type ToolSchema } from "./index";

/**
 * Write tool input schema (Zod)
 */
export const WriteToolInputSchema = z.object({
	/** Path to the file to write */
	file_path: z.string().optional(),
	/** Content to write to the file */
	content: z.string().optional(),
});

/**
 * Write tool input type (inferred from Zod schema)
 */
export type WriteToolInput = z.infer<typeof WriteToolInputSchema>;

/**
 * Write tool output schema (Zod)
 */
export const WriteToolOutputSchema = z.object({
	/** Whether the write was successful */
	success: z.boolean().optional(),
	/** Number of bytes written */
	bytes_written: z.number().optional(),
	/** Error message if write failed */
	error: z.string().optional(),
});

/**
 * Write tool output type (inferred from Zod schema)
 */
export type WriteToolOutput = z.infer<typeof WriteToolOutputSchema>;

/**
 * Validates Write tool input parameters using Zod
 */
export const validateWriteInput = createValidator(WriteToolInputSchema);

/**
 * Validates Write tool output using Zod
 */
export const validateWriteOutput = createValidator(
	z.union([WriteToolOutputSchema, z.object({}).transform(() => ({}))]),
);

/**
 * Write tool schema export
 */
export const WriteToolSchema: ToolSchema<WriteToolInput, WriteToolOutput> = {
	toolName: "Write",
	inputSchema: WriteToolInputSchema,
	outputSchema: WriteToolOutputSchema,
	validateInput: validateWriteInput,
	validateOutput: validateWriteOutput,
};
