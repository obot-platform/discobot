import { z } from "zod";
import { createValidator, type ToolSchema } from "./index";

/**
 * TodoWrite tool input schema (Zod)
 */
export const TodoWriteToolInputSchema = z.object({
	/** Array of todos with status and content */
	todos: z
		.array(
			z.object({
				/** Todo description (imperative form) */
				content: z.string().optional(),
				/** Todo status: pending, in_progress, or completed */
				status: z.enum(["pending", "in_progress", "completed"]).optional(),
				/** Active form of the content (present continuous) */
				activeForm: z.string().optional(),
			}),
		)
		.optional(),
});

/**
 * TodoWrite tool input type (inferred from Zod schema)
 */
export type TodoWriteToolInput = z.infer<typeof TodoWriteToolInputSchema>;

/**
 * TodoWrite tool output schema (Zod)
 */
export const TodoWriteToolOutputSchema = z.object({
	/** Success indicator */
	success: z.boolean().optional(),
	/** Error message if operation failed */
	error: z.string().optional(),
});

/**
 * TodoWrite tool output type (inferred from Zod schema)
 */
export type TodoWriteToolOutput = z.infer<typeof TodoWriteToolOutputSchema>;

/**
 * Validates TodoWrite tool input parameters using Zod
 */
export const validateTodoWriteInput = createValidator(TodoWriteToolInputSchema);

/**
 * Validates TodoWrite tool output using Zod
 */
export const validateTodoWriteOutput = createValidator(
	z.union([TodoWriteToolOutputSchema, z.object({}).transform(() => ({}))]),
);

/**
 * TodoWrite tool schema export
 */
export const TodoWriteToolSchema: ToolSchema<
	TodoWriteToolInput,
	TodoWriteToolOutput
> = {
	toolName: "TodoWrite",
	inputSchema: TodoWriteToolInputSchema,
	outputSchema: TodoWriteToolOutputSchema,
	validateInput: validateTodoWriteInput,
	validateOutput: validateTodoWriteOutput,
};
