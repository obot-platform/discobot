import { z } from "zod";
import { createValidator, type ToolSchema } from "./index";

/**
 * Edit tool input schema (Zod)
 *
 * Performs exact string replacements in files
 */
export const EditToolInputSchema = z.object({
	/** Path to the file to modify */
	file_path: z.string().optional(),
	/** The text to replace */
	old_string: z.string().optional(),
	/** The text to replace it with */
	new_string: z.string().optional(),
	/** Replace all occurrences (default false) */
	replace_all: z.boolean().optional().default(false),
});

/**
 * Edit tool input type (inferred from Zod schema)
 */
export type EditToolInput = z.infer<typeof EditToolInputSchema>;

/**
 * Edit tool output schema (Zod)
 */
export const EditToolOutputSchema = z.object({
	/** Whether the edit was successful */
	success: z.boolean().optional(),
	/** Number of replacements made */
	replacements: z.number().optional(),
	/** Error message if edit failed */
	error: z.string().optional(),
});

/**
 * Edit tool output type (inferred from Zod schema)
 */
export type EditToolOutput = z.infer<typeof EditToolOutputSchema>;

/**
 * Validates Edit tool input parameters using Zod
 */
export const validateEditInput = createValidator(EditToolInputSchema);

/**
 * Validates Edit tool output using Zod
 */
export const validateEditOutput = createValidator(
	z.union([EditToolOutputSchema, z.object({}).transform(() => ({}))]),
);

/**
 * Edit tool schema export
 */
export const EditToolSchema: ToolSchema<EditToolInput, EditToolOutput> = {
	toolName: "Edit",
	inputSchema: EditToolInputSchema,
	outputSchema: EditToolOutputSchema,
	validateInput: validateEditInput,
	validateOutput: validateEditOutput,
};
