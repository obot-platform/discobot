import type { DynamicToolUIPart } from "ai";
import type { z } from "zod";

/**
 * Base interface for all tool renderers
 *
 * Each tool renderer receives the tool input/output and must handle
 * validation, rendering, and error states.
 */
export interface ToolRendererProps<TInput = unknown, TOutput = unknown> {
	/** Unique identifier for this tool call */
	toolCallId: string;
	/** Tool input parameters (validated by renderer) */
	input: TInput;
	/** Tool execution result (null until execution completes) */
	output?: TOutput;
	/** Error message if execution failed */
	errorText?: string;
	/** Current lifecycle state of the tool */
	state: DynamicToolUIPart["state"];
}

/**
 * Validation result wrapper
 *
 * Returned by schema validation functions to indicate success/failure
 * and provide validated data or error messages.
 */
export interface ValidationResult<T> {
	/** Whether validation succeeded */
	success: boolean;
	/** Validated and typed data (only present if success is true) */
	data?: T;
	/** Error message (only present if success is false) */
	error?: string;
}

/**
 * Tool schema interface using Zod
 *
 * Each tool schema exports Zod schemas for input and output validation.
 * Validation is permissive to handle schema evolution - extra fields are allowed.
 */
export interface ToolSchema<TInput, TOutput> {
	/** Tool name as it appears in the API (e.g., "Bash", "Read") */
	toolName: string;
	/** Zod schema for input validation */
	inputSchema: z.ZodType<TInput>;
	/** Zod schema for output validation */
	outputSchema: z.ZodType<TOutput>;
	/** Validates and types the tool input parameters */
	validateInput: (input: unknown) => ValidationResult<TInput>;
	/** Validates and types the tool execution result */
	validateOutput: (output: unknown) => ValidationResult<TOutput>;
}

/**
 * Helper to create validation function from Zod schema
 */
export function createValidator<T>(
	schema: z.ZodType<T>,
): (input: unknown) => ValidationResult<T> {
	return (input: unknown) => {
		const result = schema.safeParse(input);
		if (result.success) {
			return { success: true, data: result.data };
		}
		return {
			success: false,
			error: result.error.issues.map((i) => i.message).join(", "),
		};
	};
}
