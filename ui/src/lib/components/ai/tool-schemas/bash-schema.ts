import { z } from "zod";
import { createValidator, type ToolSchema } from "./index";

/**
 * Bash tool input schema (Zod)
 */
export const BashToolInputSchema = z.object({
	/** The shell command to execute */
	command: z.string().optional(),
	/** Optional description of what this command does */
	description: z.string().optional(),
	/** Optional timeout in milliseconds */
	timeout: z.number().optional(),
	/** Whether to run the command in the background */
	run_in_background: z.boolean().optional(),
	/** Whether to disable sandbox mode (dangerous) */
	dangerouslyDisableSandbox: z.boolean().optional(),
});

/**
 * Bash tool input type (inferred from Zod schema)
 */
export type BashToolInput = z.infer<typeof BashToolInputSchema>;

/**
 * Bash tool output schema (Zod)
 */
export const BashToolOutputSchema = z.object({
	/** Combined stdout/stderr output (most common format) */
	output: z.string().optional(),
	/** Standard output stream */
	stdout: z.string().optional(),
	/** Standard error stream */
	stderr: z.string().optional(),
	/** Exit code from the command */
	exitCode: z.number().optional(),
});

/**
 * Bash tool output type (inferred from Zod schema)
 */
export type BashToolOutput = z.infer<typeof BashToolOutputSchema>;

/**
 * Validates Bash tool input parameters using Zod
 */
export const validateBashInput = createValidator(BashToolInputSchema);

/**
 * Validates Bash tool output using Zod
 *
 * Handles both string output (legacy) and object output with stdout/stderr/exitCode
 */
export const validateBashOutput = createValidator(
	z.union([
		z.string().transform((str) => ({ output: str })),
		BashToolOutputSchema,
		z.object({}).transform(() => ({})),
	]),
);

/**
 * Bash tool schema export
 */
export const BashToolSchema: ToolSchema<BashToolInput, BashToolOutput> = {
	toolName: "Bash",
	inputSchema: BashToolInputSchema,
	outputSchema: BashToolOutputSchema,
	validateInput: validateBashInput,
	validateOutput: validateBashOutput,
};
